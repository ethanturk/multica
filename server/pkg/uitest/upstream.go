package uitest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const upstreamNotificationBuffer = 32

type upstreamResult struct {
	response rpcResponse
	err      error
}

type upstreamWireError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type upstreamWireMessage struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id,omitempty"`
	Method  string             `json:"method,omitempty"`
	Params  json.RawMessage    `json:"params,omitempty"`
	Result  json.RawMessage    `json:"result,omitempty"`
	Error   *upstreamWireError `json:"error,omitempty"`
}

type Upstream struct {
	reader       io.ReadCloser
	writer       io.WriteCloser
	stop         func() error
	logger       *slog.Logger
	networkProxy *loopbackForwardProxy

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan upstreamResult
	err     error

	nextID     atomic.Uint64
	events     chan rpcRequest
	done       chan struct{}
	readerDone chan struct{}
	closeOnce  sync.Once
}

func newUpstream(reader io.ReadCloser, writer io.WriteCloser, stop func() error) *Upstream {
	upstream := &Upstream{
		reader:     reader,
		writer:     writer,
		stop:       stop,
		pending:    make(map[string]chan upstreamResult),
		events:     make(chan rpcRequest, upstreamNotificationBuffer),
		done:       make(chan struct{}),
		readerDone: make(chan struct{}),
	}
	go upstream.readLoop()
	return upstream
}

func (u *Upstream) request(ctx context.Context, request rpcRequest) (rpcResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id := strconv.FormatUint(u.nextID.Add(1), 10)
	request.JSONRPC = "2.0"
	request.ID = json.RawMessage(id)
	result := make(chan upstreamResult, 1)

	u.mu.Lock()
	if u.err != nil {
		err := u.err
		u.mu.Unlock()
		return rpcResponse{}, err
	}
	u.pending[id] = result
	u.mu.Unlock()

	if err := u.writeContext(ctx, request); err != nil {
		u.removePending(id)
		return rpcResponse{}, err
	}
	select {
	case delivered := <-result:
		return delivered.response, delivered.err
	case <-ctx.Done():
		u.removePending(id)
		return rpcResponse{}, ctx.Err()
	case <-u.done:
		select {
		case delivered := <-result:
			return delivered.response, delivered.err
		default:
		}
		u.mu.Lock()
		err := u.err
		u.mu.Unlock()
		if err == nil {
			err = io.EOF
		}
		return rpcResponse{}, err
	}
}

func (u *Upstream) notify(ctx context.Context, request rpcRequest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	request.JSONRPC = "2.0"
	request.ID = nil
	return u.writeContext(ctx, request)
}

func (u *Upstream) eventStream() <-chan rpcRequest { return u.events }
func (u *Upstream) doneStream() <-chan struct{}    { return u.done }

func (u *Upstream) terminalError() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.err
}

func (u *Upstream) Close() error {
	var closeErr error
	u.closeOnce.Do(func() {
		if u.networkProxy != nil {
			closeErr = u.networkProxy.Close()
		}
		closeErr = errors.Join(closeErr, u.writer.Close(), u.reader.Close())
		if u.stop != nil {
			closeErr = errors.Join(closeErr, u.stop())
		}
		u.fail(closeErr)
		<-u.readerDone
	})
	if closeErr == nil {
		u.mu.Lock()
		closeErr = u.err
		u.mu.Unlock()
		if errors.Is(closeErr, io.EOF) || errors.Is(closeErr, os.ErrClosed) {
			return nil
		}
	}
	return closeErr
}

func (u *Upstream) write(value any) error {
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	select {
	case <-u.done:
		u.mu.Lock()
		err := u.err
		u.mu.Unlock()
		if err == nil {
			return io.EOF
		}
		return err
	default:
	}
	return writeRPCFrame(u.writer, value)
}

func (u *Upstream) writeContext(ctx context.Context, value any) error {
	done := make(chan error, 1)
	go func() {
		done <- u.write(value)
	}()
	select {
	case err := <-done:
		if err != nil {
			u.fail(err)
		}
		return err
	case <-ctx.Done():
		_ = u.writer.Close()
		_ = u.reader.Close()
		u.fail(ctx.Err())
		return ctx.Err()
	case <-u.done:
		_ = u.writer.Close()
		if err := <-done; err == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		u.mu.Lock()
		err := u.err
		u.mu.Unlock()
		return err
	}
}

