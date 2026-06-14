package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/ail"
	"github.com/multica-ai/multica/server/internal/cli"
)

const ailTuningIssueEnv = "MULTICA_AIL_TUNING_ISSUE_ID"

var ailCmd = &cobra.Command{
	Use:   "ail",
	Short: "Agent improvement loop operations",
}

var ailStage2Cmd = &cobra.Command{
	Use:   "stage2",
	Short: "Run Stage 2 capture against a Stage 1 events file and write the index + summary",
	RunE:  runAilStage2,
}

var ailStage3Cmd = &cobra.Command{
	Use:   "stage3",
	Short: "Run Stage 3 analysis against a Stage 2 index and write digest + signatures",
	RunE:  runAilStage3,
}

var ailRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Stage 2 capture then Stage 3 analysis in one workflow (Option A)",
	RunE:  runAilRun,
}

func init() {
	ailCmd.AddCommand(ailStage2Cmd)
	ailCmd.AddCommand(ailStage3Cmd)
	ailCmd.AddCommand(ailRunCmd)

	ailStage2Cmd.Flags().String("config", "", "Path to optional Stage 2 config JSON file (contains stage1.events_path, stage1.emit_categories)")
	ailStage2Cmd.Flags().String("events-path", "", "Path to Stage 1 events JSONL file (overrides config and default)")
	ailStage2Cmd.Flags().String("output-dir", "", "Directory for stage2_index.jsonl and stage2_summary.json output (overrides default)")
	ailStage2Cmd.Flags().String("emit-categories", "", "Comma-separated event types to include (default: agent_event,attempt_event,failure_event)")
	ailStage2Cmd.Flags().Int("window-hours", 0, "Capture window in hours (default: 24)")
	ailStage2Cmd.Flags().String("output", "json", "Output format: json or table")

	ailStage3Cmd.Flags().String("index-path", "", "Path to Stage 2 index JSONL file (default: <stage2 output-dir>/stage2_index.jsonl)")
	ailStage3Cmd.Flags().String("output-dir", "", "Directory for Stage 3 output files (default: ~/diagnostics/stage3)")
	ailStage3Cmd.Flags().Int("window-hours", 0, "Analysis window in hours (default: 24)")
	ailStage3Cmd.Flags().Int("min-signature-count", 0, "Minimum event count for a repeat signature to be a candidate (default: 3)")
	ailStage3Cmd.Flags().Int("min-unique-tasks", 0, "Minimum unique task count for a repeat signature to be a candidate (default: 2)")
	ailStage3Cmd.Flags().String("output", "json", "Output format: json or table")

	ailRunCmd.Flags().String("config", "", "Path to optional Stage 2 config JSON file")
	ailRunCmd.Flags().String("events-path", "", "Path to Stage 1 events JSONL file")
	ailRunCmd.Flags().String("stage2-output-dir", "", "Directory for Stage 2 output files (default: ~/diagnostics/stage2)")
	ailRunCmd.Flags().String("stage3-output-dir", "", "Directory for Stage 3 output files (default: ~/diagnostics/stage3)")
	ailRunCmd.Flags().String("emit-categories", "", "Comma-separated event types to include (default: agent_event,attempt_event,failure_event)")
	ailRunCmd.Flags().Int("window-hours", 0, "Capture/analysis window in hours (default: 24)")
	ailRunCmd.Flags().Int("min-signature-count", 0, "Minimum event count for a repeat signature to be a candidate (default: 3)")
	ailRunCmd.Flags().Int("min-unique-tasks", 0, "Minimum unique task count for a repeat signature to be a candidate (default: 2)")
	ailRunCmd.Flags().String("stage5-output-dir", "", "Directory for Stage 5 digest and watermark output (default: ~/diagnostics/stage5)")
	ailRunCmd.Flags().String("digest-issue", "", "Issue ID to receive the Stage 5 human-readable digest (fallback: MULTICA_AIL_TUNING_ISSUE_ID)")
	ailRunCmd.Flags().String("output", "json", "Output format: json or table")
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

func runAilStage3(cmd *cobra.Command, _ []string) error {
	indexPath, _ := cmd.Flags().GetString("index-path")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	windowHours, _ := cmd.Flags().GetInt("window-hours")
	minSigCount, _ := cmd.Flags().GetInt("min-signature-count")
	minUniqueTasks, _ := cmd.Flags().GetInt("min-unique-tasks")

	cfg := ail.NewStage3ConfigFromArgs(indexPath, outputDir, windowHours, minSigCount, minUniqueTasks)

	result, err := ail.RunStage3Analyze(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilStage3Table(cmd, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runAilRun(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	eventsPath, _ := cmd.Flags().GetString("events-path")
	stage2OutputDir, _ := cmd.Flags().GetString("stage2-output-dir")
	emitCats, _ := cmd.Flags().GetString("emit-categories")
	windowHours, _ := cmd.Flags().GetInt("window-hours")

	s2cfg, err := ail.NewStage2ConfigFromArgs(configPath, eventsPath, stage2OutputDir, emitCats, windowHours)
	if err != nil {
		return err
	}

	s2result, err := ail.RunStage2Capture(s2cfg)
	if err != nil {
		return fmt.Errorf("stage2: %w", err)
	}

	stage3OutputDir, _ := cmd.Flags().GetString("stage3-output-dir")
	minSigCount, _ := cmd.Flags().GetInt("min-signature-count")
	minUniqueTasks, _ := cmd.Flags().GetInt("min-unique-tasks")

	s3cfg := ail.NewStage3ConfigFromArgs(s2cfg.IndexFilePath(), stage3OutputDir, windowHours, minSigCount, minUniqueTasks)

	s3result, err := ail.RunStage3Analyze(s3cfg)
	if err != nil {
		return fmt.Errorf("stage3: %w", err)
	}

	stage5OutputDir, _ := cmd.Flags().GetString("stage5-output-dir")
	s5digest, err := ail.RunStage5Digest(ail.Stage5Config{OutputDir: stage5OutputDir}, s3result)
	if err != nil {
		return fmt.Errorf("stage5: %w", err)
	}

	digestIssue, _ := cmd.Flags().GetString("digest-issue")
	if strings.TrimSpace(digestIssue) == "" {
		digestIssue = os.Getenv(ailTuningIssueEnv)
	}
	digestIssue = strings.TrimSpace(digestIssue)
	digestPosted := false
	if digestIssue != "" {
		posted, postErr := postAilStage5Digest(cmd, digestIssue, s5digest)
		if postErr != nil {
			return fmt.Errorf("stage5 digest post: %w", postErr)
		}
		digestPosted = posted
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilRunTable(cmd, s2result, s3result, s5digest, digestPosted)
		return nil
	}
	combined := struct {
		Stage2            ail.Stage2Result `json:"stage2"`
		Stage3            ail.Stage3Result `json:"stage3"`
		Stage5            ail.Stage5Digest `json:"stage5"`
		Stage5DigestPost  bool             `json:"stage5_digest_posted"`
		Stage5DigestIssue string           `json:"stage5_digest_issue,omitempty"`
	}{Stage2: s2result, Stage3: s3result, Stage5: s5digest, Stage5DigestPost: digestPosted, Stage5DigestIssue: digestIssue}
	return cli.PrintJSON(cmd.OutOrStdout(), combined)
}

func postAilStage5Digest(cmd *cobra.Command, issueID string, digest ail.Stage5Digest) (bool, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cli.APITimeout())
	defer cancel()

	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueID+"/comments", &comments); err != nil {
		return false, fmt.Errorf("list comments: %w", err)
	}
	for _, comment := range comments {
		if content, ok := comment["content"].(string); ok && strings.Contains(content, digest.Marker) {
			return false, nil
		}
	}

	body := map[string]any{"content": ail.RenderStage5Comment(digest)}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueID+"/comments", body, &result); err != nil {
		return false, fmt.Errorf("add comment: %w", err)
	}
	return true, nil
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

func printAilStage3Table(cmd *cobra.Command, result ail.Stage3Result) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "analyzed_at: %s  window: %s  total_window: %d  signatures: %d  candidates: %d\n",
		result.AnalyzedAt, result.WindowDuration, result.TotalEvents, len(result.RepeatSignatures), len(result.CandidateDettools))
	top := result.TopPainBuckets
	if len(top) > 3 {
		top = top[:3]
	}
	if len(top) == 0 {
		fmt.Fprintf(w, "No pain buckets in window.\n")
		return
	}
	headers := []string{"RANK", "KEY", "COUNT", "TASKS", "AGENTS"}
	rows := make([][]string, 0, len(top))
	for i, b := range top {
		rows = append(rows, []string{strconv.Itoa(i + 1), b.Key, strconv.Itoa(b.Count), strconv.Itoa(b.UniqueTasks), strconv.Itoa(b.UniqueAgents)})
	}
	cli.PrintTable(w, headers, rows)
}

func printAilRunTable(cmd *cobra.Command, s2 ail.Stage2Result, s3 ail.Stage3Result, s5 ail.Stage5Digest, stage5Posted bool) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "stage2: window=%s total_window=%d unique_tasks=%d\n",
		s2.WindowDuration, s2.TotalWindow, s2.UniqueTasks)
	fmt.Fprintf(w, "stage3: analyzed_at=%s total_events=%d candidates=%d\n",
		s3.AnalyzedAt, s3.TotalEvents, len(s3.CandidateDettools))
	fmt.Fprintf(w, "stage5: marker=%s digest_posted=%t alerts=%d\n",
		s5.Marker, stage5Posted, len(s5.Alerts))
}
