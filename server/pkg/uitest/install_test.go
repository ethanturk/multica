package uitest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUIRuntimeStatusStates(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{name: "unavailable", setup: func(t *testing.T, root string) {}, want: StatusUnavailable},
		{
			name: "installing",
			setup: func(t *testing.T, root string) {
				writeJSONFile(t, filepath.Join(root, "install.lock"), installMarker{PID: 42, StartedAt: now.Add(-time.Minute), Token: "active"})
			},
			want: StatusInstalling,
		},
		{
			name: "stale install is broken",
			setup: func(t *testing.T, root string) {
				writeJSONFile(t, filepath.Join(root, "install.lock"), installMarker{PID: 42, StartedAt: now.Add(-31 * time.Minute), Token: "stale"})
			},
			want: StatusBroken,
		},
		{
			name: "ready",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
			},
			want: StatusReady,
		},
		{
			name: "missing cli is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
				os.Remove(filepath.Join(root, "runtimes", PlaywrightMCPVersion, "node_modules", "@playwright", "mcp", "cli.js"))
			},
			want: StatusBroken,
		},
		{
			name: "missing axe is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
				os.Remove(filepath.Join(root, "runtimes", PlaywrightMCPVersion, "node_modules", "axe-core", "axe.min.js"))
			},
			want: StatusBroken,
		},
		{
			name: "missing playwright is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
				os.Remove(filepath.Join(root, "runtimes", PlaywrightMCPVersion, "node_modules", ".bin", "playwright"))
			},
			want: StatusBroken,
		},
		{
			name: "missing chromium is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
				os.Remove(filepath.Join(root, "runtimes", PlaywrightMCPVersion, "browsers", "chromium"))
			},
			want: StatusBroken,
		},
		{
			name: "mismatched mcp version is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, "0.0.77", AxeCoreVersion)
			},
			want: StatusBroken,
		},
		{
			name: "mismatched axe version is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, "4.12.0")
			},
			want: StatusBroken,
		},
		{
			name: "mismatched installed package version is broken",
			setup: func(t *testing.T, root string) {
				writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
				runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
				writeTestRuntimeFile(
					t,
					runtimeDir,
					filepath.Join("node_modules", "@playwright", "mcp", "package.json"),
					`{"version":"0.0.77"}`,
				)
			},
			want: StatusBroken,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "ui-test")
			test.setup(t, root)
			manager := testManager(root, now, nil)

			got := manager.Status()
			if got.Status != test.want {
				t.Fatalf("Status() = %+v, want status %q", got, test.want)
			}
			if test.want == StatusReady && got.Version != PlaywrightMCPVersion {
				t.Fatalf("Status() version = %q, want %q", got.Version, PlaywrightMCPVersion)
			}
			if test.want == StatusBroken && got.Error == "" {
				t.Fatalf("Status() = %+v, want bounded diagnostic", got)
			}
		})
	}
}

