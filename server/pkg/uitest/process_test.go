package uitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestUITestHelperProcess(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index < 0 || index >= len(os.Args) {
		return
	}
	args := os.Args[index:]
	switch args[0] {
	case "descendant":
		mustWriteHelperFile(args[1], strconv.Itoa(os.Getpid()))
		waitForever()
	case "descendant-resistant":
		signal.Ignore(syscall.SIGTERM)
		mustWriteHelperFile(args[1], strconv.Itoa(os.Getpid()))
		waitForever()
	case "app":
		runAppHelper(args[1:])
	case "setup":
		runSetupHelper(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q", args[0])
		os.Exit(2)
	}
}

func runAppHelper(args []string) {
	address, events, descendantPID, behavior := args[0], args[1], args[2], args[3]
	if descendantPID != "-" {
		descendantMode := "descendant"
		if behavior == "healthy-resistant" {
			descendantMode = "descendant-resistant"
		}
		child := exec.Command(os.Args[0], "-test.run=^TestUITestHelperProcess$", "--", descendantMode, descendantPID)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	appendHelperEvent(events, "app")
	fmt.Fprintln(os.Stdout, "app evidence")
	switch behavior {
	case "fail":
		waitHelperFile(descendantPID, 2*time.Second)
		os.Exit(7)
	case "no-health":
		waitForever()
	case "healthy":
		serveHelper(address, events, "", false)
	case "healthy-resistant":
		serveHelper(address, events, "", false)
	case "healthy-exit":
		serveHelper(address, events, "", true)
	case "redirect":
		serveHelper(address, events, args[4], false)
	default:
		fmt.Fprintln(os.Stderr, "unknown app behavior")
		os.Exit(2)
	}
}

func waitForever() {
	for {
		time.Sleep(time.Hour)
	}
}

func serveHelper(address, events, redirect string, exitAfterResponse bool) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		appendHelperEvent(events, "health")
		if redirect != "" {
			w.Header().Set("Location", redirect)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		if exitAfterResponse {
			go func() {
				time.Sleep(50 * time.Millisecond)
				os.Exit(11)
			}()
		}
	})
	if err := http.Serve(listener, handler); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func runSetupHelper(args []string) {
	events, behavior := args[0], args[1]
	appendHelperEvent(events, "setup")
	statePath := os.Getenv("MULTICA_UI_TEST_STORAGE_STATE")
	if statePath == "" ||
		os.Getenv("MULTICA_UI_TEST_BASE_URL") == "" ||
		os.Getenv("MULTICA_UI_TEST_ARTIFACT_DIR") == "" ||
		os.Getenv("MULTICA_UI_TEST_TASK_ID") == "" {
		fmt.Fprintln(os.Stderr, "missing setup environment")
		os.Exit(8)
	}
	switch behavior {
	case "success":
		mustWriteHelperFile(statePath, `{"cookies":[],"origins":[]}`)
	case "external-cookie":
		mustWriteHelperFile(statePath, `{"cookies":[{"name":"token","value":"secret","domain":"example.com","path":"/","expires":-1,"httpOnly":true,"secure":false,"sameSite":"Lax"}],"origins":[]}`)
	case "fail":
		fmt.Fprintln(os.Stderr, "setup failed")
		os.Exit(9)
	case "hang":
		waitForever()
	default:
		fmt.Fprintln(os.Stderr, "unknown setup behavior")
		os.Exit(2)
	}
}

func mustWriteHelperFile(path, value string) {
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func appendHelperEvent(path, value string) {
	if path == "-" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_, writeErr := fmt.Fprintln(file, value)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "write event:", writeErr, closeErr)
		os.Exit(2)
	}
}

func waitHelperFile(path string, limit time.Duration) {
	if path == "-" {
		return
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "timed out waiting for helper file")
	os.Exit(2)
}

