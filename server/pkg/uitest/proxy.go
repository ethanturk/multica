package uitest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

var SafeBrowserTools = map[string]bool{
	"browser_click":            true,
	"browser_console_messages": true,
	"browser_drag":             true,
	"browser_drop":             true,
	"browser_fill_form":        true,
	"browser_find":             true,
	"browser_hover":            true,
	"browser_navigate":         true,
	"browser_navigate_back":    true,
	"browser_network_request":  true,
	"browser_network_requests": true,
	"browser_press_key":        true,
	"browser_resize":           true,
	"browser_select_option":    true,
	"browser_snapshot":         true,
	"browser_tabs":             true,
	"browser_take_screenshot":  true,
	"browser_type":             true,
	"browser_wait_for":         true,
}

const (
	defaultBrowserCallLimit = 10 * time.Second
	longBrowserCallLimit    = 30 * time.Second
)

type browserActionSession interface {
	RunBrowserAction(context.Context, func(context.Context) error) error
}

type proxyUpstream interface {
	request(context.Context, rpcRequest) (rpcResponse, error)
	notify(context.Context, rpcRequest) error
	eventStream() <-chan rpcRequest
	Close() error
}

type Proxy struct {
	session  browserActionSession
	upstream proxyUpstream
	axe      []byte
	logger   *slog.Logger
	writeMu  sync.Mutex
}

type ServeOptions struct {
	WorkDir     string
	TaskID      string
	Runtime     ReadyRuntime
	Input       io.Reader
	Output      io.Writer
	ErrorOutput io.Writer
}

func RunServer(ctx context.Context, options ServeOptions) error {
	if options.Input == nil || options.Output == nil {
		return fmt.Errorf("UI test server requires protocol input and output")
	}
	if options.ErrorOutput == nil {
		options.ErrorOutput = io.Discard
	}
	logger := slog.New(slog.NewTextHandler(options.ErrorOutput, nil))
	config, err := LoadConfig(options.WorkDir)
	if err != nil {
		return err
	}
	session, err := NewSession(SessionOptions{
		WorkDir: options.WorkDir,
		TaskID:  options.TaskID,
		Runtime: options.Runtime,
		Config:  config,
	}, logger)
	if err != nil {
		return err
	}
	defer session.Close()
	upstream, files, err := startUpstream(session, options.Runtime, logger)
	if err != nil {
		return err
	}
	defer upstream.Close()
	axe, err := readManagedAxe(files)
	if err != nil {
		return err
	}
	return newProxy(session, upstream, axe, logger).Serve(ctx, options.Input, options.Output)
}

func newProxy(session browserActionSession, upstream proxyUpstream, axe []byte, logger *slog.Logger) *Proxy {
	if logger == nil {
		logger = slog.Default()
	}
	return &Proxy{
		session: session, upstream: upstream,
		axe: append([]byte(nil), axe...), logger: logger,
	}
}

type clientFrame struct {
	frame []byte
	err   error
}

func (p *Proxy) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	serveContext, cancel := context.WithCancel(ctx)
	defer cancel()

	frames := make(chan clientFrame)
	go readClientFrames(serveContext, input, frames)
	events := p.upstream.eventStream()

	for {
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if err := p.write(output, event); err != nil {
				cancel()
				return err
			}
		case item, ok := <-frames:
			if !ok {
				cancel()
				return nil
			}
			if item.err != nil {
				if errors.Is(item.err, errRPCFrameTooLarge) {
					if err := p.write(output, responseError(nil, rpcInvalidRequest, ErrorPolicy, item.err.Error())); err != nil {
						cancel()
						return err
					}
					continue
				}
				cancel()
				return item.err
			}
			var request rpcRequest
			if err := json.Unmarshal(item.frame, &request); err != nil {
				if err := p.write(output, responseError(nil, rpcParseError, ErrorPolicy, "parse error")); err != nil {
					cancel()
					return err
				}
				continue
			}
			if request.JSONRPC != "2.0" || request.Method == "" {
				if len(request.ID) != 0 {
					if err := p.write(output, responseError(request.ID, rpcInvalidRequest, ErrorPolicy, "invalid JSON-RPC request")); err != nil {
						cancel()
						return err
					}
				}
				continue
			}
			if len(request.ID) == 0 {
				if request.Method == "notifications/initialized" {
					if err := p.upstream.notify(serveContext, request); err != nil && p.logger != nil {
						p.logger.Warn("ui-test: forward initialized notification failed", "error", err)
					}
				}
				continue
			}
			response := p.route(serveContext, request)
			if err := p.write(output, response); err != nil {
				cancel()
				return err
			}
		}
	}
}

