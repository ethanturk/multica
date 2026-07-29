package uitest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestUpstreamConfigUsesCanonicalManagedPathsAndFixedPolicy(t *testing.T) {
	fixture := newRuntimeFixture(t)
	workDir := t.TempDir()
	stateDir := taskStateDir(workDir, "task-1")
	artifactDir := taskArtifactDir(workDir, "task-1")
	paths, err := prepareUpstreamFiles(fixture.runtime, workDir, stateDir, artifactDir, Viewport{Width: 1440, Height: 900})
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
		workDir,
		stateDir,
		taskArtifactDir(workDir, "task-escape"),
		Viewport{Width: 1440, Height: 900},
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
	if _, err := resolveRuntimeFiles(fixture.runtime); err == nil || !strings.Contains(err.Error(), "Axe path") {
		t.Fatalf("resolveRuntimeFiles() error = %v, want Axe path rejection", err)
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
