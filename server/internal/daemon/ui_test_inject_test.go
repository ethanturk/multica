package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/uitest"
)

func TestUITestInjectionRequiresReadyRuntime(t *testing.T) {
	for _, status := range []string{
		uitest.StatusUnavailable,
		uitest.StatusInstalling,
		uitest.StatusBroken,
		uitest.StatusReady,
	} {
		t.Run(status, func(t *testing.T) {
			d := newUITestInjectionDaemon(status)
			got, injected := d.injectExecOptionsUITest(
				json.RawMessage(`{"mcpServers":{}}`),
				"claude",
				"/work/task",
				"task-1",
				discardUITestLogger(),
			)

			wantInjected := status == uitest.StatusReady
			if injected != wantInjected {
				t.Fatalf("injected = %v, want %v", injected, wantInjected)
			}
			_, present := parseServers(t, got)[uiTestServerName]
			if present != wantInjected {
				t.Fatalf("managed server present = %v, want %v", present, wantInjected)
			}
		})
	}
}

func TestUITestInjectionPreservesConfigAndUsesMinimalEnvironment(t *testing.T) {
	d := newUITestInjectionDaemon(uitest.StatusReady)
	original := json.RawMessage(`{
		"mcpServers":{"github":{"command":"github-mcp","args":["serve"]}},
		"other":{"enabled":true}
	}`)

	got, injected := d.injectExecOptionsUITest(
		original,
		"codex",
		"/work/task",
		"task-secret",
		discardUITestLogger(),
	)
	if !injected {
		t.Fatal("ready runtime was not injected")
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if _, ok := root["other"]; !ok {
		t.Fatal("unrelated top-level config was dropped")
	}
	servers := parseServers(t, got)
	if _, ok := servers["github"]; !ok {
		t.Fatal("existing MCP server was dropped")
	}

	var entry struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(servers[uiTestServerName], &entry); err != nil {
		t.Fatalf("decode managed entry: %v", err)
	}
	if entry.Command != "/resolved/multica" {
		t.Fatalf("command = %q, want resolved current binary", entry.Command)
	}
	if !reflect.DeepEqual(entry.Args, []string{"ui-test", "serve"}) {
		t.Fatalf("args = %v, want [ui-test serve]", entry.Args)
	}
	wantEnv := map[string]string{
		"MULTICA_UI_TEST_WORKDIR": "/work/task",
		"MULTICA_UI_TEST_TASK_ID": "task-secret",
		"MULTICA_UI_TEST_RUNTIME_DIR": filepath.Join(
			"/home/tester", ".multica", "profiles", "team", "ui-test",
			"runtimes", uitest.PlaywrightMCPVersion,
		),
	}
	if !reflect.DeepEqual(entry.Env, wantEnv) {
		t.Fatalf("env = %#v, want exact non-secret environment %#v", entry.Env, wantEnv)
	}
}

func TestUITestInjectionUserEntryWinsUnchanged(t *testing.T) {
	d := newUITestInjectionDaemon(uitest.StatusReady)
	original := json.RawMessage(`{"mcpServers":{"multica-ui-test":{"command":"user-browser","env":{"TOKEN":"keep"}}}}`)

	got, injected := d.injectExecOptionsUITest(
		original,
		"claude",
		"/work/task",
		"task-1",
		discardUITestLogger(),
	)
	if injected {
		t.Fatal("user-owned server collision reported as managed injection")
	}
	if string(got) != string(original) {
		t.Fatalf("user config changed:\n got %s\nwant %s", got, original)
	}
}

func TestUITestInjectionProviderSeams(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "dirge", "opencode", "hermes", "kimi", "kiro"} {
		t.Run(provider, func(t *testing.T) {
			d := newUITestInjectionDaemon(uitest.StatusReady)
			got, injected := d.injectExecOptionsUITest(nil, provider, "/work/task", "task-1", discardUITestLogger())
			if !injected {
				t.Fatalf("%s: managed server not injected", provider)
			}
			assertUITestWorkDir(t, got, "/work/task")
		})
	}

	d := newUITestInjectionDaemon(uitest.StatusReady)
	statusCalls := 0
	d.uiTestStatus = func(string) uitest.CapabilityStatus {
		statusCalls++
		return uitest.CapabilityStatus{Status: uitest.StatusReady}
	}
	original := json.RawMessage(`{"mcpServers":{}}`)
	got, injected := d.injectExecOptionsUITest(original, "gemini", "/work/task", "task-1", discardUITestLogger())
	if injected || string(got) != string(original) {
		t.Fatal("unsupported provider changed")
	}
	if statusCalls != 0 {
		t.Fatalf("unsupported provider probed runtime status %d times", statusCalls)
	}
}