func (u *Upstream) readLoop() {
	defer close(u.readerDone)
	defer close(u.events)
	reader := bufio.NewReaderSize(u.reader, 64*1024)
	for {
		frame, err := readRPCFrame(reader)
		if err != nil {
			u.fail(err)
			return
		}
		if len(frame) == 0 {
			continue
		}
		select {
		case <-u.done:
			return
		default:
		}
		var message upstreamWireMessage
		if err := json.Unmarshal(frame, &message); err != nil {
			u.fail(fmt.Errorf("decode upstream JSON-RPC: %w", err))
			return
		}
		if message.Method != "" {
			request := rpcRequest{
				JSONRPC: message.JSONRPC,
				ID:      append(json.RawMessage(nil), message.ID...),
				Method:  message.Method,
				Params:  append(json.RawMessage(nil), message.Params...),
			}
			if len(message.ID) != 0 {
				_ = u.write(responseError(message.ID, rpcMethodNotFound, ErrorPolicy, "upstream-initiated requests are unsupported"))
				continue
			}
			select {
			case u.events <- request:
			default:
				if u.logger != nil {
					u.logger.Warn("ui-test: dropped upstream notification", "method", message.Method)
				}
			}
			continue
		}
		key := string(message.ID)
		u.mu.Lock()
		pending := u.pending[key]
		delete(u.pending, key)
		u.mu.Unlock()
		if pending == nil {
			continue
		}
		response := rpcResponse{
			JSONRPC: message.JSONRPC,
			ID:      message.ID,
			Result:  message.Result,
		}
		if message.Error != nil {
			response.Error = &rpcError{
				Code:    message.Error.Code,
				Message: message.Error.Message,
				Data:    rpcErrorData{Detail: message.Error.Data},
			}
		}
		pending <- upstreamResult{response: response}
	}
}

func (u *Upstream) removePending(id string) {
	u.mu.Lock()
	delete(u.pending, id)
	u.mu.Unlock()
}

func (u *Upstream) fail(err error) {
	if err == nil {
		err = io.EOF
	}
	u.mu.Lock()
	if u.err != nil {
		u.mu.Unlock()
		return
	}
	u.err = err
	pending := u.pending
	u.pending = make(map[string]chan upstreamResult)
	u.mu.Unlock()
	for _, waiter := range pending {
		waiter <- upstreamResult{err: err}
	}
	close(u.done)
}

type runtimeFiles struct {
	RuntimeDir string
	MCPCLI     string
	Axe        string
	Browsers   string
}

type upstreamPaths struct {
	runtimeFiles
	Config       string
	StorageState string
}

type upstreamConfig struct {
	Browser struct {
		BrowserName   string `json:"browserName"`
		Isolated      bool   `json:"isolated"`
		LaunchOptions struct {
			Headless bool `json:"headless"`
			Proxy    struct {
				Server string `json:"server"`
			} `json:"proxy"`
		} `json:"launchOptions"`
		ContextOptions struct {
			Viewport Viewport `json:"viewport"`
		} `json:"contextOptions"`
	} `json:"browser"`
	OutputDir     string `json:"outputDir"`
	OutputMaxSize int    `json:"outputMaxSize"`
	Network       struct {
		AllowedOrigins []string `json:"allowedOrigins"`
	} `json:"network"`
	AllowUnrestrictedFileAccess bool `json:"allowUnrestrictedFileAccess"`
}

func resolveRuntimeFiles(runtime ReadyRuntime, trustedRoot string) (runtimeFiles, error) {
	canonical, err := exactTrustedRuntimeDirectory(runtime.Directory, trustedRoot)
	if err != nil {
		return runtimeFiles{}, err
	}
	return verifyRuntimeDirectory(canonical)
}

