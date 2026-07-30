package uitest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpstreamCorrelatesConcurrentRequestsWithOneReader(t *testing.T) {
	serverOutputReader, serverOutputWriter := io.Pipe()
	serverInputReader, serverInputWriter := io.Pipe()
	upstream := newUpstream(serverOutputReader, serverInputWriter, nil)
	defer upstream.Close()

	go func() {
		reader := bufio.NewReader(serverInputReader)
		var requests []rpcRequest
		for range 2 {
			frame, err := readRPCFrame(reader)
			if err != nil {
				return
			}
			var request rpcRequest
			if json.Unmarshal(frame, &request) != nil {
				return
			}
			requests = append(requests, request)
		}
		for index := len(requests) - 1; index >= 0; index-- {
			response := rpcResponse{
				JSONRPC: "2.0",
				ID:      requests[index].ID,
				Result:  json.RawMessage(`{"method":"` + requests[index].Method + `"}`),
			}
			_ = writeRPCFrame(serverOutputWriter, response)
		}
	}()

	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for _, method := range []string{"first", "second"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := upstream.request(context.Background(), rpcRequest{JSONRPC: "2.0", Method: method})
			if err != nil {
				errs <- err
				return
			}
			if !strings.Contains(string(response.Result), method) {
				errs <- errors.New("response correlated to wrong request")
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestUpstreamReturnsDeliveredResponseBeforeTrailingEOF(t *testing.T) {
	for range 100 {
		serverOutputReader, serverOutputWriter := io.Pipe()
		serverInputReader, serverInputWriter := io.Pipe()
		upstream := newUpstream(serverOutputReader, serverInputWriter, nil)

		go func() {
			defer serverInputReader.Close()
			frame, err := readRPCFrame(bufio.NewReader(serverInputReader))
			if err != nil {
				return
			}
			var request rpcRequest
			if json.Unmarshal(frame, &request) != nil {
				return
			}
			_ = writeRPCFrame(serverOutputWriter, rpcResponse{
				JSONRPC: "2.0",
				ID:      request.ID,
				Result:  json.RawMessage(`{"ok":true}`),
			})
			_ = serverOutputWriter.Close()
		}()

		response, err := upstream.request(context.Background(), rpcRequest{
			JSONRPC: "2.0",
			Method:  "respond-then-close",
		})
		_ = upstream.Close()
		if err != nil {
			t.Fatalf("delivered response lost to trailing EOF: %v", err)
		}
		if string(response.Result) != `{"ok":true}` {
			t.Fatalf("response result = %s", response.Result)
		}
	}
}

func TestUpstreamCleansPendingRequestOnCancellation(t *testing.T) {
	serverOutputReader, serverOutputWriter := io.Pipe()
	serverInputReader, serverInputWriter := io.Pipe()
	upstream := newUpstream(serverOutputReader, serverInputWriter, nil)
	defer serverOutputWriter.Close()
	defer upstream.Close()
	go func() {
		_, _ = io.Copy(io.Discard, serverInputReader)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := upstream.request(ctx, rpcRequest{JSONRPC: "2.0", Method: "never"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request error = %v, want deadline", err)
	}
	upstream.mu.Lock()
	pending := len(upstream.pending)
	upstream.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending requests = %d, want zero", pending)
	}
}

type blockingWriteCloser struct {
	once   sync.Once
	closed chan struct{}
}

func (w *blockingWriteCloser) Write([]byte) (int, error) {
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingWriteCloser) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

func TestUpstreamDeadlineInterruptsBlockedWrite(t *testing.T) {
	serverOutputReader, serverOutputWriter := io.Pipe()
	writer := &blockingWriteCloser{closed: make(chan struct{})}
	upstream := newUpstream(serverOutputReader, writer, nil)
	defer serverOutputWriter.Close()
	defer upstream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := upstream.request(ctx, rpcRequest{JSONRPC: "2.0", Method: "blocked-write"})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("request error = %v, want deadline", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("request deadline did not interrupt blocked upstream write")
	}
}

func TestUpstreamRejectsOversizedFrameAndFailsPending(t *testing.T) {
	serverOutputReader, serverOutputWriter := io.Pipe()
	serverInputReader, serverInputWriter := io.Pipe()
	upstream := newUpstream(serverOutputReader, serverInputWriter, nil)
	defer upstream.Close()

	go func() {
		_, _ = readRPCFrame(bufio.NewReader(serverInputReader))
		_, _ = io.WriteString(serverOutputWriter, strings.Repeat("x", maxRPCFrameBytes+1)+"\n")
	}()
	_, err := upstream.request(context.Background(), rpcRequest{JSONRPC: "2.0", Method: "oversized"})
	if !errors.Is(err, errRPCFrameTooLarge) {
		t.Fatalf("request error = %v, want frame-too-large", err)
	}
}

func TestUpstreamRejectsServerInitiatedRequestAndForwardsNotification(t *testing.T) {
	serverOutputReader, serverOutputWriter := io.Pipe()
	serverInputReader, serverInputWriter := io.Pipe()
	upstream := newUpstream(serverOutputReader, serverInputWriter, nil)
	defer upstream.Close()

	if err := writeRPCFrame(serverOutputWriter, rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`"server-request"`), Method: "sampling/createMessage",
	}); err != nil {
		t.Fatal(err)
	}
	responseFrame, err := readRPCFrame(bufio.NewReader(serverInputReader))
	if err != nil {
		t.Fatal(err)
	}
	var response rpcResponse
	if err := json.Unmarshal(responseFrame, &response); err != nil {
		t.Fatal(err)
	}
	if string(response.ID) != `"server-request"` || response.Error == nil || response.Error.Code != rpcMethodNotFound {
		t.Fatalf("server request rejection = %#v", response)
	}

	if err := writeRPCFrame(serverOutputWriter, rpcRequest{
		JSONRPC: "2.0", Method: "notifications/message", Params: json.RawMessage(`{"level":"info"}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case notification := <-upstream.eventStream():
		if notification.Method != "notifications/message" {
			t.Fatalf("notification method = %q", notification.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("notification not forwarded")
	}
}

func TestUpstreamReaderAloneClosesNotificationStreamAfterFailure(t *testing.T) {
	serverOutputReader, serverOutputWriter := io.Pipe()
	serverInputReader, serverInputWriter := io.Pipe()
	upstream := newUpstream(serverOutputReader, serverInputWriter, nil)
	defer serverInputReader.Close()
	defer upstream.Close()

	writeNotification := func(sequence int) {
		t.Helper()
		if err := writeRPCFrame(serverOutputWriter, rpcRequest{
			JSONRPC: "2.0",
			Method:  "notifications/message",
			Params:  json.RawMessage(`{"sequence":` + strconv.Itoa(sequence) + `}`),
		}); err != nil {
			t.Fatalf("write notification %d: %v", sequence, err)
		}
	}
	writeNotification(1)
	if event := <-upstream.eventStream(); !strings.Contains(string(event.Params), `"sequence":1`) {
		t.Fatalf("first notification = %s", event.Params)
	}

	upstream.fail(errors.New("forced concurrent failure"))
	writeNotification(2)
	if err := serverOutputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-upstream.eventStream():
		if ok {
			t.Fatal("notification stream remained open after reader exit")
		}
	case <-time.After(time.Second):
		t.Fatal("notification stream did not close after reader exit")
	}
}

func TestStartUpstreamRunsMCPInTaskArtifactDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake node wrapper uses a POSIX shell")
	}
	fixture := newRuntimeFixture(t)
	workDir := t.TempDir()
	config, err := loadManifest([]byte(`{
		"start":"unused",
		"url":"http://127.0.0.1:1",
		"health":"/"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(SessionOptions{
		WorkDir: workDir,
		TaskID:  "artifact-cwd",
		Runtime: fixture.runtime,
		Config:  config,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close session: %v", err)
		}
	})

	cwdPath := filepath.Join(t.TempDir(), "cwd")
	binDir := t.TempDir()
	nodePath := filepath.Join(binDir, "node")
	if err := os.WriteFile(nodePath, []byte(
		"#!/bin/sh\npwd > \"$MULTICA_UI_TEST_FAKE_NODE_CWD\"\nexec /bin/sleep 60\n",
	), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("MULTICA_UI_TEST_FAKE_NODE_CWD", cwdPath)

	upstream, _, err := startUpstream(session, fixture.runtime, fixture.trustedRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile(filepath.Join(taskStateDir(workDir, "artifact-cwd"), "playwright-mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var launchConfig struct {
		Browser struct {
			LaunchOptions struct {
				Proxy struct {
					Server string `json:"server"`
				} `json:"proxy"`
			} `json:"launchOptions"`
		} `json:"browser"`
	}
	if err := json.Unmarshal(configData, &launchConfig); err != nil {
		t.Fatal(err)
	}
	proxyAddress := strings.TrimPrefix(launchConfig.Browser.LaunchOptions.Proxy.Server, "http://")
	proxyConnection, err := net.Dial("tcp", proxyAddress)
	if err != nil {
		t.Fatalf("launch proxy unavailable while upstream runs: %v", err)
	}
	_ = proxyConnection.Close()
	t.Cleanup(func() {
		if err := upstream.Close(); err != nil {
			t.Errorf("close upstream: %v", err)
		}
	})

	deadline := time.Now().Add(time.Second)
	var actual []byte
	for time.Now().Before(deadline) {
		actual, err = os.ReadFile(cwdPath)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read fake node cwd: %v", err)
	}
	want, err := filepath.EvalSymlinks(session.opts.ArtifactDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(actual)); got != want {
		t.Fatalf("Playwright MCP cwd = %q, want task artifact directory %q", got, want)
	}
	if err := upstream.Close(); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("tcp", proxyAddress, 50*time.Millisecond); err == nil {
		_ = connection.Close()
		t.Fatal("launch proxy remained reachable after upstream close")
	}
}

func TestStartUpstreamClosesNetworkProxyWhenPreparationFails(t *testing.T) {
	fixture := newRuntimeFixture(t)
	workDir := t.TempDir()
	config, err := loadManifest([]byte(`{
		"start":"unused",
		"url":"http://127.0.0.1:1",
		"health":"/"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(SessionOptions{
		WorkDir: workDir,
		TaskID:  "proxy-start-error",
		Runtime: fixture.runtime,
		Config:  config,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	networkProxy, err := startLoopbackForwardProxy(nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress := strings.TrimPrefix(networkProxy.URL(), "http://")
	_, _, err = startUpstreamWithNetworkProxy(
		session,
		ReadyRuntime{Directory: t.TempDir()},
		fixture.trustedRoot,
		nil,
		networkProxy,
	)
	if err == nil {
		t.Fatal("startUpstreamWithNetworkProxy succeeded with untrusted runtime")
	}
	if connection, dialErr := net.DialTimeout("tcp", proxyAddress, 50*time.Millisecond); dialErr == nil {
		_ = connection.Close()
		t.Fatal("launch proxy remained reachable after upstream preparation failure")
	}
}

func TestUpstreamConfigUsesCanonicalManagedPathsAndFixedPolicy(t *testing.T) {
	fixture := newRuntimeFixture(t)
	workDir := t.TempDir()
	stateDir := taskStateDir(workDir, "task-1")
	artifactDir := taskArtifactDir(workDir, "task-1")
	paths, err := prepareUpstreamFiles(
		fixture.runtime,
		fixture.trustedRoot,
		workDir,
		stateDir,
		artifactDir,
		Viewport{Width: 1440, Height: 900},
		"http://127.0.0.1:43210",
	)
	if err != nil {
		t.Fatalf("prepareUpstreamFiles() error = %v", err)
	}
	canonicalCLI, _ := filepath.EvalSymlinks(fixture.cliPath)
	canonicalAxe, _ := filepath.EvalSymlinks(fixture.axePath)
	if paths.MCPCLI != canonicalCLI || paths.Axe != canonicalAxe {
		t.Fatalf("managed paths = %#v, want manifest paths", paths)
	}
	data, err := io.ReadAll(mustOpen(t, paths.Config))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
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
		OutputDir                   string `json:"outputDir"`
		OutputMaxSize               int    `json:"outputMaxSize"`
		AllowUnrestrictedFileAccess bool   `json:"allowUnrestrictedFileAccess"`
		Network                     struct {
			AllowedOrigins []string `json:"allowedOrigins"`
		} `json:"network"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Browser.BrowserName != "chromium" || !config.Browser.Isolated ||
		!config.Browser.LaunchOptions.Headless ||
		config.Browser.LaunchOptions.Proxy.Server != "http://127.0.0.1:43210" ||
		config.Browser.ContextOptions.Viewport != (Viewport{Width: 1440, Height: 900}) {
		t.Fatalf("browser config = %#v", config.Browser)
	}
	canonicalArtifactDir, _ := filepath.EvalSymlinks(artifactDir)
	if config.OutputDir != canonicalArtifactDir || config.OutputMaxSize != 10*1024*1024 ||
		config.AllowUnrestrictedFileAccess {
		t.Fatalf("output/file policy = %#v", config)
	}
	wantOrigins := "http://localhost:*,https://localhost:*,http://127.0.0.1:*,https://127.0.0.1:*,http://[::1]:*,https://[::1]:*"
	if strings.Join(config.Network.AllowedOrigins, ",") != wantOrigins {
		t.Fatalf("allowed origins = %v", config.Network.AllowedOrigins)
	}
}

func TestUpstreamConfigRejectsTaskPathSymlinkEscape(t *testing.T) {
	fixture := newRuntimeFixture(t)
	workDir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".multica", "ui-test"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := taskStateDir(workDir, "task-escape")
	if err := os.Symlink(outside, stateDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := prepareUpstreamFiles(
		fixture.runtime,
		fixture.trustedRoot,
		workDir,
		stateDir,
		taskArtifactDir(workDir, "task-escape"),
		Viewport{Width: 1440, Height: 900},
		"http://127.0.0.1:43210",
	)
	if err == nil || !strings.Contains(err.Error(), "escapes UI test workdir") {
		t.Fatalf("prepareUpstreamFiles() error = %v, want symlink escape rejection", err)
	}
}

func TestUpstreamManagedRuntimeRejectsEscapingManifestPath(t *testing.T) {
	fixture := newRuntimeFixture(t)
	manifest := fixture.manifest
	manifest.AxePath = "../outside.js"
	writeReadyManifest(t, fixture.runtime.Directory, manifest)
	if _, err := resolveRuntimeFiles(fixture.runtime, fixture.trustedRoot); err == nil || !strings.Contains(err.Error(), "Axe") {
		t.Fatalf("resolveRuntimeFiles() error = %v, want Axe path rejection", err)
	}
}

func TestUpstreamManagedRuntimeRequiresExactPinnedDirectoryUnderTrustedRoot(t *testing.T) {
	fixture := newRuntimeFixture(t)
	if _, err := resolveRuntimeFiles(fixture.runtime, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "trusted UI test root") {
		t.Fatalf("resolveRuntimeFiles() error = %v, want trusted-root rejection", err)
	}
}

func TestUpstreamManagedRuntimeRejectsAlternateSelfAssertedCLIPath(t *testing.T) {
	fixture := newRuntimeFixture(t)
	alternate := filepath.Join(fixture.runtime.Directory, "alternate", "cli.js")
	if err := os.MkdirAll(filepath.Dir(alternate), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alternate, []byte("malicious"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fixture.manifest
	manifest.MCPCLIPath = "alternate/cli.js"
	writeReadyManifest(t, fixture.runtime.Directory, manifest)
	if _, err := resolveRuntimeFiles(fixture.runtime, fixture.trustedRoot); err == nil ||
		!strings.Contains(err.Error(), "fixed managed path") {
		t.Fatalf("resolveRuntimeFiles() error = %v, want fixed CLI path rejection", err)
	}
}

func TestUpstreamManagedRuntimeRejectsSelfAssertedBrowserOutsideManagedDirectory(t *testing.T) {
	fixture := newRuntimeFixture(t)
	alternate := filepath.Join(fixture.runtime.Directory, "alternate", "browser")
	if err := os.MkdirAll(filepath.Dir(alternate), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alternate, []byte("malicious"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fixture.manifest
	manifest.BrowserPath = "alternate/browser"
	writeReadyManifest(t, fixture.runtime.Directory, manifest)
	if _, err := resolveRuntimeFiles(fixture.runtime, fixture.trustedRoot); err == nil ||
		!strings.Contains(err.Error(), "Chromium") {
		t.Fatalf("resolveRuntimeFiles() error = %v, want managed Chromium path rejection", err)
	}
}

func TestUpstreamManagedRuntimeRejectsSymlinkedReadyManifest(t *testing.T) {
	fixture := newRuntimeFixture(t)
	readyPath := filepath.Join(fixture.runtime.Directory, "ready.json")
	external := filepath.Join(t.TempDir(), "ready.json")
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(readyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, readyPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveRuntimeFiles(fixture.runtime, fixture.trustedRoot); err == nil ||
		!strings.Contains(err.Error(), "ready manifest") {
		t.Fatalf("resolveRuntimeFiles() error = %v, want symlinked ready rejection", err)
	}
}

func mustOpen(t *testing.T, path string) io.Reader {
	t.Helper()
	file, err := openRegularManagedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