func TestUITestInjectionOpenClawUsesCwdFallback(t *testing.T) {
	d := newUITestInjectionDaemon(uitest.StatusReady)
	got, injected := d.injectExecenvUITest(nil, "openclaw", "task-1", discardUITestLogger())
	if !injected {
		t.Fatal("OpenClaw managed server not injected")
	}
	assertUITestWorkDir(t, got, "")

	original := json.RawMessage(`{"mcpServers":{}}`)
	got, injected = d.injectExecenvUITest(original, "claude", "task-1", discardUITestLogger())
	if injected || string(got) != string(original) {
		t.Fatal("non-OpenClaw provider changed through pre-workdir seam")
	}
}

func TestUITestInjectionOpenClawPreservesNativeInheritance(t *testing.T) {
	for _, input := range []json.RawMessage{nil, json.RawMessage("null")} {
		t.Run(string(input), func(t *testing.T) {
			d := newUITestInjectionDaemon(uitest.StatusReady)
			setOpenclawNativeMCP(t, d, map[string]string{
				"native-browser": "native-browser-command",
			})

			got, injected := d.injectExecenvUITestWithBin(
				input, "openclaw", "/test/openclaw", "task-1", discardUITestLogger(),
			)
			if !injected {
				t.Fatal("managed UI server not injected")
			}
			servers := parseServers(t, got)
			assertMCPCommand(t, servers, "native-browser", "native-browser-command")
			assertMCPCommand(t, servers, uiTestServerName, "/resolved/multica")
		})
	}
}

func TestUITestInjectionOpenClawNativeCollisionsWin(t *testing.T) {
	d := newUITestInjectionDaemon(uitest.StatusReady)
	resolverCalls := setOpenclawNativeMCP(t, d, map[string]string{
		uiTestServerName:   "native-ui-command",
		dettoolsServerName: "native-tools-command",
	})
	d.cfg.DetTools = testDetToolsCfg()

	got := d.injectExecenvToolsWithBin(
		nil, "openclaw", "/test/openclaw", nil, nil, discardUITestLogger(),
	)
	got, injected := d.injectExecenvUITestWithBin(
		got, "openclaw", "/test/openclaw", "task-1", discardUITestLogger(),
	)
	if injected {
		t.Fatal("native UI collision reported as managed injection")
	}
	servers := parseServers(t, got)
	assertMCPCommand(t, servers, uiTestServerName, "native-ui-command")
	assertMCPCommand(t, servers, dettoolsServerName, "native-tools-command")
	if *resolverCalls != 1 {
		t.Fatalf("active config resolved %d times, want once for combined overlay", *resolverCalls)
	}
}

func TestUITestInjectionOpenClawCombinesNativeUITestAndDettools(t *testing.T) {
	for _, input := range []json.RawMessage{nil, json.RawMessage("null")} {
		t.Run(string(input), func(t *testing.T) {
			d := newUITestInjectionDaemon(uitest.StatusReady)
			resolverCalls := setOpenclawNativeMCP(t, d, map[string]string{
				"native-browser": "native-browser-command",
			})
			d.cfg.DetTools = testDetToolsCfg()

			got := d.injectExecenvToolsWithBin(
				input, "openclaw", "/test/openclaw", nil, nil, discardUITestLogger(),
			)
			got, injected := d.injectExecenvUITestWithBin(
				got, "openclaw", "/test/openclaw", "task-1", discardUITestLogger(),
			)
			if !injected {
				t.Fatal("managed UI server not injected")
			}
			servers := parseServers(t, got)
			assertMCPCommand(t, servers, "native-browser", "native-browser-command")
			if _, ok := servers[dettoolsServerName]; !ok {
				t.Fatal("deterministic tool server was dropped")
			}
			assertMCPCommand(t, servers, uiTestServerName, "/resolved/multica")
			if *resolverCalls != 1 {
				t.Fatalf("active config resolved %d times, want once for combined overlay", *resolverCalls)
			}
		})
	}
}

