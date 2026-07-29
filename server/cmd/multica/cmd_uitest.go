package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/multica-ai/multica/server/pkg/uitest"
	"github.com/spf13/cobra"
)

type uiTestRuntime interface {
	Status() uitest.CapabilityStatus
	Install(context.Context) (uitest.CapabilityStatus, error)
}

var newUITestRuntime = func(root string) uiTestRuntime {
	return uitest.NewManager(root)
}

var runUITestServer = uitest.RunServer

var uiTestCmd = newUITestCommand()

func newUITestCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "ui-test",
		Short: "Manage the local UI testing capability",
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the pinned local UI testing runtime",
		Args:  cobra.NoArgs,
		RunE:  runUITestInstall,
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the local UI testing runtime status",
		Args:  cobra.NoArgs,
		RunE:  runUITestStatus,
	}
	serve := &cobra.Command{
		Use:    "serve",
		Short:  "Run the task-scoped UI testing MCP proxy",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   runUITestServe,
	}
	status.Flags().String("output", "table", "Output format: table or json")
	command.AddCommand(install, status, serve)
	return command
}

func runUITestServe(cmd *cobra.Command, _ []string) error {
	workDir := strings.TrimSpace(os.Getenv("MULTICA_UI_TEST_WORKDIR"))
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve UI test workdir: %w", err)
		}
	}
	taskID := strings.TrimSpace(os.Getenv("MULTICA_UI_TEST_TASK_ID"))
	if taskID == "" {
		return fmt.Errorf("MULTICA_UI_TEST_TASK_ID is required")
	}
	runtimeDir := strings.TrimSpace(os.Getenv("MULTICA_UI_TEST_RUNTIME_DIR"))
	if runtimeDir == "" {
		return fmt.Errorf("MULTICA_UI_TEST_RUNTIME_DIR is required")
	}
	absoluteWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolve UI test workdir: %w", err)
	}
	absoluteRuntimeDir, err := filepath.Abs(runtimeDir)
	if err != nil {
		return fmt.Errorf("resolve UI test runtime directory: %w", err)
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runUITestServer(ctx, uitest.ServeOptions{
		WorkDir:     absoluteWorkDir,
		TaskID:      taskID,
		Runtime:     uitest.ReadyRuntime{Directory: absoluteRuntimeDir},
		Input:       cmd.InOrStdin(),
		Output:      cmd.OutOrStdout(),
		ErrorOutput: cmd.ErrOrStderr(),
	})
}

func runUITestInstall(cmd *cobra.Command, _ []string) error {
	runtime, err := uiTestRuntimeForCommand(cmd)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Installing pinned UI test runtime...")
	status, err := runtime.Install(cmd.Context())
	if err != nil {
		return err
	}
	writeUITestStatusTable(cmd, status)
	return nil
}

func runUITestStatus(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "table" && output != "json" {
		return fmt.Errorf("invalid output %q: must be table or json", output)
	}
	runtime, err := uiTestRuntimeForCommand(cmd)
	if err != nil {
		return err
	}
	status := runtime.Status()
	if output == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
	}
	writeUITestStatusTable(cmd, status)
	return nil
}

func uiTestRuntimeForCommand(cmd *cobra.Command) (uiTestRuntime, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve UI test runtime directory: %w", err)
	}
	return newUITestRuntime(uitest.RootForProfile(home, resolveProfile(cmd))), nil
}

func writeUITestStatusTable(cmd *cobra.Command, status uitest.CapabilityStatus) {
	fmt.Fprintln(cmd.OutOrStdout(), "STATUS\tVERSION\tERROR")
	fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", status.Status, status.Version, status.Error)
}