func verifyRuntimeDirectory(runtimeDirectory string) (runtimeFiles, error) {
	if runtimeDirectory == "" {
		return runtimeFiles{}, fmt.Errorf("runtime directory is required")
	}
	absolute, err := filepath.Abs(runtimeDirectory)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("resolve runtime directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Lstat(absolute)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("inspect runtime directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return runtimeFiles{}, fmt.Errorf("managed runtime directory must be a directory, not a symlink")
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("resolve runtime directory: %w", err)
	}
	ready, err := openRegularManagedFile(filepath.Join(canonical, "ready.json"))
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("open regular ready manifest: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(ready, diagnosticLimit+1))
	closeErr := ready.Close()
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("read ready manifest: %w", err)
	}
	if closeErr != nil {
		return runtimeFiles{}, fmt.Errorf("close ready manifest: %w", closeErr)
	}
	if len(data) > diagnosticLimit {
		return runtimeFiles{}, fmt.Errorf("ready manifest exceeds %d bytes", diagnosticLimit)
	}
	var manifest readyManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return runtimeFiles{}, fmt.Errorf("decode ready manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runtimeFiles{}, fmt.Errorf("decode ready manifest: multiple JSON values")
	}
	if manifest.MCPVersion != PlaywrightMCPVersion ||
		manifest.AxeVersion != AxeCoreVersion ||
		manifest.Browser != "chromium" ||
		manifest.InstalledAt.IsZero() {
		return runtimeFiles{}, fmt.Errorf("ready manifest does not match pinned runtime")
	}
	wantCLI := filepath.ToSlash(filepath.Join("node_modules", "@playwright", "mcp", "cli.js"))
	wantAxe := filepath.ToSlash(filepath.Join("node_modules", "axe-core", "axe.min.js"))
	wantPlaywright := filepath.ToSlash(filepath.Join("node_modules", ".bin", "playwright"))
	if manifest.MCPCLIPath != wantCLI {
		return runtimeFiles{}, fmt.Errorf("Playwright MCP CLI must use fixed managed path %q", wantCLI)
	}
	if manifest.AxePath != wantAxe {
		return runtimeFiles{}, fmt.Errorf("Axe must use fixed managed path %q", wantAxe)
	}
	if manifest.PlaywrightPath != wantPlaywright {
		return runtimeFiles{}, fmt.Errorf("Playwright CLI must use fixed managed path %q", wantPlaywright)
	}
	cli, err := managedPath(canonical, manifest.MCPCLIPath)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("Playwright MCP CLI path: %w", err)
	}
	axe, err := managedPath(canonical, manifest.AxePath)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("Axe path: %w", err)
	}
	playwright, err := managedPath(canonical, manifest.PlaywrightPath)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("Playwright CLI path: %w", err)
	}
	for label, path := range map[string]string{
		"Playwright MCP CLI": cli,
		"Axe":                axe,
		"Playwright CLI":     playwright,
	} {
		file, err := openRegularManagedFile(path)
		if err != nil {
			return runtimeFiles{}, fmt.Errorf("%s is not a regular managed file: %w", label, err)
		}
		if err := file.Close(); err != nil {
			return runtimeFiles{}, fmt.Errorf("close %s: %w", label, err)
		}
	}
	for label, metadata := range map[string]struct {
		path string
		want string
	}{
		"Playwright MCP": {
			path: filepath.Join(canonical, "node_modules", "@playwright", "mcp", "package.json"),
			want: PlaywrightMCPVersion,
		},
		"Axe": {
			path: filepath.Join(canonical, "node_modules", "axe-core", "package.json"),
			want: AxeCoreVersion,
		},
	} {
		if err := verifyManagedPackageVersion(metadata.path, metadata.want); err != nil {
			return runtimeFiles{}, fmt.Errorf("%s package: %w", label, err)
		}
	}
	expectedBrowsers := filepath.Join(canonical, "browsers")
	browsers, err := filepath.EvalSymlinks(expectedBrowsers)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("browser directory: %w", err)
	}
	if browsers != expectedBrowsers {
		return runtimeFiles{}, fmt.Errorf("browser directory contains a symlink")
	}
	relative, err := filepath.Rel(canonical, browsers)
	if err != nil || relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return runtimeFiles{}, fmt.Errorf("browser directory escapes runtime")
	}
	browserExecutable, err := managedPath(canonical, manifest.BrowserPath)
	if err != nil || !pathWithin(browsers, browserExecutable) {
		return runtimeFiles{}, fmt.Errorf("Chromium path is outside fixed managed browser directory")
	}
	browserFile, err := openRegularManagedFile(browserExecutable)
	if err != nil {
		return runtimeFiles{}, fmt.Errorf("Chromium is not a regular managed file: %w", err)
	}
	if err := browserFile.Close(); err != nil {
		return runtimeFiles{}, fmt.Errorf("close Chromium: %w", err)
	}
	return runtimeFiles{RuntimeDir: canonical, MCPCLI: cli, Axe: axe, Browsers: browsers}, nil
}