func TestUITestInjectionOpenClawResolverCallGating(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    string
		provider  string
		wantCalls int
	}{
		{name: "ready_openclaw", status: uitest.StatusReady, provider: "openclaw", wantCalls: 1},
		{name: "unready_openclaw", status: uitest.StatusInstalling, provider: "openclaw", wantCalls: 0},
		{name: "unsupported_provider", status: uitest.StatusReady, provider: "claude", wantCalls: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newUITestInjectionDaemon(test.status)
			calls := 0
			d.openclawMCPResolver = func(bin string) (map[string]json.RawMessage, error) {
				calls++
				if bin != "/test/openclaw" {
					t.Fatalf("resolver bin = %q", bin)
				}
				return map[string]json.RawMessage{}, nil
			}

			d.injectExecenvUITestWithBin(
				nil, test.provider, "/test/openclaw", "task-1", discardUITestLogger(),
			)
			if calls != test.wantCalls {
				t.Fatalf("resolver calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

func TestManagedOpenClawMCPResolverErrorsAreNonfatal(t *testing.T) {
	original := json.RawMessage("null")
	d := newUITestInjectionDaemon(uitest.StatusReady)
	d.openclawMCPResolver = func(string) (map[string]json.RawMessage, error) {
		return nil, errors.New("active config unavailable")
	}

	got, injected := d.injectExecenvUITestWithBin(
		original, "openclaw", "/test/openclaw", "task-1", discardUITestLogger(),
	)
	if injected || string(got) != string(original) {
		t.Fatal("UI resolver error changed task MCP config")
	}

	d.cfg.DetTools = testDetToolsCfg()
	got = d.injectExecenvToolsWithBin(
		original, "openclaw", "/test/openclaw", nil, nil, discardUITestLogger(),
	)
	if string(got) != string(original) {
		t.Fatal("dettools resolver error changed task MCP config")
	}
}

func TestUITestMCPMergeAcceptsNullMaps(t *testing.T) {
	for _, input := range []json.RawMessage{
		json.RawMessage("null"),
		json.RawMessage(`{"mcpServers":null}`),
	} {
		t.Run(string(input), func(t *testing.T) {
			got, injected, err := mergeUITestMCPConfig(
				input,
				"/resolved/multica",
				"",
				"task-1",
				"/runtime",
			)
			if err != nil {
				t.Fatalf("merge null config: %v", err)
			}
			if !injected {
				t.Fatal("managed UI server not injected")
			}
			assertMCPCommand(t, parseServers(t, got), uiTestServerName, "/resolved/multica")
		})
	}
}

func TestDetToolsMCPMergeAcceptsNullMaps(t *testing.T) {
	for _, input := range []json.RawMessage{
		json.RawMessage("null"),
		json.RawMessage(`{"mcpServers":null}`),
	} {
		t.Run(string(input), func(t *testing.T) {
			got, err := buildEffectiveMcpConfig(
				input,
				"/resolved/multica",
				"",
				"",
				testDetToolsCfg(),
				testAllowed(),
			)
			if err != nil {
				t.Fatalf("merge null config: %v", err)
			}
			assertMCPCommand(t, parseServers(t, got), dettoolsServerName, "/resolved/multica")
		})
	}
}

func TestUITestInjectionMalformedMergeIsNonfatal(t *testing.T) {
	d := newUITestInjectionDaemon(uitest.StatusReady)
	original := json.RawMessage(`{not-json`)

	got, injected := d.injectExecenvUITest(
		original,
		"openclaw",
		"task-1",
		discardUITestLogger(),
	)
	if injected {
		t.Fatal("malformed config reported as managed injection")
	}
	if string(got) != string(original) {
		t.Fatalf("malformed config changed:\n got %s\nwant %s", got, original)
	}
}

func TestUITestInjectionReadinessAndResolutionErrorsAreNonfatal(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Daemon)
	}{
		{
			name: "home",
			edit: func(d *Daemon) {
				d.uiTestHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
			},
		},
		{
			name: "executable",
			edit: func(d *Daemon) {
				d.uiTestExecutable = func() (string, error) { return "", errors.New("binary unavailable") }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newUITestInjectionDaemon(uitest.StatusReady)
			test.edit(d)
			original := json.RawMessage(`{"mcpServers":{}}`)
			got, injected := d.injectExecOptionsUITest(
				original, "claude", "/work/task", "task-1", discardUITestLogger(),
			)
			if injected || string(got) != string(original) {
				t.Fatal("injection error changed task MCP config")
			}
		})
	}
}

func TestUITestInjectionBrokenReadinessLogsNoStatusDetail(t *testing.T) {
	d := newUITestInjectionDaemon(uitest.StatusBroken)
	d.uiTestStatus = func(string) uitest.CapabilityStatus {
		return uitest.CapabilityStatus{
			Status: uitest.StatusBroken,
			Error:  "secret-cookie-value",
		}
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	_, injected := d.injectExecOptionsUITest(
		nil, "claude", "/work/task", "task-1", logger,
	)

	if injected {
		t.Fatal("broken runtime was injected")
	}
	if !strings.Contains(logs.String(), "runtime is broken") {
		t.Fatalf("missing non-secret readiness warning: %s", logs.String())
	}
	if strings.Contains(logs.String(), "secret-cookie-value") {
		t.Fatalf("readiness detail leaked into warning: %s", logs.String())
	}
}

func newUITestInjectionDaemon(status string) *Daemon {
	return &Daemon{
		cfg: Config{Profile: "team"},
		uiTestHomeDir: func() (string, error) {
			return "/home/tester", nil
		},
		uiTestStatus: func(string) uitest.CapabilityStatus {
			return uitest.CapabilityStatus{Status: status, Version: uitest.PlaywrightMCPVersion}
		},
		uiTestExecutable: func() (string, error) {
			return "/resolved/multica", nil
		},
		openclawMCPResolver: func(string) (map[string]json.RawMessage, error) {
			return map[string]json.RawMessage{}, nil
		},
	}
}

func assertUITestWorkDir(t *testing.T, config json.RawMessage, want string) {
	t.Helper()
	var entry struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(parseServers(t, config)[uiTestServerName], &entry); err != nil {
		t.Fatalf("decode managed entry: %v", err)
	}
	got, present := entry.Env["MULTICA_UI_TEST_WORKDIR"]
	if want == "" {
		if present {
			t.Fatalf("OpenClaw workdir = %q, want cwd fallback", got)
		}
		return
	}
	if !present || got != want {
		t.Fatalf("workdir = %q (present %v), want %q", got, present, want)
	}
}

func discardUITestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func setOpenclawNativeMCP(t *testing.T, d *Daemon, servers map[string]string) *int {
	t.Helper()
	calls := 0
	d.openclawMCPResolver = func(bin string) (map[string]json.RawMessage, error) {
		calls++
		if bin != "/test/openclaw" {
			t.Fatalf("resolver bin = %q", bin)
		}
		entries := make(map[string]json.RawMessage, len(servers))
		for name, command := range servers {
			entry, _ := json.Marshal(map[string]any{"command": command})
			entries[name] = entry
		}
		return entries, nil
	}
	return &calls
}

func assertMCPCommand(t *testing.T, servers map[string]json.RawMessage, name, want string) {
	t.Helper()
	var entry struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(servers[name], &entry); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if entry.Command != want {
		t.Fatalf("%s command = %q, want %q", name, entry.Command, want)
	}
}
