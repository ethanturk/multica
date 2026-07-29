package uitest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAxeCallUsesOnlyPinnedScriptAndFixedEvaluateFunction(t *testing.T) {
	upstream := newFakeProxyUpstream()
	upstream.responses["tools/call"] = rpcResponse{
		JSONRPC: "2.0",
		Result:  json.RawMessage(`{"content":[{"type":"text","text":"[{\"id\":\"button-name\"}]"}]}`),
	}
	session := &fakeProxySession{}
	script := []byte(`globalThis.axe={run:()=>Promise.resolve({violations:[]})};`)
	responses := runProxy(t, session, upstream, script,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"browser_accessibility_scan","arguments":{}}}`+"\n")
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("response = %#v", responses)
	}
	calls := upstream.calls()
	if len(calls) != 1 {
		t.Fatalf("upstream calls = %d, want one", len(calls))
	}
	var call toolCallParams
	if err := json.Unmarshal(calls[0].Params, &call); err != nil {
		t.Fatal(err)
	}
	if call.Name != "browser_evaluate" {
		t.Fatalf("translated tool = %q, want browser_evaluate", call.Name)
	}
	var arguments struct {
		Function string `json:"function"`
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(arguments.Function, string(script)) ||
		!strings.Contains(arguments.Function, "failureSummary") ||
		!strings.Contains(arguments.Function, "helpUrl") ||
		strings.Contains(arguments.Function, "html") {
		t.Fatalf("fixed Axe function missing minimization policy: %s", arguments.Function)
	}
}

func TestAxeRejectsEveryAgentSuppliedArgument(t *testing.T) {
	upstream := newFakeProxyUpstream()
	responses := runProxy(t, &fakeProxySession{}, upstream, []byte("fixed"),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"browser_accessibility_scan","arguments":{"javascript":"steal()"}}}`+"\n")
	if len(responses) != 1 || responses[0].Error == nil ||
		responses[0].Error.Data.Class != ErrorPolicy {
		t.Fatalf("response = %#v, want policy error", responses)
	}
	if len(upstream.calls()) != 0 {
		t.Fatal("injected Axe arguments reached upstream")
	}
}

func TestAxeDescriptorHasClosedEmptySchema(t *testing.T) {
	var schema struct {
		Type                 string         `json:"type"`
		Properties           map[string]any `json:"properties"`
		AdditionalProperties bool           `json:"additionalProperties"`
	}
	if err := json.Unmarshal(accessibilityScanTool.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if accessibilityScanTool.Name != "browser_accessibility_scan" ||
		schema.Type != "object" || len(schema.Properties) != 0 || schema.AdditionalProperties {
		t.Fatalf("descriptor = %#v, schema = %#v", accessibilityScanTool, schema)
	}
}

func TestAxeManagedReadIsBoundedAndRejectsChangedFile(t *testing.T) {
	fixture := newRuntimeFixture(t)
	if err := os.WriteFile(fixture.axePath, []byte("fixed axe"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := resolveRuntimeFiles(fixture.runtime)
	if err != nil {
		t.Fatal(err)
	}
	script, err := readManagedAxe(files)
	if err != nil || string(script) != "fixed axe" {
		t.Fatalf("readManagedAxe() = %q, %v", script, err)
	}
	if err := os.WriteFile(fixture.axePath, []byte(strings.Repeat("x", maxAxeScriptBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readManagedAxe(files); err == nil {
		t.Fatal("oversized Axe script accepted")
	}
}

type runtimeFixture struct {
	runtime  ReadyRuntime
	manifest readyManifest
	cliPath  string
	axePath  string
}

func newRuntimeFixture(t *testing.T) runtimeFixture {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "runtimes", PlaywrightMCPVersion)
	cliPath := filepath.Join(directory, "node_modules", "@playwright", "mcp", "cli.js")
	axePath := filepath.Join(directory, "node_modules", "axe-core", "axe.min.js")
	playwrightPath := filepath.Join(directory, "node_modules", ".bin", "playwright")
	browserPath := filepath.Join(directory, "browsers", "chromium", "chrome")
	for _, path := range []string{cliPath, axePath, playwrightPath, browserPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := readyManifest{
		MCPVersion:     PlaywrightMCPVersion,
		AxeVersion:     AxeCoreVersion,
		Browser:        "chromium",
		InstalledAt:    time.Now(),
		MCPCLIPath:     "node_modules/@playwright/mcp/cli.js",
		AxePath:        "node_modules/axe-core/axe.min.js",
		PlaywrightPath: "node_modules/.bin/playwright",
		BrowserPath:    "browsers/chromium/chrome",
	}
	writeReadyManifest(t, directory, manifest)
	return runtimeFixture{
		runtime:  ReadyRuntime{Directory: directory},
		manifest: manifest,
		cliPath:  cliPath,
		axePath:  axePath,
	}
}

func writeReadyManifest(t *testing.T, directory string, manifest readyManifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "ready.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
