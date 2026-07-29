package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/uitest"
	"github.com/spf13/cobra"
)

type fakeUITestRuntime struct {
	status      uitest.CapabilityStatus
	installErr  error
	installRuns int
}

func (f *fakeUITestRuntime) Status() uitest.CapabilityStatus {
	return f.status
}

func (f *fakeUITestRuntime) Install(context.Context) (uitest.CapabilityStatus, error) {
	f.installRuns++
	return f.status, f.installErr
}

func TestUITestCommandStatusJSONUsesProfileAndEmitsOneObject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fake := &fakeUITestRuntime{status: uitest.CapabilityStatus{
		Status:  uitest.StatusBroken,
		Version: uitest.PlaywrightMCPVersion,
		Error:   "missing Chromium",
	}}
	var gotRoot string
	restore := replaceUITestRuntimeFactory(func(root string) uiTestRuntime {
		gotRoot = root
		return fake
	})
	defer restore()

	cmd, stdout, _ := executeUITestCommand(t, "--profile", "dev", "ui-test", "status", "--output", "json")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command error = %v", err)
	}
	if gotRoot != filepath.Join(home, ".multica", "profiles", "dev", "ui-test") {
		t.Fatalf("runtime root = %q, want named profile layout", gotRoot)
	}
	if got := stdout.String(); got != `{"status":"broken","version":"0.0.78","error":"missing Chromium"}`+"\n" {
		t.Fatalf("stdout = %q, want exactly one status object", got)
	}
}

func TestUITestCommandStatusSucceedsForEveryCapabilityState(t *testing.T) {
	for _, state := range []string{
		uitest.StatusUnavailable,
		uitest.StatusInstalling,
		uitest.StatusReady,
		uitest.StatusBroken,
	} {
		t.Run(state, func(t *testing.T) {
			fake := &fakeUITestRuntime{status: uitest.CapabilityStatus{Status: state}}
			restore := replaceUITestRuntimeFactory(func(string) uiTestRuntime { return fake })
			defer restore()

			cmd, _, _ := executeUITestCommand(t, "ui-test", "status")
			if err := cmd.Execute(); err != nil {
				t.Fatalf("status command error = %v", err)
			}
		})
	}
}

func TestUITestCommandInstallPrintsProgressAndFinalStatus(t *testing.T) {
	fake := &fakeUITestRuntime{status: uitest.CapabilityStatus{
		Status:  uitest.StatusReady,
		Version: uitest.PlaywrightMCPVersion,
	}}
	restore := replaceUITestRuntimeFactory(func(string) uiTestRuntime { return fake })
	defer restore()

	cmd, stdout, stderr := executeUITestCommand(t, "ui-test", "install")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install command error = %v", err)
	}
	if fake.installRuns != 1 {
		t.Fatalf("Install() calls = %d, want 1", fake.installRuns)
	}
	if !strings.Contains(stderr.String(), "Installing") {
		t.Fatalf("stderr = %q, want progress", stderr.String())
	}
	if !strings.Contains(stdout.String(), "ready") || !strings.Contains(stdout.String(), uitest.PlaywrightMCPVersion) {
		t.Fatalf("stdout = %q, want final ready status", stdout.String())
	}
}

func TestUITestCommandRejectsMalformedArguments(t *testing.T) {
	restore := replaceUITestRuntimeFactory(func(string) uiTestRuntime {
		return &fakeUITestRuntime{installErr: errors.New("must not run")}
	})
	defer restore()

	for _, args := range [][]string{
		{"ui-test", "install", "extra"},
		{"ui-test", "status", "extra"},
		{"ui-test", "status", "--output", "yaml"},
		{"ui-test", "serve", "extra"},
	} {
		cmd, _, _ := executeUITestCommand(t, args...)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("%v succeeded, want argument error", args)
		}
	}
}

func TestUITestServeRequiresTaskAndReadyRuntime(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("MULTICA_UI_TEST_WORKDIR", workDir)
	t.Setenv("MULTICA_UI_TEST_TASK_ID", "")
	t.Setenv("MULTICA_UI_TEST_RUNTIME_DIR", "")
	called := false
	restore := replaceUITestServer(func(context.Context, uitest.ServeOptions) error {
		called = true
		return nil
	})
	defer restore()

	cmd, _, _ := executeUITestCommand(t, "ui-test", "serve")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "MULTICA_UI_TEST_TASK_ID") {
		t.Fatalf("serve error = %v, want task ID requirement", err)
	}
	if called {
		t.Fatal("server ran without task ID and ready runtime")
	}

	t.Setenv("MULTICA_UI_TEST_TASK_ID", "task-1")
	cmd, _, _ = executeUITestCommand(t, "ui-test", "serve")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "MULTICA_UI_TEST_RUNTIME_DIR") {
		t.Fatalf("serve error = %v, want runtime requirement", err)
	}
}

