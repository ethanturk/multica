package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/ail"
	"github.com/multica-ai/multica/server/internal/cli"
)

var ailCmd = &cobra.Command{
	Use:   "ail",
	Short: "Agent improvement loop operations",
}

var ailStage2Cmd = &cobra.Command{
	Use:   "stage2",
	Short: "Run Stage 2 capture against a Stage 1 events file and write the index + summary",
	RunE:  runAilStage2,
}

func init() {
	ailCmd.AddCommand(ailStage2Cmd)

	ailStage2Cmd.Flags().String("config", "", "Path to optional Stage 2 config JSON file (contains stage1.events_path, stage1.emit_categories)")
	ailStage2Cmd.Flags().String("events-path", "", "Path to Stage 1 events JSONL file (overrides config and default)")
	ailStage2Cmd.Flags().String("output-dir", "", "Directory for stage2_index.jsonl and stage2_summary.json output (overrides default)")
	ailStage2Cmd.Flags().String("emit-categories", "", "Comma-separated event types to include (default: agent_event,attempt_event,failure_event)")
	ailStage2Cmd.Flags().Int("window-hours", 0, "Capture window in hours (default: 24)")
	ailStage2Cmd.Flags().String("output", "json", "Output format: json or table")
}

func runAilStage2(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	eventsPath, _ := cmd.Flags().GetString("events-path")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	emitCats, _ := cmd.Flags().GetString("emit-categories")
	windowHours, _ := cmd.Flags().GetInt("window-hours")

	cfg, err := ail.NewStage2ConfigFromArgs(configPath, eventsPath, outputDir, emitCats, windowHours)
	if err != nil {
		return err
	}

	result, err := ail.RunStage2Capture(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilStage2Table(cmd, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func printAilStage2Table(cmd *cobra.Command, result ail.Stage2Result) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "generated_at: %s  window: %s  total_window: %d  unique_tasks: %d  unique_agents: %d\n",
		result.GeneratedAt, result.WindowDuration, result.TotalWindow, result.UniqueTasks, result.UniqueAgents)
	top := result.TopPainBuckets
	if len(top) > 3 {
		top = top[:3]
	}
	if len(top) == 0 {
		fmt.Fprintf(w, "No pain buckets in window.\n")
		return
	}
	headers := []string{"RANK", "KEY", "COUNT", "TASKS"}
	rows := make([][]string, 0, len(top))
	for i, b := range top {
		rows = append(rows, []string{strconv.Itoa(i + 1), b.Key, strconv.Itoa(b.Count), strconv.Itoa(b.TaskCount)})
	}
	cli.PrintTable(w, headers, rows)
}
