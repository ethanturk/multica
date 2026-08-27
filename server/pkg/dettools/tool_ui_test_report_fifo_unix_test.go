//go:build !windows

package dettools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestUIReportRejectsFIFOWithoutBlocking(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-fifo"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(runDir, "console.log"), 0o600); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(t.TempDir(), "result.json")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUIReportFIFOHelperProcess$")
	cmd.Env = append(os.Environ(),
		"MULTICA_UI_REPORT_FIFO_HELPER=1",
		"MULTICA_UI_REPORT_FIFO_WORKDIR="+workDir,
		"MULTICA_UI_REPORT_FIFO_RESULT="+resultPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("FIFO evidence blocked the handler: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("FIFO helper failed: %v: %s", err, stderr.String())
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
		t.Fatalf("result = %+v, want error/INVALID_INPUT", result)
	}
}

func TestUIReportFIFOHelperProcess(t *testing.T) {
	if os.Getenv("MULTICA_UI_REPORT_FIFO_HELPER") != "1" {
		return
	}
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": ".multica/artifacts/ui-test/task-fifo/console.log", "type": "log", "description": "Console",
	}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	result := uiTestReportHandler(context.Background(), raw, ToolEnv{
		WorkDir: os.Getenv("MULTICA_UI_REPORT_FIFO_WORKDIR"), ArtifactDir: DefaultArtifactDir, TaskID: "task-fifo",
	})
	raw, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("MULTICA_UI_REPORT_FIFO_RESULT"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