func TestUITestServePassesCanonicalEnvironmentAndProtocolStreams(t *testing.T) {
	workDir := t.TempDir()
	runtimeDir := t.TempDir()
	t.Setenv("MULTICA_UI_TEST_WORKDIR", workDir)
	t.Setenv("MULTICA_UI_TEST_TASK_ID", "task-7")
	t.Setenv("MULTICA_UI_TEST_RUNTIME_DIR", runtimeDir)
	var got uitest.ServeOptions
	restore := replaceUITestServer(func(_ context.Context, options uitest.ServeOptions) error {
		got = options
		_, _ = io.WriteString(options.Output, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
		_, _ = io.WriteString(options.ErrorOutput, "upstream log\n")
		return nil
	})
	defer restore()

	cmd, stdout, stderr := executeUITestCommand(t, "ui-test", "serve")
	cmd.SetIn(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("serve command error = %v", err)
	}
	canonicalWorkDir, _ := filepath.Abs(workDir)
	canonicalRuntimeDir, _ := filepath.Abs(runtimeDir)
	if got.WorkDir != canonicalWorkDir || got.TaskID != "task-7" || got.Runtime.Directory != canonicalRuntimeDir {
		t.Fatalf("serve options = %#v", got)
	}
	if got.Input != cmd.InOrStdin() {
		t.Fatal("serve input is not command stdin")
	}
	if strings.Contains(stdout.String(), "upstream log") ||
		!strings.Contains(stdout.String(), `"jsonrpc":"2.0"`) ||
		!strings.Contains(stderr.String(), "upstream log") {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestUITestServeFallsBackToCurrentDirectoryWhenWorkdirAbsent(t *testing.T) {
	t.Setenv("MULTICA_UI_TEST_WORKDIR", "")
	t.Setenv("MULTICA_UI_TEST_TASK_ID", "task-openclaw")
	t.Setenv("MULTICA_UI_TEST_RUNTIME_DIR", t.TempDir())
	var got uitest.ServeOptions
	restore := replaceUITestServer(func(_ context.Context, options uitest.ServeOptions) error {
		got = options
		return nil
	})
	defer restore()

	cmd, _, _ := executeUITestCommand(t, "ui-test", "serve")
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	current, _ := os.Getwd()
	current, _ = filepath.Abs(current)
	if got.WorkDir != current {
		t.Fatalf("workdir = %q, want current directory %q", got.WorkDir, current)
	}
}

func TestUITestServeReturnsCancellationWithoutProtocolNoise(t *testing.T) {
	t.Setenv("MULTICA_UI_TEST_WORKDIR", t.TempDir())
	t.Setenv("MULTICA_UI_TEST_TASK_ID", "task-cancel")
	t.Setenv("MULTICA_UI_TEST_RUNTIME_DIR", t.TempDir())
	restore := replaceUITestServer(func(context.Context, uitest.ServeOptions) error {
		return context.Canceled
	})
	defer restore()

	cmd, stdout, _ := executeUITestCommand(t, "ui-test", "serve")
	if err := cmd.Execute(); !errors.Is(err, context.Canceled) {
		t.Fatalf("serve error = %v, want context cancellation", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no protocol noise", stdout.String())
	}
}

func executeUITestCommand(t *testing.T, args ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{Use: "multica", SilenceUsage: true, SilenceErrors: true}
	cmd.PersistentFlags().String("profile", "", "")
	cmd.AddCommand(newUITestCommand())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	return cmd, stdout, stderr
}

func replaceUITestRuntimeFactory(factory func(string) uiTestRuntime) func() {
	previous := newUITestRuntime
	newUITestRuntime = factory
	return func() {
		newUITestRuntime = previous
	}
}

func replaceUITestServer(server func(context.Context, uitest.ServeOptions) error) func() {
	previous := runUITestServer
	runUITestServer = server
	return func() {
		runUITestServer = previous
	}
}
