package uitest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionConstructionStartsNoProcess(t *testing.T) {
	fixture := newSessionFixture(t, "lazy", "healthy", "", 2*time.Second, 0)
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(fixture.events); !os.IsNotExist(err) {
		t.Fatalf("construction started app: %v", err)
	}
}

func TestSessionFirstActionStartsOnceAndOrdersHealthBeforeSetup(t *testing.T) {
	workDir := t.TempDir()
	setup := helperCommand("setup", filepath.Join(workDir, "ordered-events"), "success")
	fixture := newSessionFixtureAtWorkDir(t, workDir, "ordered", "healthy", setup, 2*time.Second, 0)

	if err := fixture.session.RunBrowserAction(context.Background(), func(context.Context) error {
		appendHelperEvent(fixture.events, "browser")
		return nil
	}); err != nil {
		t.Fatalf("first RunBrowserAction() error = %v", err)
	}
	if err := fixture.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("second EnsureReady() error = %v", err)
	}

	events := readEvents(t, fixture.events)
	if countEvent(events, "app") != 1 {
		t.Fatalf("app starts = %d, want 1; events=%v", countEvent(events, "app"), events)
	}
	want := []string{"app", "health", "setup", "browser"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSessionBrowserActionReturnsProductFailureUnclassified(t *testing.T) {
	fixture := newSessionFixture(t, "browser-product-failure", "healthy", "", 2*time.Second, 0)
	productErr := errors.New("product assertion failed")
	err := fixture.session.RunBrowserAction(context.Background(), func(context.Context) error {
		return productErr
	})
	if !errors.Is(err, productErr) {
		t.Fatalf("RunBrowserAction() error = %v, want product error", err)
	}
	var lifecycleErr *LifecycleError
	if errors.As(err, &lifecycleErr) {
		t.Fatalf("product error was classified as lifecycle error: %v", err)
	}
}

func TestSessionConcurrentFirstCallsShareInitialization(t *testing.T) {
	workDir := t.TempDir()
	events := filepath.Join(workDir, "concurrent-events")
	setup := helperCommand("setup", events, "success")
	fixture := newSessionFixtureAtWorkDir(t, workDir, "concurrent", "healthy", setup, 2*time.Second, 0)

	const calls = 24
	start := make(chan struct{})
	errs := make(chan error, calls)
	for range calls {
		go func() {
			<-start
			errs <- fixture.session.EnsureReady(context.Background())
		}()
	}
	close(start)
	for range calls {
		if err := <-errs; err != nil {
			t.Fatalf("EnsureReady() error = %v", err)
		}
	}
	eventsFound := readEvents(t, events)
	if countEvent(eventsFound, "app") != 1 || countEvent(eventsFound, "setup") != 1 {
		t.Fatalf("events = %v, want one app and setup", eventsFound)
	}
}

func TestSessionTerminalFailuresKillDescendants(t *testing.T) {
	for _, test := range []struct {
		name       string
		app        string
		setup      string
		wantClass  string
		cancel     bool
		startLimit time.Duration
	}{
		{name: "application failure", app: "fail", wantClass: ErrorApplicationStart, startLimit: 2 * time.Second},
		{name: "health timeout", app: "no-health", wantClass: ErrorHealthTimeout, startLimit: 120 * time.Millisecond},
		{name: "setup failure", app: "healthy", setup: "fail", wantClass: ErrorSetup, startLimit: 2 * time.Second},
		{name: "setup policy", app: "healthy", setup: "external-cookie", wantClass: ErrorPolicy, startLimit: 2 * time.Second},
		{name: "cancellation", app: "no-health", wantClass: ErrorCancelled, cancel: true, startLimit: 5 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			events := filepath.Join(workDir, "events")
			setup := ""
			if test.setup != "" {
				setup = helperCommand("setup", events, test.setup)
			}
			fixture := newSessionFixtureAtWorkDir(t, workDir, "terminal", test.app, setup, test.startLimit, 0)
			ctx, cancel := context.WithCancel(context.Background())
			if test.cancel {
				go func() {
					_ = waitForFile(fixture.descendantPID, 2*time.Second)
					cancel()
				}()
			} else {
				defer cancel()
			}
			err := fixture.session.EnsureReady(ctx)
			assertLifecycleClass(t, err, test.wantClass)
			pid := readHelperPID(t, fixture.descendantPID)
			waitProcessGone(t, pid, 7*time.Second)
		})
	}
}

func TestSessionSetupTimeoutAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name      string
		cancel    bool
		wantClass string
	}{
		{name: "timeout", wantClass: ErrorSetup},
		{name: "cancellation", cancel: true, wantClass: ErrorCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			events := filepath.Join(workDir, "events")
			setup := helperCommand("setup", events, "hang")
			fixture := newSessionFixtureAtWorkDir(t, workDir, "setup-terminal", "healthy", setup, 2*time.Second, 0)
			fixture.session.opts.SetupLimit = 150 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				go func() {
					_ = waitForEvent(events, "setup", 2*time.Second)
					cancel()
				}()
			}
			assertLifecycleClass(t, fixture.session.EnsureReady(ctx), test.wantClass)
			pid := readHelperPID(t, fixture.descendantPID)
			waitProcessGone(t, pid, 7*time.Second)
		})
	}
}