func TestProcessStateIsPrivateAndContainsNoCommandOrEnvironment(t *testing.T) {
	fixture := newSessionFixture(t, "process-state", "healthy", "", 2*time.Second, 0)
	if err := fixture.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}

	statePath := filepath.Join(fixture.workDir, ".multica", "ui-test", fixture.taskID, "processes.json")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat process state: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("process state mode = %04o, want 0600", got)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read process state: %v", err)
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode process state: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("process records = %d, want 1", len(records))
	}
	allowed := map[string]bool{
		"task_id": true, "proxy_pid": true, "child_pid": true,
		"pgid": true, "kind": true, "started_at": true,
	}
	for key := range records[0] {
		if !allowed[key] {
			t.Fatalf("process state leaked unexpected field %q", key)
		}
	}
	for key := range allowed {
		if _, ok := records[0][key]; !ok {
			t.Fatalf("process state missing %q", key)
		}
	}
}

func TestCleanupTaskKillsOnlyExactTaskAndPreservesEvidence(t *testing.T) {
	first := newSessionFixture(t, "cleanup-a", "healthy", "", 2*time.Second, 0)
	second := newSessionFixtureAtWorkDir(t, first.workDir, "cleanup-b", "healthy", "", 2*time.Second, 0)
	if err := first.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("first EnsureReady() error = %v", err)
	}
	if err := second.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("second EnsureReady() error = %v", err)
	}
	firstPID := readHelperPID(t, first.descendantPID)
	secondPID := readHelperPID(t, second.descendantPID)

	if err := CleanupTask(first.workDir, first.taskID, nil); err != nil {
		t.Fatalf("CleanupTask() error = %v", err)
	}
	waitProcessGone(t, firstPID, 7*time.Second)
	if !platformProcessAlive(secondPID) {
		t.Fatal("CleanupTask killed another task's descendant")
	}
	if _, err := os.Stat(filepath.Join(first.artifactDir, "app.log")); err != nil {
		t.Fatalf("app evidence was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first.workDir, ".multica", "ui-test", first.taskID)); !os.IsNotExist(err) {
		t.Fatalf("task process state remains: %v", err)
	}
}

func TestCleanupTaskRetainsUnverifiableStaleMetadataAndDoesNotKillPID(t *testing.T) {
	workDir := t.TempDir()
	pidPath := filepath.Join(workDir, "unrelated.pid")
	unrelated := exec.Command(os.Args[0], "-test.run=^TestUITestHelperProcess$", "--", "descendant", pidPath)
	if err := unrelated.Start(); err != nil {
		t.Fatalf("start unrelated helper: %v", err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_ = unrelated.Wait()
	})
	pid := readHelperPID(t, pidPath)

	for _, test := range []struct {
		taskID    string
		proxyPID  int
		startedAt time.Time
	}{
		{taskID: "other-proxy", proxyPID: os.Getpid() + 1, startedAt: time.Now().UTC()},
		{taskID: "reused-proxy-pid", proxyPID: os.Getpid(), startedAt: proxyLifetimeStartedAt.Add(-time.Second)},
	} {
		t.Run(test.taskID, func(t *testing.T) {
			stateDir := filepath.Join(workDir, ".multica", "ui-test", test.taskID)
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			record := processRecord{
				TaskID: test.taskID, ProxyPID: test.proxyPID, ChildPID: pid,
				PGID: pid, Kind: "app", StartedAt: test.startedAt,
			}
			data, err := json.Marshal([]processRecord{record})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateDir, "processes.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}

			if err := CleanupTask(workDir, test.taskID, nil); err == nil {
				t.Fatal("CleanupTask() succeeded for unverifiable stale metadata")
			}
			if !platformProcessAlive(pid) {
				t.Fatal("CleanupTask killed unrelated reused PID")
			}
			if _, err := os.Stat(filepath.Join(stateDir, "processes.json")); err != nil {
				t.Fatalf("stale metadata was not retained: %v", err)
			}
		})
	}
}