func exactTrustedRuntimeDirectory(runtimeDirectory, trustedRoot string) (string, error) {
	if runtimeDirectory == "" || trustedRoot == "" {
		return "", fmt.Errorf("ready runtime and trusted UI test root are required")
	}
	root, err := filepath.Abs(trustedRoot)
	if err != nil {
		return "", fmt.Errorf("resolve trusted UI test root: %w", err)
	}
	root = filepath.Clean(root)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve trusted UI test root: %w", err)
	}
	if canonicalRoot != root {
		return "", fmt.Errorf("trusted UI test root contains a symlink")
	}
	expected := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	supplied, err := filepath.Abs(runtimeDirectory)
	if err != nil {
		return "", fmt.Errorf("resolve UI test runtime directory: %w", err)
	}
	supplied = filepath.Clean(supplied)
	if supplied != expected {
		return "", fmt.Errorf("runtime is outside trusted UI test root or not pinned version %s", PlaywrightMCPVersion)
	}
	canonical, err := filepath.EvalSymlinks(supplied)
	if err != nil {
		return "", fmt.Errorf("resolve UI test runtime directory: %w", err)
	}
	if canonical != expected {
		return "", fmt.Errorf("managed runtime directory contains a symlink")
	}
	return canonical, nil
}

func verifyManagedPackageVersion(path, want string) error {
	file, err := openRegularManagedFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, diagnosticLimit+1))
	if err != nil {
		return err
	}
	if len(data) > diagnosticLimit {
		return fmt.Errorf("package metadata exceeds %d bytes", diagnosticLimit)
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}
	if metadata.Version != want {
		return fmt.Errorf("version is %q, want %q", metadata.Version, want)
	}
	return nil
}

func prepareUpstreamFiles(
	runtime ReadyRuntime,
	trustedRoot string,
	workDir, stateDir, artifactDir string,
	viewport Viewport,
	proxyServer string,
) (upstreamPaths, error) {
	files, err := resolveRuntimeFiles(runtime, trustedRoot)
	if err != nil {
		return upstreamPaths{}, err
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("resolve UI test workdir: %w", err)
	}
	workDir, err = filepath.EvalSymlinks(workDir)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("canonicalize UI test workdir: %w", err)
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("resolve UI test state directory: %w", err)
	}
	artifactDir, err = filepath.Abs(artifactDir)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("resolve UI test artifact directory: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return upstreamPaths{}, fmt.Errorf("create UI test state directory: %w", err)
	}
	stateDir, err = filepath.EvalSymlinks(stateDir)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("canonicalize UI test state directory: %w", err)
	}
	if !pathWithin(workDir, stateDir) {
		return upstreamPaths{}, fmt.Errorf("UI test state directory escapes UI test workdir")
	}
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return upstreamPaths{}, fmt.Errorf("create UI test artifact directory: %w", err)
	}
	artifactDir, err = filepath.EvalSymlinks(artifactDir)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("canonicalize UI test artifact directory: %w", err)
	}
	if !pathWithin(workDir, artifactDir) {
		return upstreamPaths{}, fmt.Errorf("UI test artifact directory escapes UI test workdir")
	}

	var config upstreamConfig
	config.Browser.BrowserName = "chromium"
	config.Browser.Isolated = true
	config.Browser.LaunchOptions.Headless = true
	if err := validateLoopbackProxyServer(proxyServer); err != nil {
		return upstreamPaths{}, fmt.Errorf("UI test network proxy: %w", err)
	}
	config.Browser.LaunchOptions.Proxy.Server = proxyServer
	config.Browser.ContextOptions.Viewport = viewport
	config.OutputDir = artifactDir
	config.OutputMaxSize = 10 * 1024 * 1024
	config.Network.AllowedOrigins = []string{
		"http://localhost:*",
		"https://localhost:*",
		"http://127.0.0.1:*",
		"https://127.0.0.1:*",
		"http://[::1]:*",
		"https://[::1]:*",
	}
	// Playwright documents allowedOrigins as advisory. Launch proxy enforcement is
	// the network boundary; these patterns remain useful defense in depth.
	config.AllowUnrestrictedFileAccess = false
	data, err := json.Marshal(config)
	if err != nil {
		return upstreamPaths{}, fmt.Errorf("encode Playwright MCP config: %w", err)
	}
	data = append(data, '\n')
	configPath := filepath.Join(stateDir, "playwright-mcp.json")
	if err := writeAtomic0600(configPath, data); err != nil {
		return upstreamPaths{}, fmt.Errorf("write Playwright MCP config: %w", err)
	}
	storagePath := filepath.Join(stateDir, "storage-state.json")
	if err := writeEmptyStorageState(storagePath); err != nil {
		return upstreamPaths{}, fmt.Errorf("initialize browser storage state: %w", err)
	}
	return upstreamPaths{
		runtimeFiles: files,
		Config:       configPath,
		StorageState: storagePath,
	}, nil
}

