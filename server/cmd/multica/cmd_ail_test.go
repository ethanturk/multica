package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/ail"
)

func newAilStage2TestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stage2"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("events-path", "", "")
	cmd.Flags().String("output-dir", "", "")
	cmd.Flags().String("emit-categories", "", "")
	cmd.Flags().Int("window-hours", 0, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func writeTestAilEvents(t *testing.T, path string, events []ail.Stage2Event) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}

func TestRunAilStage2WritesOutputFilesAndJSONStdout(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	eventsPath := filepath.Join(tmp, "events.jsonl")
	outputDir := filepath.Join(tmp, "out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a2", Status: "failed", FailureReason: "agent_error"},
		{TS: now.Add(-1 * time.Minute).Format(time.RFC3339Nano), EventType: "attempt_event", TaskID: "t3", AgentID: "a3", Status: "running"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "stage2_index.jsonl")); err != nil {
		t.Fatalf("stage2_index.jsonl not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "stage2_summary.json")); err != nil {
		t.Fatalf("stage2_summary.json not created: %v", err)
	}

	var result ail.Stage2Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.TotalWindow == 0 {
		t.Fatalf("total_window_events = 0, want > 0")
	}
}

func TestRunAilStage2TableOutputNoPainBuckets(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	eventsPath := filepath.Join(tmp, "events.jsonl")
	outputDir := filepath.Join(tmp, "out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2 table no buckets: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "total_window") {
		t.Fatalf("table output missing summary line, got: %q", out)
	}
	if !strings.Contains(out, "No pain buckets") {
		t.Fatalf("table output should say no pain buckets, got: %q", out)
	}
}

func TestRunAilStage2TableOutputWithPainBucketsTruncatedToThree(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	eventsPath := filepath.Join(tmp, "events.jsonl")
	outputDir := filepath.Join(tmp, "out")

	// 4 distinct failure reasons exercises the top-3 truncation branch
	reasons := []string{"reason_a", "reason_b", "reason_c", "reason_d"}
	events := make([]ail.Stage2Event, 0, len(reasons))
	for i, r := range reasons {
		events = append(events, ail.Stage2Event{
			TS:            now.Add(-time.Duration(i+1) * time.Minute).Format(time.RFC3339Nano),
			EventType:     "failure_event",
			TaskID:        "t" + strconv.Itoa(i),
			AgentID:       "a1",
			Status:        "failed",
			FailureReason: r,
		})
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2 table with buckets: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "RANK") {
		t.Fatalf("table output missing RANK header, got: %q", out)
	}
}

func TestRunAilStage2ErrorFromInvalidConfigFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(configPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	cmd := newAilStage2TestCmd()
	_ = cmd.Flags().Set("config", configPath)

	if err := runAilStage2(cmd, nil); err == nil {
		t.Fatal("expected error from invalid config, got nil")
	}
}

func TestRunAilStage2ErrorMissingEventsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newAilStage2TestCmd()

	if err := runAilStage2(cmd, nil); err == nil {
		t.Fatal("expected error from missing events file, got nil")
	}
}