func TestOwnedProcessStartOrdersAttachBeforeResumeAndAbortsFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		attachErr  error
		resumeErr  error
		wantEvents []string
	}{
		{name: "success", wantEvents: []string{"start", "attach", "resume"}},
		{name: "attach failure", attachErr: errors.New("attach"), wantEvents: []string{"start", "attach", "abort"}},
		{name: "resume failure", resumeErr: errors.New("resume"), wantEvents: []string{"start", "attach", "resume", "abort"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []string
			step := func(name string, err error) func() error {
				return func() error {
					events = append(events, name)
					return err
				}
			}
			err := runOwnedProcessStart(
				step("start", nil),
				step("attach", test.attachErr),
				step("resume", test.resumeErr),
				step("abort", nil),
			)
			if (test.attachErr != nil || test.resumeErr != nil) != (err != nil) {
				t.Fatalf("runOwnedProcessStart() error = %v", err)
			}
			if got, want := strings.Join(events, ","), strings.Join(test.wantEvents, ","); got != want {
				t.Fatalf("events = %q, want %q", got, want)
			}
		})
	}
}

type retryProcessController struct {
	mu           sync.Mutex
	terminateErr error
	closeErr     error
	terminations int
	closes       int
}

func (c *retryProcessController) terminate(time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.terminations++
	return c.terminateErr
}

func (c *retryProcessController) attach(*exec.Cmd) error { return nil }

func (c *retryProcessController) resume(*exec.Cmd) error { return nil }

func (c *retryProcessController) abort(*exec.Cmd, time.Duration) error {
	return c.terminate(0)
}

func (c *retryProcessController) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
	return c.closeErr
}

type startFailureController struct {
	mu        sync.Mutex
	attachErr error
	resumeErr error
	abortErr  error
	closeErr  error
}

func (c *startFailureController) attach(*exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attachErr
}

func (c *startFailureController) resume(*exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resumeErr
}

func (c *startFailureController) abort(cmd *exec.Cmd, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.abortErr != nil {
		return c.abortErr
	}
	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}

func (c *startFailureController) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

type blockingAbortController struct {
	*startFailureController
	abortEntered chan struct{}
	releaseAbort chan struct{}
	blockOnce    sync.Once
}

func (c *blockingAbortController) abort(cmd *exec.Cmd, grace time.Duration) error {
	c.blockOnce.Do(func() {
		close(c.abortEntered)
		<-c.releaseAbort
	})
	return c.startFailureController.abort(cmd, grace)
}