func TestUIRuntimeInstallUsesPinnedCommandsAndPromotesVerifiedRuntime(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	var commands []Command
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		commands = append(commands, command)
		switch len(commands) {
		case 1:
			if command.Path != "/tools/npm" || len(command.Args) != 7 {
				t.Fatalf("npm command = %#v", command)
			}
			want := []string{"install", "--prefix", command.Args[2], "--no-save", "--ignore-scripts", "@playwright/mcp@" + PlaywrightMCPVersion, "axe-core@" + AxeCoreVersion}
			if strings.Join(command.Args, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("npm args = %q, want %q", command.Args, want)
			}
			createFakeInstall(t, command.Args[2])
		case 2:
			if !strings.HasSuffix(command.Path, filepath.Join("node_modules", ".bin", "playwright")) || strings.Join(command.Args, " ") != "install chromium" {
				t.Fatalf("playwright command = %#v", command)
			}
			if got := envValue(command.Env, "PLAYWRIGHT_BROWSERS_PATH"); got != filepath.Join(command.Dir, "browsers") {
				t.Fatalf("PLAYWRIGHT_BROWSERS_PATH = %q, want temp browsers", got)
			}
		case 3:
			if command.Path != "/tools/node" || command.Dir == "" || len(command.Args) != 2 || command.Args[0] != "-e" || !strings.Contains(command.Args[1], "chromium.executablePath") {
				t.Fatalf("node resolution command = %#v", command)
			}
			return CommandResult{Stdout: filepath.Join(command.Dir, "browsers", "chromium")}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
		}
		return CommandResult{}, nil
	})

	got, err := manager.Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got.Status != StatusReady || got.Version != PlaywrightMCPVersion {
		t.Fatalf("Install() = %+v, want ready %s", got, PlaywrightMCPVersion)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(commands))
	}
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	if _, err := os.Stat(filepath.Join(runtimeDir, "ready.json")); err != nil {
		t.Fatalf("promoted ready manifest: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "runtimes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != PlaywrightMCPVersion {
		t.Fatalf("runtime entries = %v, want only promoted version", entryNames(entries))
	}
	if _, err := os.Stat(filepath.Join(root, "install.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install.lock remains after install: %v", err)
	}
}

func TestUIRuntimeInstallRejectsConcurrentInstaller(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		once.Do(func() { close(started) })
		<-release
		return CommandResult{}, errors.New("stop first install")
	})

	firstDone := make(chan error, 1)
	go func() {
		_, err := manager.Install(context.Background())
		firstDone <- err
	}()
	<-started

	if _, err := manager.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second Install() error = %v, want already in progress", err)
	}
	close(release)
	if err := <-firstDone; err == nil {
		t.Fatal("first Install() succeeded, want injected failure")
	}
}

func TestUIRuntimeLockReleaseDoesNotRemoveSuccessor(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(root, now, nil)
	unlock, err := manager.acquireLock()
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}

	lockPath := filepath.Join(root, "install.lock")
	writeJSONFile(t, lockPath, map[string]any{
		"pid":        456,
		"started_at": now.Add(time.Minute),
		"token":      "successor",
	})
	unlock()

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("successor lock was removed: %v", err)
	}
	if !strings.Contains(string(data), "successor") {
		t.Fatalf("lock = %s, want successor owner", data)
	}
}

func TestUIRuntimeLockRemovesMarkerBeforeReleasingHandleOwnership(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(root, now, nil)
	lockCalls := 0
	unlockCalls := 0
	manager.lockFile = func(*os.File) (bool, error) {
		lockCalls++
		return true, nil
	}
	manager.unlockFile = func(*os.File) error {
		unlockCalls++
		if _, err := os.Stat(filepath.Join(root, "install.lock")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("install marker still exists when handle ownership is released: %v", err)
		}
		return nil
	}

	unlock, err := manager.acquireLock()
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}
	unlock()

	if lockCalls != 1 {
		t.Fatalf("handle lock calls = %d, want 1", lockCalls)
	}
	if unlockCalls != 1 {
		t.Fatalf("handle unlock calls = %d, want 1", unlockCalls)
	}
}

