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
<script>
console.error("fixture console error");
fetch("/missing");
window.open("https://example.com", "_blank");
</script>`)
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
	readyRuntime := ReadyRuntime{Directory: runtimeDir}
	files, err := resolveRuntimeFiles(readyRuntime)
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
	upstream, _, err := startUpstream(session, readyRuntime, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		_ = session.Close()
		t.Fatalf("start pinned Playwright MCP (also validates config schema): %v", err)
	}
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

	external := integrationToolCall(proxy, 4, "browser_navigate", map[string]any{"url": "https://example.com"})
	if external.Error == nil || external.Error.Data.Class != ErrorPolicy {
		t.Fatalf("external direct navigation was not blocked locally: %#v", external)
	}
	redirect := integrationToolCall(proxy, 5, "browser_navigate", map[string]any{"url": baseURL.String() + "/redirect"})
	if redirect.Error == nil && !strings.Contains(strings.ToLower(string(redirect.Result)), "blocked") {
		t.Fatalf("external redirect was not blocked by allowedOrigins: %#v", redirect)
	}
	tabs := integrationToolCall(proxy, 6, "browser_tabs", map[string]any{"action": "list"})
	assertIntegrationOK(t, tabs)
	if strings.Contains(string(tabs.Result), "https://example.com") {
		t.Fatalf("external popup escaped allowedOrigins: %s", tabs.Result)
	}
	network := integrationToolCall(proxy, 7, "browser_network_requests", map[string]any{"includeStatic": false})
	assertIntegrationOK(t, network)
	if !strings.Contains(string(network.Result), "/missing") {
		t.Fatalf("first-party failed request missing from network output: %s", network.Result)
	}
	console := integrationToolCall(proxy, 8, "browser_console_messages", map[string]any{"level": "error"})
	assertIntegrationOK(t, console)
	if !strings.Contains(string(console.Result), "fixture console error") {
		t.Fatalf("console error missing from output: %s", console.Result)
	}
	scan := integrationToolCall(proxy, 9, accessibilityScanTool.Name, map[string]any{})
	assertIntegrationOK(t, scan)
	if !strings.Contains(string(scan.Result), "button-name") {
		t.Fatalf("critical Axe fixture missing from scan: %s", scan.Result)
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