func TestCleanupRejectsProcessStartNotAdmittedBeforeClose(t *testing.T) {
	workDir := t.TempDir()
	registry, err := newProcessRegistry(workDir, "cleanup-before-admission", nil)
	if err != nil {
		t.Fatal(err)
	}
	releaseAbort := make(chan struct{})
	controller := &blockingAbortController{
		startFailureController: &startFailureController{},
		abortEntered:           make(chan struct{}),
		releaseAbort:           releaseAbort,
	}
	done := make(chan struct{})
	close(done)
	process := &managedProcess{
		controller: controller,
		registry:   registry,
		done:       done,
		record: processRecord{
			TaskID: registry.taskID, ProxyPID: os.Getpid(), ChildPID: 424243,
			Kind: "app", StartedAt: time.Now().UTC(),
		},
	}
	if err := registry.add(process); err != nil {
		t.Fatal(err)
	}

	cleanupResult := make(chan error, 1)
	go func() { cleanupResult <- registry.cleanup() }()
	<-controller.abortEntered

	pidPath := filepath.Join(workDir, "late-start.pid")
	logPath := filepath.Join(workDir, "late-start", "app.log")
	_, startErr := startManagedProcess(
		registry, "app", helperCommand("descendant", pidPath),
		workDir, os.Environ(), logPath,
	)
	close(releaseAbort)
	cleanupErr := <-cleanupResult

	if startErr == nil || !strings.Contains(startErr.Error(), "process registry is closed") {
		t.Fatalf("startManagedProcess() error = %v, want closed admission", startErr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("startManagedProcess() created resources after cleanup closed admission: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("startManagedProcess() spawned after cleanup closed admission: %v", err)
	}
	if cleanupErr != nil {
		t.Fatalf("cleanup() error = %v", cleanupErr)
	}
}

func TestCleanupWaitsForAdmittedStartBeforeSpawn(t *testing.T) {
	workDir := t.TempDir()
	registry, err := newProcessRegistry(workDir, "cleanup-between-admission-and-spawn", nil)
	if err != nil {
		t.Fatal(err)
	}
	finishStart, err := registry.admitStart()
	if err != nil {
		t.Fatal(err)
	}
	cleanupResult := make(chan error, 1)
	go func() { cleanupResult <- registry.cleanup() }()
	waitForRegistryAdmissionClosed(t, registry)

	cmd := newWaitingHelperCommand(t)
	process, startErr := startManagedCommandAdmitted(
		registry, "app", cmd, &startFailureController{}, cmd.Start,
	)
	finishStart()
	cleanupErr := <-cleanupResult

	if startErr != nil {
		t.Fatalf("admitted start error = %v", startErr)
	}
	if process == nil || cmd.Process == nil {
		t.Fatal("admitted start did not spawn after cleanup closed admission")
	}
	if cleanupErr != nil {
		t.Fatalf("cleanup() error = %v", cleanupErr)
	}
	if platformProcessAlive(cmd.Process.Pid) {
		_ = cmd.Process.Kill()
		t.Fatal("cleanup did not stop admitted process")
	}
	if _, ok := liveProcessRegistries.Load(registry.stateDir); ok {
		t.Fatal("successful cleanup retained live registry")
	}
}

func TestCleanupRetainsAdmittedOwnerAddedAfterCloseWhenAbortFails(t *testing.T) {
	workDir := t.TempDir()
	registry, err := newProcessRegistry(workDir, "cleanup-between-spawn-and-add", nil)
	if err != nil {
		t.Fatal(err)
	}
	finishStart, err := registry.admitStart()
	if err != nil {
		t.Fatal(err)
	}
	cmd := newWaitingHelperCommand(t)
	attachErr := errors.New("injected attach failure")
	abortErr := errors.New("injected abort failure")
	controller := &startFailureController{attachErr: attachErr, abortErr: abortErr}
	spawned := make(chan struct{})
	allowAdd := make(chan struct{})
	startResult := make(chan error, 1)
	go func() {
		defer finishStart()
		_, err := startManagedCommandAdmitted(
			registry, "app", cmd, controller,
			func() error {
				if err := cmd.Start(); err != nil {
					return err
				}
				close(spawned)
				<-allowAdd
				return nil
			},
		)
		startResult <- err
	}()
	<-spawned
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		liveProcessRegistries.Delete(registry.stateDir)
	})

	cleanupResult := make(chan error, 1)
	go func() { cleanupResult <- registry.cleanup() }()
	waitForRegistryAdmissionClosed(t, registry)
	close(allowAdd)
	startErr := <-startResult
	cleanupErr := <-cleanupResult

	if !errors.Is(startErr, attachErr) || !errors.Is(startErr, abortErr) {
		t.Fatalf("admitted start error = %v, want attach and abort failures", startErr)
	}
	if !errors.Is(cleanupErr, abortErr) {
		t.Fatalf("cleanup() error = %v, want abort failure", cleanupErr)
	}
	assertRetryableOwner(t, registry)
	assertPersistedOwner(t, registry, 1)

	controller.mu.Lock()
	controller.abortErr = nil
	controller.mu.Unlock()
	if err := registry.cleanup(); err != nil {
		t.Fatalf("retry cleanup error = %v", err)
	}
}

