package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
	status.Flags().String("output", "table", "Output format: table or json")
	command.AddCommand(install, status)
	return command
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