func startUpstream(
	session *Session,
	runtime ReadyRuntime,
	trustedRoot string,
	logger *slog.Logger,
) (*Upstream, runtimeFiles, error) {
	networkProxy, err := startLoopbackForwardProxy(nil)
	if err != nil {
		return nil, runtimeFiles{}, err
	}
	return startUpstreamWithNetworkProxy(
		session,
		runtime,
		trustedRoot,
		logger,
		networkProxy,
	)
}

func startUpstreamWithNetworkProxy(
	session *Session,
	runtime ReadyRuntime,
	trustedRoot string,
	logger *slog.Logger,
	networkProxy *loopbackForwardProxy,
) (*Upstream, runtimeFiles, error) {
	if networkProxy == nil {
		return nil, runtimeFiles{}, fmt.Errorf("UI test network proxy is required")
	}
	keepNetworkProxy := true
	defer func() {
		if keepNetworkProxy {
			_ = networkProxy.Close()
		}
	}()
	paths, err := prepareUpstreamFiles(
		runtime,
		trustedRoot,
		session.opts.WorkDir,
		taskStateDir(session.opts.WorkDir, session.opts.TaskID),
		session.opts.ArtifactDir,
		session.opts.Config.Viewport,
		networkProxy.URL(),
	)
	if err != nil {
		return nil, runtimeFiles{}, err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, runtimeFiles{}, fmt.Errorf("Playwright MCP requires node on PATH: %w", err)
	}
	logPath := filepath.Join(session.opts.ArtifactDir, "upstream.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, runtimeFiles{}, fmt.Errorf("open Playwright MCP log: %w", err)
	}
	cmd := exec.Command(node, paths.MCPCLI, "--config", paths.Config, "--storage-state", paths.StorageState)
	cmd.Dir = session.opts.ArtifactDir
	cmd.Env = replaceEnvironment(os.Environ(), map[string]string{
		"PLAYWRIGHT_BROWSERS_PATH":                            paths.Browsers,
		"PLAYWRIGHT_DISABLE_FORCED_CHROMIUM_PROXIED_LOOPBACK": "",
	})
	cmd.Stderr = logFile
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = logFile.Close()
		return nil, runtimeFiles{}, fmt.Errorf("open Playwright MCP stdout: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		_ = logFile.Close()
		return nil, runtimeFiles{}, fmt.Errorf("open Playwright MCP stdin: %w", err)
	}
	controller, err := newPlatformProcessController(cmd)
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = logFile.Close()
		return nil, runtimeFiles{}, fmt.Errorf("create Playwright MCP process owner: %w", err)
	}
	process, err := startManagedCommand(session.registry, "browser", cmd, controller)
	_ = logFile.Close()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, runtimeFiles{}, fmt.Errorf("start Playwright MCP: %w", err)
	}
	upstream := newUpstream(stdout, stdin, process.stop)
	upstream.logger = logger
	upstream.networkProxy = networkProxy
	keepNetworkProxy = false
	return upstream, paths.runtimeFiles, nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func taskStateDir(workDir, taskID string) string {
	return filepath.Join(workDir, ".multica", "ui-test", taskID)
}

func taskArtifactDir(workDir, taskID string) string {
	return filepath.Join(workDir, ".multica", "artifacts", "ui-test", taskID)
}

func openRegularManagedFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("managed file is not regular")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("managed file changed while opening")
	}
	return file, nil
}
