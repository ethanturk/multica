package uitest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Command struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

type CommandResult struct {
	Stdout string
	Stderr string
}

type CommandRunner func(context.Context, Command) (CommandResult, error)

type Manager struct {
	root     string
	now      func() time.Time
	lookPath func(string) (string, error)
	run      CommandRunner
	rename   func(string, string) error
	newToken func() (string, error)
	pid      int
}

func NewManager(root string) *Manager {
	return &Manager{
		root:     root,
		now:      time.Now,
		lookPath: exec.LookPath,
		run:      runCommand,
		rename:   os.Rename,
		newToken: randomToken,
		pid:      os.Getpid(),
	}
}

func (m *Manager) Install(ctx context.Context) (CapabilityStatus, error) {
	if status := m.Status(); status.Status == StatusReady {
		return status, nil
	}
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return CapabilityStatus{}, fmt.Errorf("create UI test root: %w", err)
	}
	unlock, err := m.acquireLock()
	if err != nil {
		return CapabilityStatus{}, err
	}
	defer unlock()

	target := filepath.Join(m.root, "runtimes", PlaywrightMCPVersion)
	if status := inspectRuntime(target); status.Status == StatusReady {
		return status, nil
	}

	nodePath, err := m.lookPath("node")
	if err != nil {
		return CapabilityStatus{}, fmt.Errorf("UI test runtime requires node on PATH: %w", err)
	}
	npmPath, err := m.lookPath("npm")
	if err != nil {
		return CapabilityStatus{}, fmt.Errorf("UI test runtime requires npm on PATH: %w", err)
	}

	runtimesDir := filepath.Dir(target)
	if err := os.MkdirAll(runtimesDir, 0o755); err != nil {
		return CapabilityStatus{}, fmt.Errorf("create runtimes directory: %w", err)
	}
	temp, err := os.MkdirTemp(runtimesDir, "."+PlaywrightMCPVersion+"-")
	if err != nil {
		return CapabilityStatus{}, fmt.Errorf("create temporary runtime: %w", err)
	}
	defer os.RemoveAll(temp)

	if err := m.runChecked(ctx, Command{
		Path: npmPath,
		Args: []string{
			"install", "--prefix", temp, "--no-save", "--ignore-scripts",
			"@playwright/mcp@" + PlaywrightMCPVersion,
			"axe-core@" + AxeCoreVersion,
		},
	}); err != nil {
		return CapabilityStatus{}, fmt.Errorf("install pinned UI test packages: %w", err)
	}

	browsersDir := filepath.Join(temp, "browsers")
	playwrightPath := filepath.Join(temp, "node_modules", ".bin", "playwright")
	if err := m.runChecked(ctx, Command{
		Path: playwrightPath,
		Args: []string{"install", "chromium"},
		Dir:  temp,
		Env:  []string{"PLAYWRIGHT_BROWSERS_PATH=" + browsersDir},
	}); err != nil {
		return CapabilityStatus{}, fmt.Errorf("install Chromium: %w", err)
	}

	result, err := m.run(ctx, Command{
		Path: nodePath,
		Args: []string{"-e", "const { chromium } = require('playwright'); process.stdout.write(chromium.executablePath())"},
		Dir:  temp,
		Env:  []string{"PLAYWRIGHT_BROWSERS_PATH=" + browsersDir},
	})
	if err != nil {
		return CapabilityStatus{}, fmt.Errorf("resolve Chromium executable: %w", commandFailure(result, err))
	}
	browserPath := strings.TrimSpace(result.Stdout)
	if browserPath == "" {
		return CapabilityStatus{}, fmt.Errorf("resolve Chromium executable: empty path")
	}
	browserRelative, err := filepath.Rel(temp, browserPath)
	if err != nil || browserRelative == ".." || strings.HasPrefix(browserRelative, ".."+string(filepath.Separator)) {
		return CapabilityStatus{}, fmt.Errorf("resolve Chromium executable: path is outside managed runtime")
	}

	manifest, err := verifyInstalledRuntime(temp, browserRelative, m.now())
	if err != nil {
		return CapabilityStatus{}, err
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return CapabilityStatus{}, fmt.Errorf("encode ready manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(filepath.Join(temp, "ready.json"), manifestData, 0o600); err != nil {
		return CapabilityStatus{}, fmt.Errorf("write ready manifest: %w", err)
	}

	if err := m.promote(temp, target); err != nil {
		return CapabilityStatus{}, err
	}
	return inspectRuntime(target), nil
}

