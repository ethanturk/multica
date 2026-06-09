package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func piTestDaemon(piEnabled bool, configEnvVar string) *Daemon {
	cfg := testDetToolsCfg()
	cfg.Enabled = true
	cfg.PiAdapterEnabled = piEnabled
	cfg.PiConfigEnvVar = configEnvVar
	return &Daemon{cfg: Config{DetTools: cfg}}
}

func TestPreparePiToolPlane_WritesPerTaskConfigAndEnv(t *testing.T) {
	root := t.TempDir()
	d := piTestDaemon(true, "PI_MCP_CONFIG")
	agentEnv := map[string]string{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d.preparePiToolPlane("pi", root, "/work", nil, nil, agentEnv, logger)

	cfgPath := agentEnv["PI_MCP_CONFIG"]
	if cfgPath == "" {
		t.Fatal("PI_MCP_CONFIG env var was not set")
	}
	want := filepath.Join(root, "pi-mcp", "mcp.json")
	if cfgPath != want {
		t.Errorf("config path = %q, want %q (per-task, not global)", cfgPath, want)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	servers := parseServers(t, json.RawMessage(data))
	if _, ok := servers[dettoolsServerName]; !ok {
		t.Errorf("adapter config missing %q server: %s", dettoolsServerName, data)
	}
}

func TestPreparePiToolPlane_RespectsCustomEnvVar(t *testing.T) {
	d := piTestDaemon(true, "PI_CFG_OVERRIDE")
	agentEnv := map[string]string{}
	d.preparePiToolPlane("pi", t.TempDir(), "/work", nil, nil, agentEnv, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if agentEnv["PI_CFG_OVERRIDE"] == "" {
		t.Error("custom PiConfigEnvVar was not honored")
	}
	if _, ok := agentEnv["PI_MCP_CONFIG"]; ok {
		t.Error("default env var should not be set when overridden")
	}
}

func TestPreparePiToolPlane_NoopWhenDisabledOrNotPi(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Pi adapter disabled.
	dOff := piTestDaemon(false, "PI_MCP_CONFIG")
	env := map[string]string{}
	dOff.preparePiToolPlane("pi", t.TempDir(), "/work", nil, nil, env, logger)
	if len(env) != 0 {
		t.Errorf("disabled: expected no env mutation, got %v", env)
	}

	// Enabled, but provider is not pi.
	dOn := piTestDaemon(true, "PI_MCP_CONFIG")
	env = map[string]string{}
	dOn.preparePiToolPlane("claude", t.TempDir(), "/work", nil, nil, env, logger)
	if len(env) != 0 {
		t.Errorf("non-pi provider: expected no env mutation, got %v", env)
	}
}

func TestPreparePiToolPlane_SkipsWhenNoToolsAllowed(t *testing.T) {
	d := piTestDaemon(true, "PI_MCP_CONFIG")
	env := map[string]string{}
	// Agent profile that allows only a tool not in the daemon allowlist → empty.
	rc := json.RawMessage(`{"deterministic_tools":{"allowed_tools":["nonexistent"]}}`)
	d.preparePiToolPlane("pi", t.TempDir(), "/work", nil, rc, env, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(env) != 0 {
		t.Errorf("empty effective allowlist: expected no env mutation, got %v", env)
	}
}
