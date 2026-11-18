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
	// The singular request inspector returns full headers and bodies, including
	// credentials injected by the local reverse proxy. Keep only the bounded
	// request summary surface available to the agent.
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
	Close() error
}

type proxyUpstream interface {
	request(context.Context, rpcRequest) (rpcResponse, error)
	notify(context.Context, rpcRequest) error
	eventStream() <-chan rpcRequest
	doneStream() <-chan struct{}
	terminalError() error
	Close() error
}

type Proxy struct {
	session  browserActionSession
	upstream proxyUpstream
	axe      []byte
	logger   *slog.Logger
}

type ServeOptions struct {
	WorkDir     string
	TaskID      string
	Runtime     ReadyRuntime
	RuntimeRoot string
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
	upstream, files, err := startUpstream(session, options.Runtime, options.RuntimeRoot, logger)
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

func (p *Proxy) Serve(ctx context.Context, input io.Reader, output io.Writer) (serveErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	serveContext, cancel := context.WithCancel(ctx)
	writer := newOwnedRPCWriter(output)
	defer func() {
		cancel()
		_ = closeWithCause(input, serveErr)
		writerErr := writer.shutdown(serveErr)
		upstreamErr := p.upstream.Close()
		sessionErr := p.session.Close()
		if serveErr == nil {
			serveErr = errors.Join(writerErr, upstreamErr, sessionErr)
		}
	}()

	frames := make(chan clientFrame)
	go readClientFrames(serveContext, input, frames)
	events := p.upstream.eventStream()
	upstreamDone := p.upstream.doneStream()

	for {
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case <-upstreamDone:
			err := p.upstream.terminalError()
			if err == nil {
				err = io.EOF
			}
			return err
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if !rpcFrameWithinLimit(event) {
				if p.logger != nil {
					p.logger.Warn("ui-test: dropped oversized upstream notification", "method", event.Method)
				}
				continue
			}
			if err := writer.write(serveContext, event); err != nil {
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
					if err := writer.write(serveContext, responseError(nil, rpcInvalidRequest, ErrorPolicy, item.err.Error())); err != nil {
						cancel()
						return err
					}
					continue
				}
				cancel()
				return item.err
			}
			request, err := decodeRPCRequest(item.frame)
			if err != nil {
				code := rpcInvalidRequest
				message := "invalid JSON-RPC request"
				if errors.Is(err, errMalformedRPCJSON) {
					code = rpcParseError
					message = "parse error"
				}
				if err := writer.write(serveContext, responseError(nil, code, ErrorPolicy, message)); err != nil {
					cancel()
					return err
				}
				continue
			}
			isNotification := len(request.ID) == 0
			if (request.Method == "notifications/initialized") != isNotification {
				if err := writer.write(serveContext, responseError(nil, rpcInvalidRequest, ErrorPolicy, "invalid request/notification shape")); err != nil {
					cancel()
					return err
				}
				continue
			}
			if isNotification {
				if err := p.upstream.notify(serveContext, request); err != nil && p.logger != nil {
					p.logger.Warn("ui-test: forward initialized notification failed", "error", err)
				}
				continue
			}
			response := p.route(serveContext, request)
			if err := writer.write(serveContext, response); err != nil {
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
	call, err := decodeToolCall(request.Params)
	if err != nil {
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
		arguments, err := decodeJSONObject(call.Arguments)
		if err != nil || len(arguments) != 1 {
			return responseError(request.ID, rpcInvalidParams, ErrorPolicy, "browser_navigate requires a loopback URL")
		}
		var target string
		if err := json.Unmarshal(arguments["url"], &target); err != nil || target == "" {
			return responseError(request.ID, rpcInvalidParams, ErrorPolicy, "browser_navigate requires a loopback URL")
		}
		if _, err := ValidateLoopbackURL(target); err != nil {
			return responseError(request.ID, rpcInvalidParams, ErrorPolicy, err.Error())
		}
		safeArguments, err := json.Marshal(map[string]string{"url": target})
		if err != nil {
			return responseError(request.ID, rpcInternalError, ErrorBrowser, err.Error())
		}
		safeParams, err := json.Marshal(toolCallParams{
			Name:      "browser_navigate",
			Arguments: safeArguments,
		})
		if err != nil {
			return responseError(request.ID, rpcInternalError, ErrorBrowser, err.Error())
		}
		sanitized := request
		sanitized.Params = safeParams
		return p.runBrowserCall(ctx, request.ID, sanitized, longBrowserCallLimit)
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

func emptyArguments(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	arguments, err := decodeJSONObject(raw)
	return err == nil && len(arguments) == 0
}

type outboundRPCFrame struct {
	data   []byte
	result chan error
}

type ownedRPCWriter struct {
	output io.Writer
	queue  chan outboundRPCFrame
	stop   chan struct{}
	done   chan struct{}

	stopOnce sync.Once
	mu       sync.Mutex
	err      error
}

func newOwnedRPCWriter(output io.Writer) *ownedRPCWriter {
	writer := &ownedRPCWriter{
		output: output,
		queue:  make(chan outboundRPCFrame, upstreamNotificationBuffer),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go writer.run()
	return writer
}

func (w *ownedRPCWriter) write(ctx context.Context, value any) error {
	if response, ok := value.(rpcResponse); ok {
		value = boundedRPCResponse(response)
	}
	frame, err := marshalRPCFrame(value)
	if err != nil {
		return err
	}
	item := outboundRPCFrame{data: frame, result: make(chan error, 1)}
	select {
	case w.queue <- item:
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return w.terminalError()
	}
	select {
	case err := <-item.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return w.terminalError()
	}
}

func (w *ownedRPCWriter) run() {
	defer close(w.done)
	for {
		select {
		case <-w.stop:
			return
		case item := <-w.queue:
			written, err := w.output.Write(item.data)
			if err == nil && written != len(item.data) {
				err = io.ErrShortWrite
			}
			if err != nil {
				w.mu.Lock()
				w.err = err
				w.mu.Unlock()
			}
			item.result <- err
			if err != nil {
				return
			}
		}
	}
}

func (w *ownedRPCWriter) shutdown(cause error) error {
	w.stopOnce.Do(func() {
		close(w.stop)
	})
	closeErr := closeWithCause(w.output, cause)
	if _, closable := w.output.(io.Closer); closable {
		<-w.done
	} else {
		select {
		case <-w.done:
		case <-time.After(100 * time.Millisecond):
		}
	}
	if cause != nil {
		return nil
	}
	return errors.Join(closeErr, w.terminalError())
}

func (w *ownedRPCWriter) terminalError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func closeWithCause(value any, cause error) error {
	if closer, ok := value.(interface{ CloseWithError(error) error }); ok {
		return closer.CloseWithError(cause)
	}
	if closer, ok := value.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func decodeToolCall(raw json.RawMessage) (toolCallParams, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	members, err := decodeJSONObject(raw)
	if err != nil {
		return toolCallParams{}, err
	}
	for name := range members {
		if name != "name" && name != "arguments" {
			return toolCallParams{}, errInvalidRPCRequest
		}
	}
	var call toolCallParams
	if err := json.Unmarshal(members["name"], &call.Name); err != nil || call.Name == "" {
		return toolCallParams{}, errInvalidRPCRequest
	}
	if arguments, ok := members["arguments"]; ok {
		call.Arguments = append(json.RawMessage(nil), arguments...)
	}
	return call, nil
}