func TestConcurrentCleanupCallersShareFailureAndLaterRetrySucceeds(t *testing.T) {
	workDir := t.TempDir()
	registry, err := newProcessRegistry(workDir, "concurrent-cleanup", nil)
	if err != nil {
		t.Fatal(err)
	}
	abortErr := errors.New("injected abort failure")
	controller := &blockingAbortController{
		startFailureController: &startFailureController{abortErr: abortErr},
		abortEntered:           make(chan struct{}),
		releaseAbort:           make(chan struct{}),
	}
	done := make(chan struct{})
	close(done)
	process := &managedProcess{
		controller: controller,
		registry:   registry,
		done:       done,
		record: processRecord{
			TaskID: registry.taskID, ProxyPID: os.Getpid(), ChildPID: 424244,
			Kind: "app", StartedAt: time.Now().UTC(),
		},
	}
	if err := registry.add(process); err != nil {
		t.Fatal(err)
	}

	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	go func() { firstResult <- CleanupTask(workDir, registry.taskID, nil) }()
	<-controller.abortEntered
	go func() { secondResult <- CleanupTask(workDir, registry.taskID, nil) }()
	waitForCleanupWaiters(t, registry, 1)
	close(controller.releaseAbort)

	firstErr := <-firstResult
	secondErr := <-secondResult
	if !errors.Is(firstErr, abortErr) || !errors.Is(secondErr, abortErr) {
		t.Fatalf("cleanup errors = (%v, %v), want shared abort failure", firstErr, secondErr)
	}
	assertRetryableOwner(t, registry)
	if finishStart, err := registry.admitStart(); err == nil {
		finishStart()
		t.Fatal("failed cleanup reopened process-start admission")
	}

	controller.mu.Lock()
	controller.abortErr = nil
	controller.mu.Unlock()
	if err := CleanupTask(workDir, registry.taskID, nil); err != nil {
		t.Fatalf("retry cleanup error = %v", err)
	}
	if _, ok := liveProcessRegistries.Load(registry.stateDir); ok {
		t.Fatal("successful retry retained live registry")
	}
}

func TestManagedCommandRetainsOwnerWhenFailureCleanupFails(t *testing.T) {
	for _, test := range []struct {
		name        string
		command     func(*testing.T) *exec.Cmd
		controller  func() *startFailureController
		wantPrimary func(*startFailureController) error
		wantError   func(*startFailureController) error
	}{
		{
			name: "start failure and controller close failure",
			command: func(t *testing.T) *exec.Cmd {
				return exec.Command(filepath.Join(t.TempDir(), "missing-ui-test-command"))
			},
			controller: func() *startFailureController {
				return &startFailureController{closeErr: errors.New("injected close failure")}
			},
			wantError: func(controller *startFailureController) error { return controller.closeErr },
		},
		{
			name:    "attach failure and abort failure",
			command: newWaitingHelperCommand,
			controller: func() *startFailureController {
				return &startFailureController{
					attachErr: errors.New("injected attach failure"),
					abortErr:  errors.New("injected abort failure"),
				}
			},
			wantPrimary: func(controller *startFailureController) error { return controller.attachErr },
			wantError:   func(controller *startFailureController) error { return controller.abortErr },
		},
		{
			name:    "resume failure and abort failure",
			command: newWaitingHelperCommand,
			controller: func() *startFailureController {
				return &startFailureController{
					resumeErr: errors.New("injected resume failure"),
					abortErr:  errors.New("injected abort failure"),
				}
			},
			wantPrimary: func(controller *startFailureController) error { return controller.resumeErr },
			wantError:   func(controller *startFailureController) error { return controller.abortErr },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			taskID := strings.ReplaceAll(test.name, " ", "-")
			registry, err := newProcessRegistry(workDir, taskID, nil)
			if err != nil {
				t.Fatal(err)
			}
			cmd := test.command(t)
			controller := test.controller()
			t.Cleanup(func() {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				liveProcessRegistries.Delete(registry.stateDir)
			})

			process, err := startManagedCommand(registry, "app", cmd, controller)
			if process != nil {
				t.Fatal("startManagedCommand() returned process on lifecycle failure")
			}
			if !errors.Is(err, test.wantError(controller)) {
				t.Fatalf("startManagedCommand() error = %v, want cleanup error", err)
			}
			if !strings.Contains(err.Error(), "cleanup failed process owner") {
				t.Fatalf("startManagedCommand() error = %v, want cleanup failure context", err)
			}
			if test.wantPrimary != nil {
				if primary := test.wantPrimary(controller); !errors.Is(err, primary) {
					t.Fatalf("startManagedCommand() error = %v, want primary error %v", err, primary)
				}
			} else {
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) {
					t.Fatalf("startManagedCommand() error = %v, want command start path error", err)
				}
			}
			assertRetryableOwner(t, registry)
			assertPersistedOwner(t, registry, 1)
			if cmd.Process != nil && !platformProcessAlive(cmd.Process.Pid) {
				t.Fatal("failed cleanup did not leave process available for retry")
			}

			controller.mu.Lock()
			controller.abortErr = nil
			controller.closeErr = nil
			controller.mu.Unlock()
			if err := CleanupTask(workDir, taskID, nil); err != nil {
				t.Fatalf("CleanupTask() retry error = %v", err)
			}
			if _, err := os.Stat(registry.stateDir); !os.IsNotExist(err) {
				t.Fatalf("metadata remains after retry: %v", err)
			}
		})
	}
}