func TestUIRuntimeLockRecoversMalformedOrphanMarkers(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "partial", content: `{"pid":42,"started_at":"2026-07-29T11:59:00Z"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "ui-test")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(root, "install.lock")
			if err := os.WriteFile(lockPath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			manager := testManager(root, now, nil)
			if got := manager.Status(); got.Status != StatusBroken {
				t.Fatalf("Status() = %+v, want malformed marker broken", got)
			}
			unlock, err := manager.acquireLock()
			if err != nil {
				t.Fatalf("acquireLock() error = %v, want malformed orphan recovery", err)
			}
			unlock()
			if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("install marker remains after recovered lock release: %v", err)
			}
		})
	}
}

func TestUIRuntimeLockCleansReservationAfterMarkerPublicationFailure(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := testManager(root, now, nil)
	manager.publishMarker = func(string, installMarker) error {
		return errors.New("injected marker publication failure")
	}

	if _, err := manager.acquireLock(); err == nil || !strings.Contains(err.Error(), "injected marker publication failure") {
		t.Fatalf("acquireLock() error = %v, want publication failure", err)
	}
	lockPath := filepath.Join(root, "install.lock")
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed publication left install marker: %v", err)
	}

	manager.publishMarker = publishInstallMarker
	unlock, err := manager.acquireLock()
	if err != nil {
		t.Fatalf("acquireLock() retry error = %v", err)
	}
	unlock()
}

func TestUIRuntimeSharedMarkerReadAllowsAtomicReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "install.lock")
	replacement := filepath.Join(root, "replacement.tmp")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	reader, err := openFileShared(target)
	if err != nil {
		t.Fatalf("openFileShared() error = %v", err)
	}
	defer reader.Close()
	if err := replaceFile(replacement, target); err != nil {
		t.Fatalf("replaceFile() with shared reader error = %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("replacement content = %q, %v; want new", data, err)
	}
}

func TestUIRuntimeLockScavengesOnlyOrphanMarkerTemps(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(root, "install.lock."+strings.Repeat("a", 32)+".tmp")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	nearMisses := []string{
		"install.lock." + strings.Repeat("b", 31) + ".tmp",
		"install.lock." + strings.Repeat("c", 32) + ".tmp.backup",
		"other.lock." + strings.Repeat("d", 32) + ".tmp",
		"install.lock." + strings.Repeat("g", 32) + ".tmp",
	}
	for _, name := range nearMisses {
		if err := os.WriteFile(filepath.Join(root, name), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	matchingDirectory := "install.lock." + strings.Repeat("e", 32) + ".tmp"
	if err := os.Mkdir(filepath.Join(root, matchingDirectory), 0o700); err != nil {
		t.Fatal(err)
	}

	unlock, err := testManager(root, now, nil).acquireLock()
	if err != nil {
		t.Fatalf("acquireLock() error = %v", err)
	}
	defer unlock()
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("matching orphan marker temp remains: %v", err)
	}
	for _, name := range append(nearMisses, matchingDirectory) {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("near-miss path %q was removed: %v", name, err)
		}
	}
}

func TestUIRuntimeLockDoesNotScavengeActiveInstallArtifact(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first := testManager(root, now, nil)
	unlock, err := first.acquireLock()
	if err != nil {
		t.Fatalf("first acquireLock() error = %v", err)
	}
	defer unlock()

	activeArtifact := filepath.Join(root, "install.lock."+strings.Repeat("f", 32)+".tmp")
	if err := os.WriteFile(activeArtifact, []byte("active"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := testManager(root, now, nil).acquireLock(); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second acquireLock() error = %v, want already in progress", err)
	}
	if data, err := os.ReadFile(activeArtifact); err != nil || string(data) != "active" {
		t.Fatalf("active publication artifact changed: data=%q err=%v", data, err)
	}
}

func TestUIRuntimeInstallPreservesReadyRuntime(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
	manifestPath := filepath.Join(root, "runtimes", PlaywrightMCPVersion, "ready.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manager := testManager(root, now.Add(time.Hour), func(context.Context, Command) (CommandResult, error) {
		return CommandResult{}, errors.New("runner must not be called for ready runtime")
	})

	got, err := manager.Install(context.Background())
	if err != nil || got.Status != StatusReady {
		t.Fatalf("Install() = %+v, %v; want preserved ready runtime", got, err)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("Install() replaced existing ready manifest")
	}
}

func TestUIRuntimePromotionPreservesReadyRuntimeAppearingDuringRepair(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	writeTestRuntimeFile(t, runtimeDir, "sentinel.txt", "broken")
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		switch {
		case command.Path == "/tools/npm":
			createFakeInstall(t, command.Args[2])
		case strings.HasSuffix(command.Path, filepath.Join("node_modules", ".bin", "playwright")):
		case command.Path == "/tools/node":
			return CommandResult{Stdout: filepath.Join(command.Dir, "browsers", "chromium")}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
		}
		return CommandResult{}, nil
	})
	renameCalls := 0
	manager.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 1 {
			if err := os.RemoveAll(oldPath); err != nil {
				t.Fatal(err)
			}
			writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
			return errors.New("target changed before quarantine")
		}
		return os.Rename(oldPath, newPath)
	}

	got, err := manager.Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v, want concurrent ready runtime preserved", err)
	}
	if got.Status != StatusReady {
		t.Fatalf("Install() = %+v, want concurrent ready runtime", got)
	}
	if renameCalls != 1 {
		t.Fatalf("rename calls = %d, want no attempt to replace ready runtime", renameCalls)
	}
}

func TestUIRuntimePromotionRestoresReadyRuntimeClaimedDuringRepair(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	writeTestRuntimeFile(t, runtimeDir, "sentinel.txt", "broken")
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		switch {
		case command.Path == "/tools/npm":
			createFakeInstall(t, command.Args[2])
		case strings.HasSuffix(command.Path, filepath.Join("node_modules", ".bin", "playwright")):
		case command.Path == "/tools/node":
			return CommandResult{Stdout: filepath.Join(command.Dir, "browsers", "chromium")}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
		}
		return CommandResult{}, nil
	})
	renameCalls := 0
	manager.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 1 {
			if err := os.RemoveAll(oldPath); err != nil {
				t.Fatal(err)
			}
			writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
			writeTestRuntimeFile(t, runtimeDir, "concurrent.txt", "preserve")
		}
		return os.Rename(oldPath, newPath)
	}

	got, err := manager.Install(context.Background())
	if err != nil {
		t.Fatalf("Install() error = %v, want claimed ready runtime restored", err)
	}
	if got.Status != StatusReady {
		t.Fatalf("Install() = %+v, want claimed ready runtime", got)
	}
	if data, err := os.ReadFile(filepath.Join(runtimeDir, "concurrent.txt")); err != nil || string(data) != "preserve" {
		t.Fatalf("ready runtime was replaced: data=%q err=%v", data, err)
	}
}

func TestUIRuntimeInstallFailurePreservesBrokenRuntime(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	writeTestRuntimeFile(t, runtimeDir, "sentinel.txt", "keep")
	manager := testManager(root, now, func(context.Context, Command) (CommandResult, error) {
		return CommandResult{Stderr: strings.Repeat("x", diagnosticLimit*2)}, errors.New("npm failed")
	})

	if _, err := manager.Install(context.Background()); err == nil {
		t.Fatal("Install() succeeded, want injected failure")
	} else if len(err.Error()) > diagnosticLimit+1024 {
		t.Fatalf("Install() diagnostic length = %d, want bounded", len(err.Error()))
	}
	if data, err := os.ReadFile(filepath.Join(runtimeDir, "sentinel.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("broken runtime changed after failed install: data=%q err=%v", data, err)
	}
}

func TestUIRuntimeInstallRepairsBrokenRuntimeByAtomicPromotion(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	writeTestRuntimeFile(t, runtimeDir, "sentinel.txt", "broken")
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		switch {
		case command.Path == "/tools/npm":
			createFakeInstall(t, command.Args[2])
		case strings.HasSuffix(command.Path, filepath.Join("node_modules", ".bin", "playwright")):
		case command.Path == "/tools/node":
			return CommandResult{Stdout: filepath.Join(command.Dir, "browsers", "chromium")}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
		}
		return CommandResult{}, nil
	})

	got, err := manager.Install(context.Background())
	if err != nil {
		t.Fatalf("Install() repair error = %v", err)
	}
	if got.Status != StatusReady {
		t.Fatalf("Install() repair = %+v, want ready", got)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "sentinel.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("broken runtime sentinel remains: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "runtimes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != PlaywrightMCPVersion {
		t.Fatalf("runtime entries = %v, want repaired runtime only", entryNames(entries))
	}
}

func TestUIRuntimeInstallRepairsManifestReadyRuntimeWithCorruptedPackage(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
	writeTestRuntimeFile(
		t,
		runtimeDir,
		filepath.Join("node_modules", "@playwright", "mcp", "package.json"),
		`{"version":"0.0.77"}`,
	)
	var commands int
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		commands++
		switch {
		case command.Path == "/tools/npm":
			createFakeInstall(t, command.Args[2])
		case strings.HasSuffix(command.Path, filepath.Join("node_modules", ".bin", "playwright")):
		case command.Path == "/tools/node":
			return CommandResult{Stdout: filepath.Join(command.Dir, "browsers", "chromium")}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
		}
		return CommandResult{}, nil
	})

	got, err := manager.Install(context.Background())
	if err != nil {
		t.Fatalf("Install() repair error = %v", err)
	}
	if got.Status != StatusReady || commands != 3 {
		t.Fatalf("Install() repair = %+v with %d commands, want ready reinstall", got, commands)
	}
	if err := verifyManagedPackageVersion(
		filepath.Join(runtimeDir, "node_modules", "@playwright", "mcp", "package.json"),
		PlaywrightMCPVersion,
	); err != nil {
		t.Fatalf("repaired package: %v", err)
	}
}

func TestUIRuntimePromotionRejectsCorruptedCandidate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ui-test")
	target := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	candidateRoot := filepath.Join(t.TempDir(), "candidate-root")
	writeReadyRuntime(t, candidateRoot, PlaywrightMCPVersion, AxeCoreVersion)
	candidate := filepath.Join(candidateRoot, "runtimes", PlaywrightMCPVersion)
	writeTestRuntimeFile(
		t,
		candidate,
		filepath.Join("node_modules", "axe-core", "package.json"),
		`{"version":"4.12.0"}`,
	)

	manager := testManager(root, time.Now(), nil)
	if err := manager.promote(candidate, target); err == nil ||
		!strings.Contains(err.Error(), "Axe package") {
		t.Fatalf("promote() error = %v, want corrupted package rejection", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupted candidate promoted: %v", err)
	}
}

func TestUIRuntimeInstallRestoresBrokenRuntimeWhenPromotionFails(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "ui-test")
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	writeTestRuntimeFile(t, runtimeDir, "sentinel.txt", "restore")
	manager := testManager(root, now, func(_ context.Context, command Command) (CommandResult, error) {
		switch {
		case command.Path == "/tools/npm":
			createFakeInstall(t, command.Args[2])
		case strings.HasSuffix(command.Path, filepath.Join("node_modules", ".bin", "playwright")):
		case command.Path == "/tools/node":
			return CommandResult{Stdout: filepath.Join(command.Dir, "browsers", "chromium")}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
		}
		return CommandResult{}, nil
	})
	renameCalls := 0
	manager.rename = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("injected promotion failure")
		}
		return os.Rename(oldPath, newPath)
	}

	if _, err := manager.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "injected promotion failure") {
		t.Fatalf("Install() error = %v, want promotion failure", err)
	}
	if data, err := os.ReadFile(filepath.Join(runtimeDir, "sentinel.txt")); err != nil || string(data) != "restore" {
		t.Fatalf("broken runtime was not restored: data=%q err=%v", data, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "runtimes"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != PlaywrightMCPVersion {
		t.Fatalf("runtime entries = %v, want restored runtime only", entryNames(entries))
	}
}

func TestUIRuntimeStatusRejectsSymlinkEscapeButAllowsInternalBinSymlink(t *testing.T) {
	t.Run("external target", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "ui-test")
		writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
		runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
		axePath := filepath.Join(runtimeDir, "node_modules", "axe-core", "axe.min.js")
		if err := os.Remove(axePath); err != nil {
			t.Fatal(err)
		}
		external := filepath.Join(t.TempDir(), "axe.min.js")
		if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, axePath); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		got := testManager(root, time.Now(), nil).Status()
		if got.Status != StatusBroken {
			t.Fatalf("Status() = %+v, want external symlink broken", got)
		}
	})

	t.Run("internal target", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "ui-test")
		writeReadyRuntime(t, root, PlaywrightMCPVersion, AxeCoreVersion)
		runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
		playwrightPath := filepath.Join(runtimeDir, "node_modules", ".bin", "playwright")
		if err := os.Remove(playwrightPath); err != nil {
			t.Fatal(err)
		}
		internalTarget := filepath.Join(runtimeDir, "node_modules", "playwright", "cli.js")
		writeTestRuntimeFile(t, runtimeDir, filepath.Join("node_modules", "playwright", "cli.js"), "cli")
		relativeTarget, err := filepath.Rel(filepath.Dir(playwrightPath), internalTarget)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(relativeTarget, playwrightPath); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		got := testManager(root, time.Now(), nil).Status()
		if got.Status != StatusReady {
			t.Fatalf("Status() = %+v, want internal .bin symlink ready", got)
		}
	})
}

func TestUIRuntimeFutureInstallMarkerIsBrokenAndReplaceable(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	t.Run("status", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "ui-test")
		writeJSONFile(t, filepath.Join(root, "install.lock"), map[string]any{
			"pid":        42,
			"started_at": now.Add(time.Minute),
			"token":      "future",
		})
		got := testManager(root, now, nil).Status()
		if got.Status != StatusBroken {
			t.Fatalf("Status() = %+v, want future marker broken", got)
		}
	})

	t.Run("takeover", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "ui-test")
		writeJSONFile(t, filepath.Join(root, "install.lock"), map[string]any{
			"pid":        42,
			"started_at": now.Add(time.Minute),
			"token":      "future",
		})
		unlock, err := testManager(root, now, nil).acquireLock()
		if err != nil {
			t.Fatalf("acquireLock() future marker error = %v", err)
		}
		unlock()
	})
}

func TestUIRuntimeRootUsesProfileLayout(t *testing.T) {
	home := t.TempDir()
	if got := RootForProfile(home, ""); got != filepath.Join(home, ".multica", "ui-test") {
		t.Fatalf("RootForProfile(default) = %q", got)
	}
	if got := RootForProfile(home, "dev"); got != filepath.Join(home, ".multica", "profiles", "dev", "ui-test") {
		t.Fatalf("RootForProfile(dev) = %q", got)
	}
}

func testManager(root string, now time.Time, runner CommandRunner) *Manager {
	if runner == nil {
		runner = func(context.Context, Command) (CommandResult, error) {
			return CommandResult{}, errors.New("unexpected command")
		}
	}
	return &Manager{
		root: root,
		now:  func() time.Time { return now },
		lookPath: func(name string) (string, error) {
			switch name {
			case "node", "npm":
				return "/tools/" + name, nil
			default:
				return "", errors.New("missing")
			}
		},
		run:           runner,
		rename:        os.Rename,
		newToken:      randomToken,
		lockFile:      tryExclusiveFileLock,
		unlockFile:    unlockExclusiveFile,
		publishMarker: publishInstallMarker,
		pid:           123,
	}
}

func writeReadyRuntime(t *testing.T, root, mcpVersion, axeVersion string) {
	t.Helper()
	runtimeDir := filepath.Join(root, "runtimes", PlaywrightMCPVersion)
	for path, content := range map[string]string{
		filepath.Join("node_modules", "@playwright", "mcp", "cli.js"):       "cli",
		filepath.Join("node_modules", "axe-core", "axe.min.js"):             "axe",
		filepath.Join("node_modules", ".bin", "playwright"):                 "playwright",
		filepath.Join("browsers", "chromium"):                               "chromium",
		filepath.Join("node_modules", "@playwright", "mcp", "package.json"): `{"version":"` + PlaywrightMCPVersion + `"}`,
		filepath.Join("node_modules", "axe-core", "package.json"):           `{"version":"` + AxeCoreVersion + `"}`,
	} {
		writeTestRuntimeFile(t, runtimeDir, path, content)
	}
	writeJSONFile(t, filepath.Join(runtimeDir, "ready.json"), readyManifest{
		MCPVersion:     mcpVersion,
		AxeVersion:     axeVersion,
		Browser:        "chromium",
		InstalledAt:    time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC),
		MCPCLIPath:     filepath.ToSlash(filepath.Join("node_modules", "@playwright", "mcp", "cli.js")),
		AxePath:        filepath.ToSlash(filepath.Join("node_modules", "axe-core", "axe.min.js")),
		PlaywrightPath: filepath.ToSlash(filepath.Join("node_modules", ".bin", "playwright")),
		BrowserPath:    filepath.ToSlash(filepath.Join("browsers", "chromium")),
	})
}

func createFakeInstall(t *testing.T, temp string) {
	t.Helper()
	for path, content := range map[string]string{
		filepath.Join("node_modules", "@playwright", "mcp", "cli.js"):       "cli",
		filepath.Join("node_modules", "axe-core", "axe.min.js"):             "axe",
		filepath.Join("node_modules", ".bin", "playwright"):                 "playwright",
		filepath.Join("browsers", "chromium"):                               "chromium",
		filepath.Join("node_modules", "@playwright", "mcp", "package.json"): `{"version":"` + PlaywrightMCPVersion + `"}`,
		filepath.Join("node_modules", "axe-core", "package.json"):           `{"version":"` + AxeCoreVersion + `"}`,
	} {
		writeTestRuntimeFile(t, temp, path, content)
	}
}

func writeTestRuntimeFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, value := range env {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