func readClientFrames(ctx context.Context, input io.Reader, output chan<- clientFrame) {
	defer close(output)
	reader := bufio.NewReaderSize(input, 64*1024)
	for {
		frame, err := readRPCFrame(reader)
		if errors.Is(err, io.EOF) {
			return
		}
		select {
		case output <- clientFrame{frame: frame, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil && !errors.Is(err, errRPCFrameTooLarge) {
			return
		}
	}
}

func (p *Proxy) route(ctx context.Context, request rpcRequest) rpcResponse {
	switch request.Method {
	case "initialize", "ping":
		return p.forward(ctx, request, defaultBrowserCallLimit)
	case "tools/list":
		return p.listTools(ctx, request)
	case "tools/call":
		return p.callTool(ctx, request)
	default:
		return responseError(request.ID, rpcMethodNotFound, ErrorPolicy, "method not found: "+request.Method)
	}
}

func (p *Proxy) listTools(ctx context.Context, request rpcRequest) rpcResponse {
	response := p.forward(ctx, request, defaultBrowserCallLimit)
	if response.Error != nil {
		return response
	}
	var result struct {
		Tools []toolDescriptor `json:"tools"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return responseError(request.ID, rpcUpstreamError, ErrorBrowser, "invalid upstream tools/list response")
	}
	filtered := make([]toolDescriptor, 0, len(result.Tools)+1)
	for _, tool := range result.Tools {
		if SafeBrowserTools[tool.Name] {
			filtered = append(filtered, tool)
		}
	}
	filtered = append(filtered, accessibilityScanTool)
	data, err := json.Marshal(map[string]any{"tools": filtered})
	if err != nil {
		return responseError(request.ID, rpcInternalError, ErrorBrowser, err.Error())
	}
	return rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: data}
}

func (p *Proxy) callTool(ctx context.Context, request rpcRequest) rpcResponse {
	var call toolCallParams
	if err := decodeSingleJSON(request.Params, &call); err != nil || call.Name == "" {
		return responseError(request.ID, rpcInvalidParams, ErrorPolicy, "invalid tools/call parameters")
	}
	if call.Name == accessibilityScanTool.Name {
		if !emptyArguments(call.Arguments) {
			return responseError(request.ID, rpcInvalidParams, ErrorPolicy, "browser_accessibility_scan accepts no arguments")
		}
		params, err := axeEvaluateParams(p.axe)
		if err != nil {
			return responseError(request.ID, rpcInternalError, ErrorBrowser, err.Error())
		}
		translated := request
		translated.Params = params
		return p.runBrowserCall(ctx, request.ID, translated, longBrowserCallLimit)
	}
	if !SafeBrowserTools[call.Name] {
		return responseError(request.ID, rpcMethodNotFound, ErrorPolicy, "tool is not permitted: "+call.Name)
	}
	if call.Name == "browser_navigate" {
		var arguments struct {
			URL string `json:"url"`
		}
		if err := decodeSingleJSON(call.Arguments, &arguments); err != nil || arguments.URL == "" {
			return responseError(request.ID, rpcInvalidParams, ErrorPolicy, "browser_navigate requires a loopback URL")
		}
		if _, err := ValidateLoopbackURL(arguments.URL); err != nil {
			return responseError(request.ID, rpcInvalidParams, ErrorPolicy, err.Error())
		}
		return p.runBrowserCall(ctx, request.ID, request, longBrowserCallLimit)
	}
	return p.runBrowserCall(ctx, request.ID, request, defaultBrowserCallLimit)
}

func (p *Proxy) runBrowserCall(
	ctx context.Context,
	id json.RawMessage,
	request rpcRequest,
	limit time.Duration,
) rpcResponse {
	callContext, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var response rpcResponse
	err := p.session.RunBrowserAction(callContext, func(actionContext context.Context) error {
		var err error
		response, err = p.upstream.request(actionContext, request)
		return err
	})
	if err != nil {
		class := ErrorBrowser
		var lifecycleErr *LifecycleError
		if errors.As(err, &lifecycleErr) {
			class = lifecycleErr.Class
		}
		code := rpcUpstreamError
		if errors.Is(err, context.DeadlineExceeded) {
			code = rpcTimeoutError
		}
		return responseError(id, code, class, err.Error())
	}
	return boundedUpstreamResponse(response, id)
}

func (p *Proxy) forward(ctx context.Context, request rpcRequest, limit time.Duration) rpcResponse {
	callContext, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	response, err := p.upstream.request(callContext, request)
	if err != nil {
		code := rpcUpstreamError
		if errors.Is(err, context.DeadlineExceeded) {
			code = rpcTimeoutError
		}
		return responseError(request.ID, code, ErrorBrowser, err.Error())
	}
	return boundedUpstreamResponse(response, request.ID)
}

func (p *Proxy) write(output io.Writer, value any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return writeRPCFrame(output, value)
}

func emptyArguments(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	var arguments map[string]json.RawMessage
	return decodeSingleJSON(raw, &arguments) == nil && len(arguments) == 0
}

func decodeSingleJSON(raw json.RawMessage, destination any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