func TestManagedCommandKeepsDistinctOwnersForMultipleFailedStarts(t *testing.T) {
	workDir := t.TempDir()
	registry, err := newProcessRegistry(workDir, "distinct-failed-starts", nil)
	if err != nil {
		t.Fatal(err)
	}
	controllers := make([]*startFailureController, 2)
	for index := range controllers {
		controller := &startFailureController{closeErr: errors.New("injected close failure")}
		controllers[index] = controller
		cmd := exec.Command(filepath.Join(t.TempDir(), "missing-ui-test-command"))
		if _, err := startManagedCommand(registry, "app", cmd, controller); !errors.Is(err, controller.closeErr) {
			t.Fatalf("startManagedCommand() error = %v, want close failure", err)
		}
	}
	assertRetryableOwnerCount(t, registry, 2)
	assertPersistedOwner(t, registry, 2)

	for _, controller := range controllers {
		controller.mu.Lock()
		controller.closeErr = nil
		controller.mu.Unlock()
	}
	if err := CleanupTask(workDir, registry.taskID, nil); err != nil {
		t.Fatalf("CleanupTask() retry error = %v", err)
	}
}

func TestManagedCommandRetainsOwnerWhenPersistenceAndAbortFail(t *testing.T) {
	workDir := t.TempDir()
	registry, err := newProcessRegistry(workDir, "persist-abort-failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(registry.statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := newWaitingHelperCommand(t)
	controller := &startFailureController{abortErr: errors.New("injected abort failure")}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		liveProcessRegistries.Delete(registry.stateDir)
	})

	process, err := startManagedCommand(registry, "app", cmd, controller)
	if process != nil {
		t.Fatal("startManagedCommand() returned process after persistence failure")
	}
	if err == nil || !errors.Is(err, controller.abortErr) {
		t.Fatalf("startManagedCommand() error = %v, want persistence and abort failures", err)
	}
	if !strings.Contains(err.Error(), "persist process ownership") ||
		!strings.Contains(err.Error(), "cleanup failed process owner") {
		t.Fatalf("startManagedCommand() error = %v, want persistence and cleanup context", err)
	}
	assertRetryableOwner(t, registry)
	if !platformProcessAlive(cmd.Process.Pid) {
		t.Fatal("failed cleanup did not leave process available for retry")
	}

	if err := os.Remove(registry.statePath); err != nil {
		t.Fatalf("remove injected metadata directory: %v", err)
	}
	controller.mu.Lock()
	controller.abortErr = nil
	controller.mu.Unlock()
	if err := CleanupTask(workDir, registry.taskID, nil); err != nil {
		t.Fatalf("CleanupTask() retry error = %v", err)
	}
}

