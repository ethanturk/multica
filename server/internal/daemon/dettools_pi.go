package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// piInstallTimeout bounds the optional adapter-install command.
const piInstallTimeout = 60 * time.Second

// preparePiToolPlane wires the deterministic tool plane for Pi, which has no
// native MCP and reaches the plane through pi-mcp-adapter. Because the adapter's
// exact interface is validated outside this codebase, the whole path is opt-in
// (PiAdapterEnabled) and every assumption is isolated/overridable. It is
// fail-open: any problem logs and leaves Pi running without the tool plane.
//
// Design (per the deterministic-tools plan, §6.1):
//   - Write the adapter's MCP config to a PER-TASK file under the task root —
//     never a shared global file — so concurrent Pi tasks can't race it.
//   - Point Pi/adapter at that file via a configurable env var (PiConfigEnvVar).
//   - Optionally run a configured command to ensure the adapter is installed.
func (d *Daemon) preparePiToolPlane(provider, rootDir, workDir string, agentMcpConfig, runtimeConfig json.RawMessage, agentEnv map[string]string, logger *slog.Logger) {
	if !d.cfg.DetTools.Enabled || !d.cfg.DetTools.PiAdapterEnabled || provider != "pi" {
		return
	}

	effective := computeEffectiveAllowed(d.cfg.DetTools, runtimeConfig)
	if len(effective) == 0 {
		logger.Info("dettools(pi): no tools enabled for this agent after policy; skipping adapter setup")
		return
	}

	selfBin, err := os.Executable()
	if err != nil {
		logger.Warn("dettools(pi): cannot resolve daemon binary; skipping", "error", err)
		return
	}
	if resolved, rerr := filepath.EvalSymlinks(selfBin); rerr == nil {
		selfBin = resolved
	}

	// The adapter reads the same Claude-style mcpServers shape; the work dir is
	// known here so it is pinned (no cwd fallback needed for Pi).
	merged, err := buildEffectiveMcpConfig(agentMcpConfig, selfBin, workDir, d.cfg.DetTools, effective)
	if err != nil {
		logger.Warn("dettools(pi): build adapter config failed; launching without tool plane", "error", err)
		return
	}

	cfgPath, err := writePiAdapterConfig(rootDir, merged)
	if err != nil {
		logger.Warn("dettools(pi): write adapter config failed; launching without tool plane", "error", err)
		return
	}

	// Optionally ensure the adapter is installed. Default is no-op: auto-running
	// package installs on a user's machine is opt-in via MULTICA_DETTOOLS_PI_INSTALL_CMD.
	if cmd := strings.TrimSpace(d.cfg.DetTools.PiInstallCmd); cmd != "" {
		runPiInstall(cmd, agentEnv, logger)
	}

	envVar := d.cfg.DetTools.PiConfigEnvVar
	if envVar == "" {
		envVar = DefaultPiConfigEnvVar
	}
	agentEnv[envVar] = cfgPath
	logger.Info("dettools(pi): wrote per-task adapter config",
		"path", cfgPath,
		"env_var", envVar,
		"tools", effective,
	)
}

// writePiAdapterConfig writes the merged MCP config to a per-task file. Falls
// back to a temp dir when no task root is available. Mode 0600 because the
// config env block may carry secrets.
func writePiAdapterConfig(rootDir string, config json.RawMessage) (string, error) {
	base := rootDir
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "pi-mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// runPiInstall runs the configured adapter-install command best-effort with the
// agent's environment, bounded by piInstallTimeout. Failure is logged, not
// fatal — the tool plane simply stays unavailable for Pi (fail-open).
func runPiInstall(command string, agentEnv map[string]string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), piInstallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = mergedEnv(agentEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn("dettools(pi): adapter install command failed; continuing fail-open",
			"error", err, "output", strings.TrimSpace(string(out)))
		return
	}
	logger.Info("dettools(pi): adapter install command ran", "output", strings.TrimSpace(string(out)))
}

// mergedEnv returns os.Environ overlaid with extra (extra wins on conflict).
func mergedEnv(extra map[string]string) []string {
	skip := make(map[string]bool, len(extra))
	for k := range extra {
		skip[k] = true
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 && skip[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
