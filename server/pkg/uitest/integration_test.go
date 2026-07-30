//go:build uitest_integration

package uitest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUITestIntegrationApplication(t *testing.T) {
	if os.Getenv("MULTICA_UI_TEST_INTEGRATION_HELPER") != "1" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `<!doctype html>
<title>UI test fixture</title>
<button id="critical"></button>
<button id="open-popup" onclick="
document.getElementById('popup-attempted').textContent = 'external popup attempted';
window.open('https://example.com', '_blank');
">Open external popup</button>
<p id="popup-attempted">popup not attempted</p>
<p id="external-request">external request pending</p>
<script>
console.error("fixture console error");
fetch("/missing");
fetch("https://example.com/multica-ui-test-external-probe")
  .then(() => document.getElementById("external-request").textContent = "external request escaped")
  .catch(() => document.getElementById("external-request").textContent = "external request blocked");
</script>`)
	})
	mux.HandleFunc("/missing", func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "intentional first-party failure", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/redirect", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "https://example.com", http.StatusFound)
	})
	listener, err := net.Listen("tcp", os.Getenv("MULTICA_UI_TEST_INTEGRATION_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	if err := http.Serve(listener, mux); err != nil {
		t.Fatal(err)
	}
}

func TestUITestIntegrationRealBrowserPolicy(t *testing.T) {
	runtimeDir := strings.TrimSpace(os.Getenv("MULTICA_UI_TEST_RUNTIME_DIR"))
	if runtimeDir == "" {
		t.Skip("ready runtime required: run `multica ui-test install`, then set MULTICA_UI_TEST_RUNTIME_DIR to its runtimes/0.0.78 directory; this test never installs")
	}
	runtimeRoot := filepath.Dir(filepath.Dir(runtimeDir))
	readyRuntime := ReadyRuntime{Directory: runtimeDir}
	files, err := resolveRuntimeFiles(readyRuntime, runtimeRoot)
	if err != nil {
		t.Skipf("ready runtime required at MULTICA_UI_TEST_RUNTIME_DIR; this test never installs: %v", err)
	}

	address := reserveIntegrationAddress(t)
	t.Setenv("MULTICA_UI_TEST_INTEGRATION_HELPER", "1")
	t.Setenv("MULTICA_UI_TEST_INTEGRATION_ADDRESS", address)
	baseURL, _ := url.Parse("http://" + address)
	workDir := t.TempDir()
	session, err := NewSession(SessionOptions{
		WorkDir: workDir,
		TaskID:  "integration",
		Runtime: readyRuntime,
		Config: ResolvedConfig{
			StartCommand: integrationHelperCommand(),
			BaseURL:      baseURL,
			HealthURL:    baseURL,
			Viewport:     Viewport{Width: 1440, Height: 900},
		},
		StartupLimit: 20 * time.Second,
		SessionLimit: 2 * time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	upstream, _, err := startUpstream(
		session,
		readyRuntime,
		runtimeRoot,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		_ = session.Close()
		t.Fatalf("start pinned Playwright MCP (also validates config schema): %v", err)
	}
	var managedProcesses []integrationManagedProcessReference
	var chromiumProcesses []integrationProcessIdentity
	defer func() {
		if err := upstream.Close(); err != nil {
			t.Errorf("close upstream: %v", err)
		}
		if err := session.Close(); err != nil {
			t.Errorf("close session: %v", err)
		}
		if _, err := os.Stat(taskStateDir(workDir, "integration")); !os.IsNotExist(err) {
			t.Errorf("managed children/process metadata remain after close: %v", err)
		}
		for _, managed := range managedProcesses {
			deadline := time.Now().Add(2 * time.Second)
			for !integrationManagedProcessComplete(managed.Done) &&
				time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if !integrationManagedProcessComplete(managed.Done) {
				t.Errorf(
					"managed %s PID %d (%s, birth %s) did not complete after close",
					managed.Kind,
					managed.Identity.PID,
					managed.Identity.Executable,
					managed.Identity.BirthToken,
				)
			}
		}
		for _, process := range chromiumProcesses {
			deadline := time.Now().Add(2 * time.Second)
			alive, identityErr := integrationProcessStillAlive(
				process,
				integrationProcessIdentityByPID,
			)
			for identityErr == nil && alive && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
				alive, identityErr = integrationProcessStillAlive(
					process,
					integrationProcessIdentityByPID,
				)
			}
			if identityErr != nil {
				t.Errorf("check Chromium descendant PID %d after close: %v", process.PID, identityErr)
			} else if alive {
				t.Errorf(
					"Chromium descendant PID %d (%s, birth %s) remained alive after close",
					process.PID,
					process.Executable,
					process.BirthToken,
				)
			}
		}
	}()
	axe, err := readManagedAxe(files)
	if err != nil {
		t.Fatal(err)
	}
	proxy := newProxy(session, upstream, axe, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertIntegrationRPCOK(t, proxy.route(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"integration","version":"1"}}`),
	}))
	assertIntegrationToolOK(t, integrationToolCall(proxy, 2, "browser_navigate", map[string]any{"url": baseURL.String()}))
	managedProcesses, err = integrationManagedProcessReferences(session)
	if err != nil {
		t.Fatalf("capture managed process completion references: %v", err)
	}
	browserOwner := integrationBrowserOwnerIdentity(t, managedProcesses)
	screenshot := assertIntegrationToolOK(t, integrationToolCall(proxy, 3, "browser_take_screenshot", map[string]any{
		"type": "png", "filename": "integration.png",
	}))
	screenshotPath := filepath.Join(session.opts.ArtifactDir, "integration.png")
	if err := validateIntegrationScreenshot(screenshot, screenshotPath); err != nil {
		t.Fatalf("screenshot capture evidence: %v", err)
	}
	chromiumProcesses, err = integrationChromiumDescendantProcesses(browserOwner)
	if err != nil {
		t.Fatalf("capture live Chromium descendants: %v", err)
	}
	if len(chromiumProcesses) == 0 {
		t.Fatalf("browser owner PID %d had no live Chromium descendants", browserOwner.PID)
	}
	beforePopup := integrationToolCall(proxy, 4, "browser_snapshot", map[string]any{})
	assertIntegrationToolOK(t, beforePopup)
	popupRef := integrationSnapshotRef(t, beforePopup.Result)
	assertIntegrationToolOK(t, integrationToolCall(proxy, 5, "browser_click", map[string]any{
		"element": "Open external popup",
		"target":  popupRef,
	}))

	external := integrationToolCall(proxy, 6, "browser_navigate", map[string]any{"url": "https://example.com"})
	if external.Error == nil || external.Error.Data.Class != ErrorPolicy {
		t.Fatalf("external direct navigation was not blocked locally: %#v", external)
	}
	tabs := integrationToolCall(proxy, 7, "browser_tabs", map[string]any{"action": "list"})
	assertIntegrationToolOK(t, tabs)
	if strings.Contains(string(tabs.Result), "https://example.com") {
		t.Fatalf("external popup escaped browser network boundary: %s", tabs.Result)
	}
	if !strings.Contains(string(tabs.Result), address) {
		t.Fatalf("local fixture tab missing after blocked popup: %s", tabs.Result)
	}
	assertIntegrationToolOK(t, integrationToolCall(proxy, 8, "browser_tabs", map[string]any{
		"action": "select",
		"index":  0,
	}))
	snapshot := integrationToolCall(proxy, 9, "browser_snapshot", map[string]any{})
	assertIntegrationToolOK(t, snapshot)
	if !strings.Contains(integrationResultText(t, snapshot.Result), "external popup attempted") {
		t.Fatalf("fixture did not prove popup attempt before tab policy assertion: %s", snapshot.Result)
	}
	snapshotText := integrationResultText(t, snapshot.Result)
	if !strings.Contains(snapshotText, "external request blocked") ||
		strings.Contains(snapshotText, "external request escaped") {
		t.Fatalf("external request was not proven blocked: %s", snapshot.Result)
	}
	network := integrationToolCall(proxy, 10, "browser_network_requests", map[string]any{"includeStatic": false})
	assertIntegrationToolOK(t, network)
	if !strings.Contains(string(network.Result), "/missing") ||
		!strings.Contains(string(network.Result), "503") {
		t.Fatalf("explicit first-party 503 missing from network output: %s", network.Result)
	}
	console := integrationToolCall(proxy, 11, "browser_console_messages", map[string]any{"level": "error"})
	assertIntegrationToolOK(t, console)
	if !strings.Contains(string(console.Result), "fixture console error") {
		t.Fatalf("console error missing from output: %s", console.Result)
	}
	scan := integrationToolCall(proxy, 12, accessibilityScanTool.Name, map[string]any{})
	assertIntegrationToolOK(t, scan)
	if !strings.Contains(string(scan.Result), "button-name") {
		t.Fatalf("critical Axe fixture missing from scan: %s", scan.Result)
	}
	redirect := integrationToolCall(proxy, 13, "browser_navigate", map[string]any{"url": baseURL.String() + "/redirect"})
	assertIntegrationNetworkPolicyBlock(t, redirect, "example.com")
}

type integrationToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type integrationToolResult struct {
	Content []integrationToolContent `json:"content"`
	IsError bool                     `json:"isError,omitempty"`
}

func decodeIntegrationToolResult(response rpcResponse) (integrationToolResult, error) {
	if response.Error != nil {
		return integrationToolResult{}, fmt.Errorf(
			"JSON-RPC error %d: %s",
			response.Error.Code,
			response.Error.Message,
		)
	}
	if len(response.Result) == 0 {
		return integrationToolResult{}, fmt.Errorf("MCP tool result is missing")
	}
	var result integrationToolResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return integrationToolResult{}, fmt.Errorf("decode MCP tool result: %w", err)
	}
	if len(result.Content) == 0 {
		return integrationToolResult{}, fmt.Errorf("MCP tool result content is empty")
	}
	var hasContent bool
	for _, content := range result.Content {
		switch content.Type {
		case "text":
			hasContent = hasContent || strings.TrimSpace(content.Text) != ""
		case "image":
			hasContent = hasContent ||
				(content.Data != "" && strings.HasPrefix(content.MIMEType, "image/"))
		}
	}
	if !hasContent {
		return integrationToolResult{}, fmt.Errorf("MCP tool result has no supported content")
	}
	if result.IsError {
		return integrationToolResult{}, fmt.Errorf(
			"MCP tool returned isError: %s",
			boundedRPCString(integrationToolResultText(result)),
		)
	}
	return result, nil
}

func integrationToolResultText(result integrationToolResult) string {
	text := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
			text = append(text, content.Text)
		}
	}
	return strings.Join(text, "\n")
}

func validateIntegrationScreenshot(result integrationToolResult, path string) error {
	filename := strings.ToLower(filepath.Base(path))
	var captureEvidence bool
	for _, content := range result.Content {
		if content.Type == "image" && content.Data != "" && content.MIMEType == "image/png" {
			captureEvidence = true
		}
		text := strings.ToLower(content.Text)
		if content.Type == "text" &&
			strings.Contains(text, "screenshot") &&
			strings.Contains(text, filename) {
			captureEvidence = true
		}
	}
	if !captureEvidence {
		return fmt.Errorf("MCP result lacks screenshot capture evidence for %s", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open screenshot artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat screenshot artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 8 {
		return fmt.Errorf("screenshot artifact is not a non-empty regular PNG")
	}
	var header [8]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return fmt.Errorf("read screenshot PNG signature: %w", err)
	}
	if !bytes.Equal(header[:], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return fmt.Errorf("screenshot artifact has invalid PNG signature")
	}
	return nil
}

type integrationProcessIdentity struct {
	PID               int
	BirthToken        string
	CreatedAtUnixNano int64
	Executable        string
}

type integrationProcessLookup func(int) (integrationProcessIdentity, bool, error)

type integrationProcessTreeEntry struct {
	PID        int
	ParentPID  int
	Executable string
}

type integrationProcessTreeSnapshot struct {
	CaptureStartedAtUnixNano int64
	Entries                  map[int]integrationProcessTreeEntry
}

type integrationProcessSnapshotter func() (integrationProcessTreeSnapshot, error)

type integrationManagedProcessReference struct {
	Kind     string
	Identity integrationProcessIdentity
	Done     <-chan struct{}
}

func integrationManagedProcessReferences(
	session *Session,
) ([]integrationManagedProcessReference, error) {
	session.registry.mu.Lock()
	processes := make([]*managedProcess, 0, len(session.registry.processes))
	for _, process := range session.registry.processes {
		processes = append(processes, process)
	}
	session.registry.mu.Unlock()
	if len(processes) < 2 {
		return nil, fmt.Errorf(
			"managed process registry contained %d children, want app and browser owner",
			len(processes),
		)
	}
	references := make([]integrationManagedProcessReference, 0, len(processes))
	for _, process := range processes {
		if integrationManagedProcessComplete(process.done) {
			return nil, fmt.Errorf(
				"managed %s PID %d completed before identity capture",
				process.record.Kind,
				process.record.ChildPID,
			)
		}
		identity, found, err := integrationProcessIdentityByPID(process.record.ChildPID)
		if err != nil {
			return nil, fmt.Errorf(
				"capture managed %s PID %d identity: %w",
				process.record.Kind,
				process.record.ChildPID,
				err,
			)
		}
		if !found || identity.BirthToken == "" {
			return nil, fmt.Errorf(
				"managed %s PID %d identity is unavailable",
				process.record.Kind,
				process.record.ChildPID,
			)
		}
		if integrationManagedProcessComplete(process.done) {
			return nil, fmt.Errorf(
				"managed %s PID %d completed during identity capture",
				process.record.Kind,
				process.record.ChildPID,
			)
		}
		references = append(references, integrationManagedProcessReference{
			Kind:     process.record.Kind,
			Identity: identity,
			Done:     process.done,
		})
	}
	return references, nil
}

func integrationBrowserOwnerIdentity(
	t *testing.T,
	processes []integrationManagedProcessReference,
) integrationProcessIdentity {
	t.Helper()
	for _, process := range processes {
		if process.Kind == "browser" {
			return process.Identity
		}
	}
	t.Fatal("managed browser owner missing from captured process references")
	return integrationProcessIdentity{}
}

func integrationManagedProcessComplete(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func integrationProcessStillAlive(
	expected integrationProcessIdentity,
	lookup integrationProcessLookup,
) (bool, error) {
	if expected.BirthToken == "" {
		return false, fmt.Errorf("process PID %d has empty birth token", expected.PID)
	}
	current, found, err := lookup(expected.PID)
	if err != nil {
		return false, err
	}
	return found &&
		current.PID == expected.PID &&
		current.BirthToken == expected.BirthToken, nil
}

func integrationPinEnumeratedProcess(
	initial integrationProcessTreeEntry,
	captureStartedAtUnixNano int64,
	lookup integrationProcessLookup,
	snapshot integrationProcessSnapshotter,
) (integrationProcessIdentity, bool, error) {
	opened, found, err := lookup(initial.PID)
	if err != nil {
		return integrationProcessIdentity{}, false, err
	}
	if !found ||
		opened.PID != initial.PID ||
		opened.BirthToken == "" ||
		!strings.EqualFold(opened.Executable, initial.Executable) ||
		(opened.CreatedAtUnixNano > 0 &&
			opened.CreatedAtUnixNano > captureStartedAtUnixNano) {
		return integrationProcessIdentity{}, false, nil
	}
	currentSnapshot, err := snapshot()
	if err != nil {
		return integrationProcessIdentity{}, false, err
	}
	currentEntry, found := currentSnapshot.Entries[initial.PID]
	if !found ||
		currentEntry.ParentPID != initial.ParentPID ||
		!strings.EqualFold(currentEntry.Executable, initial.Executable) {
		return integrationProcessIdentity{}, false, nil
	}
	reopened, found, err := lookup(initial.PID)
	if err != nil {
		return integrationProcessIdentity{}, false, err
	}
	if !found ||
		reopened.PID != opened.PID ||
		reopened.BirthToken != opened.BirthToken ||
		!strings.EqualFold(reopened.Executable, currentEntry.Executable) {
		return integrationProcessIdentity{}, false, nil
	}
	return opened, true, nil
}

func TestUITestIntegrationProcessIdentityDoesNotTreatReusedPIDAsLeak(t *testing.T) {
	expected := integrationProcessIdentity{
		PID:        42,
		BirthToken: "kernel-birth-100.123456",
		Executable: "chromium",
	}
	tests := []struct {
		name      string
		current   integrationProcessIdentity
		found     bool
		wantAlive bool
	}{
		{
			name:      "same process remains alive",
			current:   expected,
			found:     true,
			wantAlive: true,
		},
		{
			name: "PID reused with another kernel birth token",
			current: integrationProcessIdentity{
				PID:        expected.PID,
				BirthToken: "kernel-birth-100.123457",
				Executable: expected.Executable,
			},
			found: true,
		},
		{
			name: "same process changed executable diagnostic",
			current: integrationProcessIdentity{
				PID:        expected.PID,
				BirthToken: expected.BirthToken,
				Executable: "unrelated",
			},
			found:     true,
			wantAlive: true,
		},
		{
			name: "process exited",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alive, err := integrationProcessStillAlive(
				expected,
				func(pid int) (integrationProcessIdentity, bool, error) {
					if pid != expected.PID {
						t.Fatalf("lookup PID = %d, want %d", pid, expected.PID)
					}
					return test.current, test.found, nil
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if alive != test.wantAlive {
				t.Fatalf("same identity alive = %v, want %v", alive, test.wantAlive)
			}
		})
	}
}

func TestUITestIntegrationCurrentProcessIdentityIsStable(t *testing.T) {
	expected, found, err := integrationProcessIdentityByPID(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !found || expected.PID != os.Getpid() || expected.BirthToken == "" {
		t.Fatalf("current process identity is unavailable: %#v", expected)
	}
	alive, err := integrationProcessStillAlive(
		expected,
		integrationProcessIdentityByPID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Fatalf("current process identity changed during lookup: %#v", expected)
	}
}

func TestUITestIntegrationRejectsPIDReusedBetweenSnapshotAndLookup(t *testing.T) {
	initial := integrationProcessTreeEntry{
		PID:        42,
		ParentPID:  7,
		Executable: "chrome.exe",
	}
	tests := []struct {
		name       string
		identities []integrationProcessIdentity
		wantFound  bool
	}{
		{
			name: "stable enumerated process",
			identities: []integrationProcessIdentity{
				{
					PID: initial.PID, BirthToken: "old",
					CreatedAtUnixNano: 50, Executable: initial.Executable,
				},
				{
					PID: initial.PID, BirthToken: "old",
					CreatedAtUnixNano: 50, Executable: initial.Executable,
				},
			},
			wantFound: true,
		},
		{
			name: "PID reused after enumeration before lookup",
			identities: []integrationProcessIdentity{
				{
					PID: initial.PID, BirthToken: "new",
					CreatedAtUnixNano: 150, Executable: initial.Executable,
				},
			},
		},
		{
			name: "PID reused between lookup and revalidation",
			identities: []integrationProcessIdentity{
				{
					PID: initial.PID, BirthToken: "old",
					CreatedAtUnixNano: 50, Executable: initial.Executable,
				},
				{
					PID: initial.PID, BirthToken: "new",
					CreatedAtUnixNano: 150, Executable: initial.Executable,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lookupCall := 0
			identity, found, err := integrationPinEnumeratedProcess(
				initial,
				100,
				func(pid int) (integrationProcessIdentity, bool, error) {
					if pid != initial.PID {
						t.Fatalf("lookup PID = %d, want %d", pid, initial.PID)
					}
					if lookupCall >= len(test.identities) {
						t.Fatal("unexpected identity lookup")
					}
					identity := test.identities[lookupCall]
					lookupCall++
					return identity, true, nil
				},
				func() (integrationProcessTreeSnapshot, error) {
					return integrationProcessTreeSnapshot{
						Entries: map[int]integrationProcessTreeEntry{
							initial.PID: initial,
						},
					}, nil
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if found != test.wantFound {
				t.Fatalf("pinned process found = %v, want %v", found, test.wantFound)
			}
			if found && identity.BirthToken != "old" {
				t.Fatalf("pinned birth token = %q, want old", identity.BirthToken)
			}
		})
	}
}

func TestUITestIntegrationManagedCompletionIgnoresReusedPID(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if !integrationManagedProcessComplete(done) {
		t.Fatal("completed managed process was reported running")
	}
	original := integrationProcessIdentity{PID: 42, BirthToken: "original"}
	alive, err := integrationProcessStillAlive(
		original,
		func(pid int) (integrationProcessIdentity, bool, error) {
			return integrationProcessIdentity{
				PID:        pid,
				BirthToken: "replacement",
			}, true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("replacement process with reused PID matched completed managed process")
	}

	running := make(chan struct{})
	if integrationManagedProcessComplete(running) {
		t.Fatal("running managed process was reported complete")
	}
}

func TestUITestIntegrationRejectsMCPToolErrorsAndEmptyContent(t *testing.T) {
	tests := []struct {
		name     string
		response rpcResponse
		wantErr  bool
	}{
		{
			name: "content success",
			response: rpcResponse{
				JSONRPC: "2.0",
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`),
			},
		},
		{
			name: "tool error result",
			response: rpcResponse{
				JSONRPC: "2.0",
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"navigation failed"}],"isError":true}`),
			},
			wantErr: true,
		},
		{
			name: "empty content",
			response: rpcResponse{
				JSONRPC: "2.0",
				Result:  json.RawMessage(`{"content":[]}`),
			},
			wantErr: true,
		},
		{
			name: "json rpc error",
			response: rpcResponse{
				JSONRPC: "2.0",
				Error: &rpcError{
					Code: rpcUpstreamError, Message: "failed",
					Data: rpcErrorData{Class: ErrorBrowser},
				},
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := decodeIntegrationToolResult(test.response)
			if (err != nil) != test.wantErr {
				t.Fatalf("decode error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && len(result.Content) != 1 {
				t.Fatalf("content count = %d, want one", len(result.Content))
			}
		})
	}
}

func TestUITestIntegrationScreenshotRequiresCaptureEvidenceAndPNGArtifact(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "integration.png")
	if err := os.WriteFile(artifact, append(
		[]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		[]byte("captured")...,
	), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := decodeIntegrationToolResult(rpcResponse{
		JSONRPC: "2.0",
		Result: json.RawMessage(
			`{"content":[{"type":"text","text":"Screenshot saved to integration.png"}]}`,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateIntegrationScreenshot(result, artifact); err != nil {
		t.Fatalf("valid screenshot rejected: %v", err)
	}

	result.Content[0].Text = "action completed"
	if err := validateIntegrationScreenshot(result, artifact); err == nil {
		t.Fatal("screenshot without capture result evidence accepted")
	}
	if err := os.WriteFile(artifact, []byte("not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	result.Content[0].Text = "Screenshot saved to integration.png"
	if err := validateIntegrationScreenshot(result, artifact); err == nil {
		t.Fatal("non-PNG screenshot artifact accepted")
	}
}

func integrationSnapshotRef(t *testing.T, result json.RawMessage) string {
	t.Helper()
	text := integrationResultText(t, result)
	match := regexp.MustCompile(`button "Open external popup" \[ref=([^\]]+)\]`).FindStringSubmatch(text)
	if len(match) != 2 {
		t.Fatalf("popup button ref missing from accessibility snapshot: %s", result)
	}
	return match[1]
}

func integrationResultText(t *testing.T, result json.RawMessage) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		t.Fatalf("decode MCP result: %v", err)
	}
	var parts []string
	var visit func(any)
	visit = func(item any) {
		switch item := item.(type) {
		case string:
			parts = append(parts, item)
		case []any:
			for _, child := range item {
				visit(child)
			}
		case map[string]any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(value)
	return strings.Join(parts, "\n")
}

func assertIntegrationNetworkPolicyBlock(t *testing.T, response rpcResponse, destination string) {
	t.Helper()
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	evidence := strings.ToLower(string(data))
	hasPolicyReason := strings.Contains(evidence, "blocked") ||
		strings.Contains(evidence, "not allowed") ||
		strings.Contains(evidence, "allowed origin") ||
		strings.Contains(evidence, "origin policy") ||
		strings.Contains(evidence, "err_tunnel_connection_failed")
	hasDestinationEvidence := strings.Contains(evidence, strings.ToLower(destination)) ||
		strings.Contains(evidence, "err_tunnel_connection_failed")
	if !hasDestinationEvidence || !hasPolicyReason {
		t.Fatalf("navigation lacked network policy rejection evidence: %s", data)
	}
}

func integrationToolCall(proxy *Proxy, id int, name string, arguments map[string]any) rpcResponse {
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": arguments})
	return proxy.route(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(strconv.Itoa(id)),
		Method:  "tools/call",
		Params:  params,
	})
}

func assertIntegrationRPCOK(t *testing.T, response rpcResponse) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("MCP response error: %#v", response.Error)
	}
}

func assertIntegrationToolOK(t *testing.T, response rpcResponse) integrationToolResult {
	t.Helper()
	result, err := decodeIntegrationToolResult(response)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func reserveIntegrationAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func integrationHelperCommand() string {
	executable := os.Args[0]
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("%q -test.run=^TestUITestIntegrationApplication$ --", executable)
	}
	return shellQuoteIntegration(executable) + " -test.run=^TestUITestIntegrationApplication$ --"
}

func shellQuoteIntegration(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