func newWaitingHelperCommand(t *testing.T) *exec.Cmd {
	return exec.Command(
		os.Args[0], "-test.run=^TestUITestHelperProcess$", "--",
		"descendant", filepath.Join(t.TempDir(), "pid"),
	)
}

func assertRetryableOwner(t *testing.T, registry *processRegistry) {
	t.Helper()
	assertRetryableOwnerCount(t, registry, 1)
}

func assertRetryableOwnerCount(t *testing.T, registry *processRegistry, want int) {
	t.Helper()
	registry.mu.Lock()
	count := len(registry.processes)
	registry.mu.Unlock()
	if count != want {
		t.Fatalf("retryable process owners = %d, want %d", count, want)
	}
	if _, ok := liveProcessRegistries.Load(registry.stateDir); !ok {
		t.Fatal("retryable owner not reachable through exact-task cleanup")
	}
}

func assertPersistedOwner(t *testing.T, registry *processRegistry, want int) {
	t.Helper()
	data, err := os.ReadFile(registry.statePath)
	if err != nil {
		t.Fatalf("read process metadata: %v", err)
	}
	var records []processRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode process metadata: %v", err)
	}
	if len(records) != want {
		t.Fatalf("persisted process owners = %d, want %d", len(records), want)
	}
}

func waitForRegistryAdmissionClosed(t *testing.T, registry *processRegistry) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		registry.mu.Lock()
		closed := registry.admissionClosed
		registry.mu.Unlock()
		if closed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not close process-start admission")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForCleanupWaiters(t *testing.T, registry *processRegistry, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		registry.mu.Lock()
		waiters := 0
		if registry.cleanupAttempt != nil {
			waiters = registry.cleanupAttempt.waiters
		}
		registry.mu.Unlock()
		if waiters >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup waiters = %d, want at least %d", waiters, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFailedTerminationRetainsRetryableOwnerAndMetadata(t *testing.T) {
	for _, test := range []struct {
		name               string
		terminateErr       error
		closeErr           error
		wantCloseAttempts  int
		wantTerminateCalls int
	}{
		{
			name: "termination failure", terminateErr: errors.New("injected termination failure"),
			wantCloseAttempts: 1, wantTerminateCalls: 2,
		},
		{
			name: "handle close failure", closeErr: errors.New("injected close failure"),
			wantCloseAttempts: 2, wantTerminateCalls: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			taskID := strings.ReplaceAll(test.name, " ", "-")
			registry, err := newProcessRegistry(workDir, taskID, nil)
			if err != nil {
				t.Fatal(err)
			}
			controller := &retryProcessController{
				terminateErr: test.terminateErr,
				closeErr:     test.closeErr,
			}
			done := make(chan struct{})
			close(done)
			process := &managedProcess{
				controller: controller,
				registry:   registry,
				done:       done,
				record: processRecord{
					TaskID: taskID, ProxyPID: os.Getpid(), ChildPID: 424242,
					Kind: "app", StartedAt: time.Now().UTC(),
				},
			}
			if err := registry.add(process); err != nil {
				t.Fatal(err)
			}

			if err := registry.cleanup(); err == nil {
				t.Fatal("cleanup succeeded despite injected ownership failure")
			}
			if _, ok := registry.processes[process.record.ChildPID]; !ok {
				t.Fatal("failed process owner was removed from registry")
			}
			if _, err := os.Stat(registry.statePath); err != nil {
				t.Fatalf("failed process metadata was not retained: %v", err)
			}
			if _, ok := liveProcessRegistries.Load(registry.stateDir); !ok {
				t.Fatal("failed process owner was removed from live registry")
			}

			controller.mu.Lock()
			controller.terminateErr = nil
			controller.closeErr = nil
			controller.mu.Unlock()
			if err := registry.cleanup(); err != nil {
				t.Fatalf("retry cleanup error = %v", err)
			}
			if controller.terminations != test.wantTerminateCalls {
				t.Fatalf("termination attempts = %d, want %d", controller.terminations, test.wantTerminateCalls)
			}
			if controller.closes != test.wantCloseAttempts {
				t.Fatalf("close attempts = %d, want %d", controller.closes, test.wantCloseAttempts)
			}
			if _, err := os.Stat(registry.stateDir); !os.IsNotExist(err) {
				t.Fatalf("metadata remains after successful retry: %v", err)
			}
		})
	}
}

