//go:build uitest_integration

package uitest

import (
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
<script>
console.error("fixture console error");
fetch("/missing");
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
	defer func() {
		session.registry.mu.Lock()
		childPIDs := make([]int, 0, len(session.registry.processes))
		for pid := range session.registry.processes {
			childPIDs = append(childPIDs, pid)
		}
		session.registry.mu.Unlock()
		if len(childPIDs) < 2 {
			t.Errorf("managed process registry contained %d children before close, want app and browser owner", len(childPIDs))
		}
		if err := upstream.Close(); err != nil {
			t.Errorf("close upstream: %v", err)
		}
		if err := session.Close(); err != nil {
			t.Errorf("close session: %v", err)
		}
		if _, err := os.Stat(taskStateDir(workDir, "integration")); !os.IsNotExist(err) {
			t.Errorf("managed children/process metadata remain after close: %v", err)
		}
		for _, pid := range childPIDs {
			deadline := time.Now().Add(2 * time.Second)
			for platformProcessAlive(pid) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if platformProcessAlive(pid) {
				t.Errorf("managed child PID %d remained alive after close", pid)
			}
		}
	}()
	axe, err := readManagedAxe(files)
	if err != nil {
		t.Fatal(err)
	}
	proxy := newProxy(session, upstream, axe, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assertIntegrationOK(t, proxy.route(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"integration","version":"1"}}`),
	}))
	assertIntegrationOK(t, integrationToolCall(proxy, 2, "browser_navigate", map[string]any{"url": baseURL.String()}))
	assertIntegrationOK(t, integrationToolCall(proxy, 3, "browser_take_screenshot", map[string]any{
		"type": "png", "filename": "integration.png",
	}))
	beforePopup := integrationToolCall(proxy, 4, "browser_snapshot", map[string]any{})
	assertIntegrationOK(t, beforePopup)
	popupRef := integrationSnapshotRef(t, beforePopup.Result)
	assertIntegrationOK(t, integrationToolCall(proxy, 5, "browser_click", map[string]any{
		"element": "Open external popup",
		"ref":     popupRef,
	}))

	external := integrationToolCall(proxy, 6, "browser_navigate", map[string]any{"url": "https://example.com"})
	if external.Error == nil || external.Error.Data.Class != ErrorPolicy {
		t.Fatalf("external direct navigation was not blocked locally: %#v", external)
	}
	redirect := integrationToolCall(proxy, 7, "browser_navigate", map[string]any{"url": baseURL.String() + "/redirect"})
	assertIntegrationNetworkPolicyBlock(t, redirect, "example.com")
	tabs := integrationToolCall(proxy, 8, "browser_tabs", map[string]any{"action": "list"})
	assertIntegrationOK(t, tabs)
	if strings.Contains(string(tabs.Result), "https://example.com") {
		t.Fatalf("external popup escaped allowedOrigins: %s", tabs.Result)
	}
	if !strings.Contains(string(tabs.Result), address) {
		t.Fatalf("local fixture tab missing after blocked popup: %s", tabs.Result)
	}
	snapshot := integrationToolCall(proxy, 9, "browser_snapshot", map[string]any{})
	assertIntegrationOK(t, snapshot)
	if !strings.Contains(integrationResultText(t, snapshot.Result), "external popup attempted") {
		t.Fatalf("fixture did not prove popup attempt before tab policy assertion: %s", snapshot.Result)
	}
	network := integrationToolCall(proxy, 10, "browser_network_requests", map[string]any{"includeStatic": false})
	assertIntegrationOK(t, network)
	if !strings.Contains(string(network.Result), "/missing") ||
		!strings.Contains(string(network.Result), "503") {
		t.Fatalf("explicit first-party 503 missing from network output: %s", network.Result)
	}
	console := integrationToolCall(proxy, 11, "browser_console_messages", map[string]any{"level": "error"})
	assertIntegrationOK(t, console)
	if !strings.Contains(string(console.Result), "fixture console error") {
		t.Fatalf("console error missing from output: %s", console.Result)
	}
	scan := integrationToolCall(proxy, 12, accessibilityScanTool.Name, map[string]any{})
	assertIntegrationOK(t, scan)
	if !strings.Contains(string(scan.Result), "button-name") {
		t.Fatalf("critical Axe fixture missing from scan: %s", scan.Result)
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
		strings.Contains(evidence, "origin policy")
	if !strings.Contains(evidence, strings.ToLower(destination)) || !hasPolicyReason {
		t.Fatalf("navigation lacked destination-specific network policy rejection evidence: %s", data)
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

func assertIntegrationOK(t *testing.T, response rpcResponse) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("MCP response error: %#v", response.Error)
	}
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