func TestSessionPostReadyApplicationExitEndsSession(t *testing.T) {
	fixture := newSessionFixture(t, "post-ready-exit", "healthy-exit", "", 2*time.Second, 0)
	if err := fixture.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := fixture.session.EnsureReady(context.Background())
		if err != nil {
			assertLifecycleClass(t, err, ErrorApplicationStart)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("session did not end after post-ready application exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestApplicationExitPrefersConcurrentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.DeadlineExceeded)
	err := applicationExitError(ctx, context.Background(), "health check", errors.New("application exited"))
	assertLifecycleClass(t, err, ErrorCancelled)
}

func TestSessionCloseAndTimeoutKillDescendantsAndPreserveLogs(t *testing.T) {
	t.Run("normal close is idempotent", func(t *testing.T) {
		fixture := newSessionFixture(t, "close", "healthy", "", 2*time.Second, 0)
		if err := fixture.session.EnsureReady(context.Background()); err != nil {
			t.Fatalf("EnsureReady() error = %v", err)
		}
		pid := readHelperPID(t, fixture.descendantPID)
		if err := fixture.session.Close(); err != nil {
			t.Fatalf("first Close() error = %v", err)
		}
		if err := fixture.session.Close(); err != nil {
			t.Fatalf("second Close() error = %v", err)
		}
		waitProcessGone(t, pid, 7*time.Second)
		data, err := os.ReadFile(filepath.Join(fixture.artifactDir, "app.log"))
		if err != nil {
			t.Fatalf("read app log: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("app log evidence is empty")
		}
		if _, err := os.Stat(filepath.Join(fixture.workDir, ".multica", "ui-test", fixture.taskID)); !os.IsNotExist(err) {
			t.Fatalf("task state remains after close: %v", err)
		}
	})

	t.Run("session timeout", func(t *testing.T) {
		fixture := newSessionFixture(t, "timeout", "healthy", "", 2*time.Second, 700*time.Millisecond)
		if err := fixture.session.EnsureReady(context.Background()); err != nil {
			t.Fatalf("EnsureReady() error = %v", err)
		}
		pid := readHelperPID(t, fixture.descendantPID)
		waitProcessGone(t, pid, 7*time.Second)
		assertLifecycleClass(t, fixture.session.EnsureReady(context.Background()), ErrorCancelled)
	})
}

func TestSessionRejectsHealthRedirectOutsideConfiguredOrigin(t *testing.T) {
	redirectTarget := "http://" + freeLoopbackAddress(t) + "/health"
	fixture := newSessionFixture(t, "redirect", "redirect", "", 2*time.Second, 0)

	// Replace config with an app command carrying redirect target.
	config := fixture.session.opts.Config
	config.StartCommand = helperCommand("app", config.BaseURL.Host, fixture.events, fixture.descendantPID, "redirect", redirectTarget)
	fixture.session.opts.Config = config

	err := fixture.session.EnsureReady(context.Background())
	assertLifecycleClass(t, err, ErrorPolicy)
	pid := readHelperPID(t, fixture.descendantPID)
	waitProcessGone(t, pid, 7*time.Second)
}

func TestSessionRejectsUnsafeOptions(t *testing.T) {
	config, err := loadManifest([]byte(`{"start":"true","url":"http://127.0.0.1:3000","health":"/"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*SessionOptions)
	}{
		{name: "task traversal", edit: func(o *SessionOptions) { o.TaskID = "../other" }},
		{name: "artifact outside task", edit: func(o *SessionOptions) { o.ArtifactDir = t.TempDir() }},
		{name: "health different origin", edit: func(o *SessionOptions) {
			other, _ := ValidateLoopbackURL("http://127.0.0.1:3001/")
			o.Config.HealthURL = other
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			options := SessionOptions{
				WorkDir: workDir, TaskID: "safe", Config: config,
				ArtifactDir: filepath.Join(workDir, ".multica", "artifacts", "ui-test", "safe"),
			}
			test.edit(&options)
			_, err := NewSession(options, nil)
			assertLifecycleClass(t, err, ErrorPolicy)
		})
	}
}

func assertLifecycleClass(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want class %q", want)
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Fatalf("error %T %v is not LifecycleError", err, err)
	}
	if lifecycleErr.Class != want {
		t.Fatalf("error class = %q, want %q: %v", lifecycleErr.Class, want, err)
	}
}

func waitForFile(path string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return os.ErrDeadlineExceeded
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForEvent(path, event string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			for _, found := range strings.Fields(string(data)) {
				if found == event {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return os.ErrDeadlineExceeded
		}
		time.Sleep(10 * time.Millisecond)
	}
}
