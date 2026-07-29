package uitest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StatusUnavailable = "unavailable"
	StatusInstalling  = "installing"
	StatusReady       = "ready"
	StatusBroken      = "broken"

	PlaywrightMCPVersion = "0.0.78"
	AxeCoreVersion       = "4.12.1"

	lockMaxAge      = 30 * time.Minute
	diagnosticLimit = 64 * 1024
)

type CapabilityStatus struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

type readyManifest struct {
	MCPVersion     string    `json:"mcp_version"`
	AxeVersion     string    `json:"axe_version"`
	Browser        string    `json:"browser"`
	InstalledAt    time.Time `json:"installed_at"`
	MCPCLIPath     string    `json:"mcp_cli_path"`
	AxePath        string    `json:"axe_path"`
	PlaywrightPath string    `json:"playwright_path"`
	BrowserPath    string    `json:"browser_path"`
}

type installMarker struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Token     string    `json:"token"`
}

func RootForProfile(home, profile string) string {
	if profile == "" {
		return filepath.Join(home, ".multica", "ui-test")
	}
	return filepath.Join(home, ".multica", "profiles", profile, "ui-test")
}

func (m *Manager) Status() CapabilityStatus {
	if _, err := os.Stat(m.root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CapabilityStatus{Status: StatusUnavailable}
		}
		return brokenStatus(err)
	}

	lockPath := filepath.Join(m.root, "install.lock")
	if data, err := os.ReadFile(lockPath); err == nil {
		var marker installMarker
		if err := json.Unmarshal(data, &marker); err != nil || marker.StartedAt.IsZero() || marker.Token == "" {
			return brokenStatus(fmt.Errorf("invalid install marker"))
		}
		age := m.now().Sub(marker.StartedAt)
		if age < 0 {
			return brokenStatus(fmt.Errorf("installation marker timestamp is in the future"))
		}
		if age <= lockMaxAge {
			return CapabilityStatus{Status: StatusInstalling}
		}
		return brokenStatus(fmt.Errorf("installation marker is older than %s", lockMaxAge))
	} else if !errors.Is(err, os.ErrNotExist) {
		return brokenStatus(fmt.Errorf("read install marker: %w", err))
	}

	return inspectRuntime(filepath.Join(m.root, "runtimes", PlaywrightMCPVersion))
}

func inspectRuntime(runtimeDir string) CapabilityStatus {
	data, err := os.ReadFile(filepath.Join(runtimeDir, "ready.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, statErr := os.Stat(runtimeDir); errors.Is(statErr, os.ErrNotExist) {
				return CapabilityStatus{Status: StatusUnavailable}
			}
		}
		return brokenStatus(fmt.Errorf("read ready manifest: %w", err))
	}

	var manifest readyManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return brokenStatus(fmt.Errorf("decode ready manifest: %w", err))
	}
	if manifest.MCPVersion != PlaywrightMCPVersion {
		return brokenStatus(fmt.Errorf("Playwright MCP version is %q, want %q", manifest.MCPVersion, PlaywrightMCPVersion))
	}
	if manifest.AxeVersion != AxeCoreVersion {
		return brokenStatus(fmt.Errorf("Axe version is %q, want %q", manifest.AxeVersion, AxeCoreVersion))
	}
	if manifest.Browser != "chromium" {
		return brokenStatus(fmt.Errorf("browser is %q, want chromium", manifest.Browser))
	}
	for label, path := range map[string]string{
		"Playwright MCP CLI": manifest.MCPCLIPath,
		"Axe":                manifest.AxePath,
		"Playwright CLI":     manifest.PlaywrightPath,
		"Chromium":           manifest.BrowserPath,
	} {
		fullPath, err := managedPath(runtimeDir, path)
		if err != nil {
			return brokenStatus(fmt.Errorf("%s path: %w", label, err))
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			return brokenStatus(fmt.Errorf("%s is missing: %w", label, err))
		}
		if !info.Mode().IsRegular() {
			return brokenStatus(fmt.Errorf("%s is not a file", label))
		}
	}
	return CapabilityStatus{Status: StatusReady, Version: PlaywrightMCPVersion}
}

func managedPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("escapes runtime directory")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve runtime directory: %w", err)
	}
	canonicalPath, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		return "", fmt.Errorf("resolve managed path: %w", err)
	}
	relativeToRoot, err := filepath.Rel(canonicalRoot, canonicalPath)
	if err != nil {
		return "", fmt.Errorf("compare managed path: %w", err)
	}
	if relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("escapes runtime directory")
	}
	return canonicalPath, nil
}

func brokenStatus(err error) CapabilityStatus {
	return CapabilityStatus{Status: StatusBroken, Error: bounded(err.Error())}
}

func bounded(value string) string {
	if len(value) <= diagnosticLimit {
		return value
	}
	return value[:diagnosticLimit] + "...[truncated]"
}