func (m *Manager) acquireLock() (func(), error) {
	lockPath := filepath.Join(m.root, "install.lock")
	token, err := m.newToken()
	if err != nil {
		return nil, fmt.Errorf("create install marker token: %w", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			marker := installMarker{PID: m.pid, StartedAt: m.now(), Token: token}
			encodeErr := json.NewEncoder(file).Encode(marker)
			closeErr := file.Close()
			if encodeErr != nil || closeErr != nil {
				_, _ = removeLockIfOwned(lockPath, marker.Token)
				return nil, errors.Join(encodeErr, closeErr)
			}
			return func() { _, _ = removeLockIfOwned(lockPath, marker.Token) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create install marker: %w", err)
		}
		data, readErr := os.ReadFile(lockPath)
		var marker installMarker
		if readErr != nil || json.Unmarshal(data, &marker) != nil || marker.StartedAt.IsZero() || marker.Token == "" {
			return nil, fmt.Errorf("UI test runtime installation already in progress")
		}
		age := m.now().Sub(marker.StartedAt)
		if age >= 0 && age <= lockMaxAge {
			return nil, fmt.Errorf("UI test runtime installation already in progress")
		}
		removed, err := removeLockIfOwned(lockPath, marker.Token)
		if err != nil {
			return nil, fmt.Errorf("remove stale install marker: %w", err)
		}
		if !removed {
			return nil, fmt.Errorf("UI test runtime installation already in progress")
		}
	}
	return nil, fmt.Errorf("UI test runtime installation already in progress")
}

func randomToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func removeLockIfOwned(lockPath, token string) (bool, error) {
	data, err := os.ReadFile(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var marker installMarker
	if err := json.Unmarshal(data, &marker); err != nil || marker.Token != token {
		return false, nil
	}
	if err := os.Remove(lockPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) runChecked(ctx context.Context, command Command) error {
	result, err := m.run(ctx, command)
	if err == nil {
		return nil
	}
	return commandFailure(result, err)
}

func commandFailure(result CommandResult, err error) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, bounded(detail))
}

func verifyInstalledRuntime(temp, browserRelative string, installedAt time.Time) (readyManifest, error) {
	for path, want := range map[string]string{
		filepath.Join(temp, "node_modules", "@playwright", "mcp", "package.json"): PlaywrightMCPVersion,
		filepath.Join(temp, "node_modules", "axe-core", "package.json"):           AxeCoreVersion,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			return readyManifest{}, fmt.Errorf("inspect installed package: %w", err)
		}
		var pkg struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return readyManifest{}, fmt.Errorf("inspect installed package: %w", err)
		}
		if pkg.Version != want {
			return readyManifest{}, fmt.Errorf("installed package version is %q, want %q", pkg.Version, want)
		}
	}

	manifest := readyManifest{
		MCPVersion:     PlaywrightMCPVersion,
		AxeVersion:     AxeCoreVersion,
		Browser:        "chromium",
		InstalledAt:    installedAt,
		MCPCLIPath:     filepath.ToSlash(filepath.Join("node_modules", "@playwright", "mcp", "cli.js")),
		AxePath:        filepath.ToSlash(filepath.Join("node_modules", "axe-core", "axe.min.js")),
		PlaywrightPath: filepath.ToSlash(filepath.Join("node_modules", ".bin", "playwright")),
		BrowserPath:    filepath.ToSlash(browserRelative),
	}
	for label, path := range map[string]string{
		"Playwright MCP CLI": manifest.MCPCLIPath,
		"Axe":                manifest.AxePath,
		"Playwright CLI":     manifest.PlaywrightPath,
		"Chromium":           manifest.BrowserPath,
	} {
		fullPath, err := managedPath(temp, path)
		if err != nil {
			return readyManifest{}, fmt.Errorf("verify %s: %w", label, err)
		}
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			return readyManifest{}, fmt.Errorf("verify %s: required file is missing", label)
		}
	}
	return manifest, nil
}

func (m *Manager) promote(temp, target string) error {
	if status := inspectRuntime(target); status.Status == StatusReady {
		return nil
	}

	var quarantine string
	if _, err := os.Stat(target); err == nil {
		quarantine = fmt.Sprintf("%s.broken-%d", target, m.now().UnixNano())
		if err := m.rename(target, quarantine); err != nil {
			if status := inspectRuntime(target); status.Status == StatusReady {
				return nil
			}
			return fmt.Errorf("quarantine broken runtime: %w", err)
		}
		if status := inspectRuntime(quarantine); status.Status == StatusReady {
			if status := inspectRuntime(target); status.Status == StatusReady {
				if err := os.RemoveAll(quarantine); err != nil {
					return fmt.Errorf("remove duplicate claimed runtime: %w", err)
				}
				return nil
			}
			if err := m.rename(quarantine, target); err != nil {
				if status := inspectRuntime(target); status.Status == StatusReady {
					if removeErr := os.RemoveAll(quarantine); removeErr != nil {
						return fmt.Errorf("restore claimed ready runtime: %w; remove duplicate: %v", err, removeErr)
					}
					return nil
				}
				return fmt.Errorf("restore claimed ready runtime: %w", err)
			}
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect runtime destination: %w", err)
	}

	if err := m.rename(temp, target); err != nil {
		if status := inspectRuntime(target); status.Status == StatusReady {
			if quarantine != "" {
				if removeErr := os.RemoveAll(quarantine); removeErr != nil {
					return fmt.Errorf("preserve concurrent ready runtime: remove quarantine: %w", removeErr)
				}
			}
			return nil
		}
		if quarantine != "" {
			if restoreErr := m.rename(quarantine, target); restoreErr != nil {
				return fmt.Errorf("promote runtime: %w; restore broken runtime: %v", err, restoreErr)
			}
		}
		return fmt.Errorf("promote runtime: %w", err)
	}
	if quarantine != "" {
		if err := os.RemoveAll(quarantine); err != nil {
			return fmt.Errorf("remove quarantined runtime: %w", err)
		}
	}
	return nil
}

func runCommand(ctx context.Context, command Command) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = commandEnvironment(command.Env)
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

func commandEnvironment(overrides []string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}
	keys := make(map[string]struct{}, len(overrides))
	for _, value := range overrides {
		key, _, _ := strings.Cut(value, "=")
		keys[key] = struct{}{}
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, overridden := keys[key]; !overridden {
			env = append(env, value)
		}
	}
	return append(env, overrides...)
}

type cappedBuffer struct {
	buffer bytes.Buffer
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	originalLen := len(data)
	remaining := diagnosticLimit - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buffer.Write(data)
	}
	return originalLen, nil
}

func (b *cappedBuffer) String() string {
	return b.buffer.String()
}

var _ io.Writer = (*cappedBuffer)(nil)