func TestSessionCleanupEscalatesTERMResistantDescendant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Job Objects terminate without Unix signal escalation")
	}
	fixture := newSessionFixture(t, "resistant", "healthy-resistant", "", 2*time.Second, 0)
	if err := fixture.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	pid := readHelperPID(t, fixture.descendantPID)
	if err := fixture.session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	waitProcessGone(t, pid, processShutdownGrace+2*time.Second)
}

type sessionFixture struct {
	session       *Session
	workDir       string
	taskID        string
	artifactDir   string
	events        string
	descendantPID string
}

func newSessionFixture(t *testing.T, taskID, behavior, setup string, startupLimit, sessionLimit time.Duration) sessionFixture {
	t.Helper()
	return newSessionFixtureAtWorkDir(t, t.TempDir(), taskID, behavior, setup, startupLimit, sessionLimit)
}

func newSessionFixtureAtWorkDir(t *testing.T, workDir, taskID, behavior, setup string, startupLimit, sessionLimit time.Duration) sessionFixture {
	t.Helper()
	address := freeLoopbackAddress(t)
	events := filepath.Join(workDir, taskID+"-events")
	descendantPID := filepath.Join(workDir, taskID+"-descendant.pid")
	artifactDir := filepath.Join(workDir, ".multica", "artifacts", "ui-test", taskID)
	baseURL := "http://" + address
	config, err := loadManifest([]byte(fmt.Sprintf(
		`{"start":%q,"url":%q,"health":"/health","setup":%q}`,
		helperCommand("app", address, events, descendantPID, behavior),
		baseURL,
		setup,
	)))
	if err != nil {
		t.Fatalf("load test manifest: %v", err)
	}
	session, err := NewSession(SessionOptions{
		WorkDir:      workDir,
		TaskID:       taskID,
		Config:       config,
		ArtifactDir:  artifactDir,
		StartupLimit: startupLimit,
		SetupLimit:   5 * time.Second,
		SessionLimit: sessionLimit,
	}, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return sessionFixture{
		session: session, workDir: workDir, taskID: taskID,
		artifactDir: artifactDir, events: events, descendantPID: descendantPID,
	}
}

func helperCommand(args ...string) string {
	parts := []string{commandQuote(os.Args[0]), "-test.run=^TestUITestHelperProcess$", "--"}
	for _, arg := range args {
		parts = append(parts, commandQuote(arg))
	}
	return strings.Join(parts, " ")
}

func commandQuote(value string) string {
	if runtime.GOOS == "windows" {
		return strconv.Quote(value)
	}
	return "'" + strings.ReplaceAll(value, "'", `'\"'\"'`) + "'"
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return address
}

func readHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse helper PID: %v", parseErr)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("read helper PID: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitProcessGone(t *testing.T, pid int, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for platformProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if platformProcessAlive(pid) {
		t.Fatalf("process %d remains alive", pid)
	}
}

func readEvents(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return strings.Fields(string(data))
}

func countEvent(events []string, target string) int {
	count := 0
	for _, event := range events {
		if event == target {
			count++
		}
	}
	return count
}
