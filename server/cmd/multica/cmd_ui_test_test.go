package main

import (
	"bytes"
	"context"
	"errors"
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
	} {
		cmd, _, _ := executeUITestCommand(t, args...)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("%v succeeded, want argument error", args)
		}
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
