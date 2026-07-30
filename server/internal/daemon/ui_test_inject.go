package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/uitest"
)

const uiTestServerName = "multica-ui-test"

const uiTestRuntimeBrief = "\n\n## UI Testing (MCP)\n\n" +
	"The `multica-ui-test` MCP server provides a managed local Chromium session.\n" +
	"Follow the built-in `multica-ui-testing` skill. Do not replace its safe browser\n" +
	"tools with ad-hoc browser processes or arbitrary page evaluation.\n"

func (d *Daemon) injectExecOptionsUITest(
	agentConfig json.RawMessage,
	provider, workDir, taskID string,
	logger *slog.Logger,
) (json.RawMessage, bool) {
	if !managedMCPExecOptionsProviders[provider] {
		return agentConfig, false
	}
	return d.injectUITest(agentConfig, provider, "", workDir, taskID, logger)
}

func (d *Daemon) injectExecenvUITest(
	agentConfig json.RawMessage,
	provider, taskID string,
	logger *slog.Logger,
) (json.RawMessage, bool) {
	return d.injectExecenvUITestWithBin(agentConfig, provider, "", taskID, logger)
}

func (d *Daemon) injectExecenvUITestWithBin(
	agentConfig json.RawMessage,
	provider, openclawBin, taskID string,
	logger *slog.Logger,
) (json.RawMessage, bool) {
	if provider != "openclaw" {
		return agentConfig, false
	}
	return d.injectUITest(agentConfig, provider, openclawBin, "", taskID, logger)
}

func (d *Daemon) injectUITest(
	agentConfig json.RawMessage,
	provider, openclawBin, workDir, taskID string,
	logger *slog.Logger,
) (json.RawMessage, bool) {
	status, runtimeDir, err := d.probeUITestCapability()
	if err != nil {
		logger.Warn("ui-test: readiness check failed; launching without managed UI testing", "error", err)
		return agentConfig, false
	}
	if status.Status != uitest.StatusReady {
		if status.Status == uitest.StatusBroken {
			logger.Warn("ui-test: managed runtime is broken; launching without managed UI testing")
		}
		return agentConfig, false
	}

	executable, err := d.resolveUITestExecutable()
	if err != nil {
		logger.Warn("ui-test: cannot resolve daemon binary; launching without managed UI testing", "error", err)
		return agentConfig, false
	}
	baseConfig := agentConfig
	if provider == "openclaw" {
		baseConfig, err = d.openClawManagedMCPBase(agentConfig, openclawBin)
		if err != nil {
			logger.Warn("ui-test: native MCP merge failed; launching without managed UI testing", "error", err)
			return agentConfig, false
		}
	}
	merged, injected, err := mergeUITestMCPConfig(baseConfig, executable, workDir, taskID, runtimeDir)
	if err != nil {
		logger.Warn("ui-test: MCP config merge failed; launching without managed UI testing", "provider", provider, "error", err)
		return agentConfig, false
	}
	if injected {
		logger.Info("ui-test: injected managed browser server", "provider", provider)
	}
	return merged, injected
}

func mergeUITestMCPConfig(
	agentConfig json.RawMessage,
	executable, workDir, taskID, runtimeDir string,
) (json.RawMessage, bool, error) {
	root := map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(agentConfig))) > 0 {
		if err := json.Unmarshal(agentConfig, &root); err != nil {
			return nil, false, fmt.Errorf("parse agent mcp_config: %w", err)
		}
	}
	if root == nil {
		root = map[string]json.RawMessage{}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, false, fmt.Errorf("parse mcpServers: %w", err)
		}
	}
	if servers == nil {
		servers = map[string]json.RawMessage{}
	}
	if _, exists := servers[uiTestServerName]; exists {
		return agentConfig, false, nil
	}

	env := map[string]string{
		"MULTICA_UI_TEST_TASK_ID":     taskID,
		"MULTICA_UI_TEST_RUNTIME_DIR": runtimeDir,
	}
	if workDir != "" {
		env["MULTICA_UI_TEST_WORKDIR"] = workDir
	}
	entry, err := json.Marshal(map[string]any{
		"command": executable,
		"args":    []string{"ui-test", "serve"},
		"env":     env,
	})
	if err != nil {
		return nil, false, err
	}
	servers[uiTestServerName] = entry

	encodedServers, err := json.Marshal(servers)
	if err != nil {
		return nil, false, err
	}
	root["mcpServers"] = encodedServers
	merged, err := json.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return merged, true, nil
}

func (d *Daemon) probeUITestCapability() (uitest.CapabilityStatus, string, error) {
	homeDir := d.uiTestHomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err := homeDir()
	if err != nil {
		return uitest.CapabilityStatus{}, "", fmt.Errorf("resolve user home: %w", err)
	}
	root := uitest.RootForProfile(home, d.cfg.Profile)
	statusProbe := d.uiTestStatus
	if statusProbe == nil {
		statusProbe = func(root string) uitest.CapabilityStatus {
			return uitest.NewManager(root).Status()
		}
	}
	status := statusProbe(root)
	runtimeDir := filepath.Join(root, "runtimes", uitest.PlaywrightMCPVersion)
	return status, runtimeDir, nil
}

func (d *Daemon) resolveUITestExecutable() (string, error) {
	resolver := d.uiTestExecutable
	if resolver == nil {
		resolver = resolveSelfExecutable
	}
	return resolver()
}

// openClawManagedMCPBase preserves native OpenClaw MCP inheritance when the
// daemon needs to add its first managed server. execenv treats every non-null
// mcp_config as a strict replacement, so seed absent configs from OpenClaw's
// active, fully resolved configuration.
func (d *Daemon) openClawManagedMCPBase(agentConfig json.RawMessage, openclawBin string) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(agentConfig)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		return agentConfig, nil
	}
	resolver := d.openclawMCPResolver
	if resolver == nil {
		resolver = execenv.ResolveOpenclawNativeMCPServers
	}
	servers, err := resolver(openclawBin)
	if err != nil {
		return nil, err
	}
	encodedServers, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]json.RawMessage{"mcpServers": encodedServers})
}
