package dettools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func uiReportFixture() map[string]any {
	return map[string]any{
		"schema_version":   "1",
		"execution_status": "completed",
		"target": map[string]any{
			"url":    "http://127.0.0.1:3000/issues",
			"commit": "abc123",
		},
		"environment": map[string]any{
			"browser": "chromium",
			"viewport": map[string]any{
				"width":  1440,
				"height": 900,
			},
		},
		"scenarios": []any{
			map[string]any{
				"id":          "scenario-login",
				"name":        "Login",
				"description": "Open authenticated workspace",
				"status":      "passed",
			},
		},
		"objective_checks":  []any{},
		"advisory_findings": []any{},
		"artifacts":         []any{},
	}
}

func runUIReport(t *testing.T, workDir, taskID string, input map[string]any) Result {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return uiTestReportHandler(context.Background(), raw, ToolEnv{
		WorkDir:     workDir,
		ArtifactDir: DefaultArtifactDir,
		TaskID:      taskID,
	})
}

func uiReportRunDir(workDir, taskID string) string {
	return filepath.Join(workDir, DefaultArtifactDir, "ui-test", taskID)
}

func TestUIReportDerivesVerdict(t *testing.T) {
	tests := []struct {
		name      string
		execution string
		objective string
		want      string
	}{
		{name: "completed pass", execution: "completed", objective: "passed", want: "pass"},
		{name: "completed fail", execution: "completed", objective: "failed", want: "fail"},
		{name: "infrastructure error", execution: "infrastructure_error", objective: "failed", want: "not_run"},
		{name: "blocked", execution: "blocked", objective: "passed", want: "not_run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := uiReportFixture()
			input["execution_status"] = tt.execution
			input["objective_checks"] = []any{
				map[string]any{
					"id":          "check-login",
					"scenario_id": "scenario-login",
					"name":        "Workspace opens",
					"status":      tt.objective,
					"source":      "assertion",
					"details":     "Expected workspace shell",
				},
			}
			result := runUIReport(t, t.TempDir(), "task-verdict", input)
			if result.Status != StatusOK {
				t.Fatalf("status = %q, want ok: %+v", result.Status, result)
			}
			if got := result.MachineData["verdict"]; got != tt.want {
				t.Errorf("verdict = %v, want %q", got, tt.want)
			}
			if tt.want == "fail" && result.Summary != "UI test report: completed / fail" {
				t.Errorf("summary = %q", result.Summary)
			}
		})
	}
}

func TestUIReportRejectsInvalidSchemaAndReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "submitted verdict", mutate: func(in map[string]any) { in["verdict"] = "pass" }},
		{name: "unknown top-level field", mutate: func(in map[string]any) { in["extra"] = true }},
		{name: "unknown nested field", mutate: func(in map[string]any) {
			in["target"].(map[string]any)["extra"] = true
		}},
		{name: "schema version", mutate: func(in map[string]any) { in["schema_version"] = "2" }},
		{name: "execution enum", mutate: func(in map[string]any) { in["execution_status"] = "running" }},
		{name: "scenario enum", mutate: func(in map[string]any) {
			in["scenarios"].([]any)[0].(map[string]any)["status"] = "skipped"
		}},
		{name: "objective enum", mutate: func(in map[string]any) {
			in["objective_checks"] = []any{map[string]any{
				"id": "check", "scenario_id": "scenario-login", "name": "check", "status": "broken", "source": "assertion",
			}}
		}},
		{name: "objective source enum", mutate: func(in map[string]any) {
			in["objective_checks"] = []any{map[string]any{
				"id": "check", "scenario_id": "scenario-login", "name": "check", "status": "passed", "source": "visual",
			}}
		}},
		{name: "advisory category enum", mutate: func(in map[string]any) {
			in["advisory_findings"] = []any{map[string]any{
				"id": "finding", "scenario_id": "scenario-login", "title": "Finding",
				"category": "performance", "severity": "low", "observation": "Slow", "impact": "Wait", "suggestion": "Measure",
			}}
		}},
		{name: "advisory severity enum", mutate: func(in map[string]any) {
			in["advisory_findings"] = []any{map[string]any{
				"id": "finding", "scenario_id": "scenario-login", "title": "Finding",
				"category": "feedback", "severity": "critical", "observation": "Slow", "impact": "Wait", "suggestion": "Measure",
			}}
		}},
		{name: "duplicate scenario id", mutate: func(in map[string]any) {
			in["scenarios"] = append(in["scenarios"].([]any), map[string]any{
				"id": "scenario-login", "name": "Duplicate", "status": "failed",
			})
		}},
		{name: "unknown objective scenario", mutate: func(in map[string]any) {
			in["objective_checks"] = []any{map[string]any{
				"id": "check", "scenario_id": "missing", "name": "check", "status": "passed", "source": "assertion",
			}}
		}},
		{name: "unknown advisory scenario", mutate: func(in map[string]any) {
			in["advisory_findings"] = []any{map[string]any{
				"id": "finding", "scenario_id": "missing", "title": "Finding",
				"category": "feedback", "severity": "low", "observation": "Slow", "impact": "Wait", "suggestion": "Measure",
			}}
		}},
		{name: "completed no scenarios", mutate: func(in map[string]any) { in["scenarios"] = []any{} }},
		{name: "external target", mutate: func(in map[string]any) {
			in["target"].(map[string]any)["url"] = "https://example.com"
		}},
		{name: "non-app loopback target", mutate: func(in map[string]any) {
			in["target"].(map[string]any)["url"] = "http://127.0.0.2:3000"
		}},
		{name: "browser", mutate: func(in map[string]any) {
			in["environment"].(map[string]any)["browser"] = "firefox"
		}},
		{name: "viewport width", mutate: func(in map[string]any) {
			in["environment"].(map[string]any)["viewport"].(map[string]any)["width"] = 319
		}},
		{name: "viewport height", mutate: func(in map[string]any) {
			in["environment"].(map[string]any)["viewport"].(map[string]any)["height"] = 2161
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := uiReportFixture()
			tt.mutate(input)
			result := runUIReport(t, t.TempDir(), "task-invalid", input)
			if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
				t.Fatalf("result = %+v, want error/INVALID_INPUT", result)
			}
		})
	}
}

func TestUIReportValidatesEvidenceAtConfinedOpenBoundary(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		workDir := t.TempDir()
		input := uiReportFixture()
		input["artifacts"] = []any{map[string]any{
			"path": ".multica/artifacts/ui-test/task-path/missing.png", "type": "screenshot", "description": "Missing",
		}}
		result := runUIReport(t, workDir, "task-path", input)
		if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("outside run", func(t *testing.T) {
		workDir := t.TempDir()
		outside := filepath.Join(workDir, "outside.png")
		if err := os.WriteFile(outside, []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
		input := uiReportFixture()
		input["artifacts"] = []any{map[string]any{
			"path": "outside.png", "type": "screenshot", "description": "Outside",
		}}
		result := runUIReport(t, workDir, "task-path", input)
		if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		workDir := t.TempDir()
		runDir := uiReportRunDir(workDir, "task-link")
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(workDir, "outside.png")
		if err := os.WriteFile(outside, []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(runDir, "linked.png")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		input := uiReportFixture()
		input["artifacts"] = []any{map[string]any{
			"path": ".multica/artifacts/ui-test/task-link/linked.png", "type": "screenshot", "description": "Linked",
		}}
		result := runUIReport(t, workDir, "task-link", input)
		if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("secret filename rejected before read", func(t *testing.T) {
		workDir := t.TempDir()
		runDir := uiReportRunDir(workDir, "task-secret")
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(runDir, "storage-state.json")
		if err := os.WriteFile(path, []byte(`{"token":"must-not-read"}`), 0); err != nil {
			t.Fatal(err)
		}
		input := uiReportFixture()
		input["artifacts"] = []any{map[string]any{
			"path": ".multica/artifacts/ui-test/task-secret/storage-state.json", "type": "json", "description": "Browser state",
		}}
		result := runUIReport(t, workDir, "task-secret", input)
		if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
			t.Fatalf("result = %+v", result)
		}
	})
}

func TestUIReportCountsGeneratedBytesAgainstLimit(t *testing.T) {
	workDir := t.TempDir()
	runDir := uiReportRunDir(workDir, "task-size")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(runDir, "trace.zip")
	if err := os.WriteFile(evidence, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(evidence, uiTestMaxPublishedBytes-1); err != nil {
		t.Fatal(err)
	}
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": ".multica/artifacts/ui-test/task-size/trace.zip", "type": "trace", "description": "Trace",
	}}
	result := runUIReport(t, workDir, "task-size", input)
	if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
		t.Fatalf("result = %+v, want generated bytes to push total above limit", result)
	}
}

func TestUIReportRendersFourDeterministicRedactedOutputs(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-render"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"b-console.log": "console evidence",
		"a-screen.png":  "image evidence",
	} {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	input := uiReportFixture()
	input["scenarios"] = []any{
		map[string]any{"id": "scenario-z", "name": "Settings", "status": "failed"},
		map[string]any{"id": "scenario-a", "name": "Issues", "status": "passed"},
	}
	input["objective_checks"] = []any{
		map[string]any{
			"id": "check-z", "scenario_id": "scenario-z", "name": "Save succeeds", "status": "failed",
			"source": "network", "details": "Authorization: Bearer objective-secret",
			"evidence":     []any{".multica/artifacts/ui-test/task-render/b-console.log"},
			"machine_data": map[string]any{"token": "objective-machine-secret"},
		},
		map[string]any{
			"id": "check-a", "scenario_id": "scenario-a", "name": "Issues load", "status": "passed",
			"source": "assertion", "details": "Loaded",
		},
	}
	input["advisory_findings"] = []any{
		map[string]any{
			"id": "finding-z", "scenario_id": "scenario-z", "title": "[Local](file:///tmp/secret)",
			"category": "feedback", "severity": "medium", "observation": "Cookie: advisory-secret",
			"impact": "User waits", "suggestion": "Show progress",
			"evidence": []any{".multica/artifacts/ui-test/task-render/a-screen.png"},
		},
		map[string]any{
			"id": "finding-a", "scenario_id": "scenario-a", "title": "Dense hierarchy",
			"category": "hierarchy", "severity": "low", "observation": "Dense", "impact": "Scanning", "suggestion": "Group",
		},
	}
	input["artifacts"] = []any{
		map[string]any{
			"path": ".multica/artifacts/ui-test/task-render/b-console.log", "type": "console",
			"description":  "Authorization: Bearer artifact-secret",
			"machine_data": map[string]any{"refresh_token": "artifact-machine-secret"},
		},
		map[string]any{
			"path": ".multica/artifacts/ui-test/task-render/a-screen.png", "type": "screenshot", "description": "Settings",
		},
	}

	first := runUIReport(t, workDir, taskID, input)
	if first.Status != StatusOK || first.MachineData["verdict"] != "fail" {
		t.Fatalf("first result = %+v", first)
	}
	if len(first.Artifacts) != 4 {
		t.Fatalf("generated artifacts = %v, want four", first.Artifacts)
	}

	names := []string{"report.json", "report.md", "artifact-manifest.json", "comment.md"}
	firstBytes := map[string][]byte{}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			t.Fatal(err)
		}
		firstBytes[name] = raw
	}

	for _, secret := range []string{
		"objective-secret", "objective-machine-secret", "advisory-secret",
		"artifact-secret", "artifact-machine-secret",
	} {
		for name, raw := range firstBytes {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s contains secret %q", name, secret)
			}
		}
	}
	comment := string(firstBytes["comment.md"])
	for _, want := range []string{
		"Verdict: fail", "Execution status: completed", "Target: http://127.0.0.1:3000/issues",
		"Commit: abc123", "Save succeeds", "Use the attached report and evidence",
	} {
		if !strings.Contains(comment, want) {
			t.Errorf("comment missing %q:\n%s", want, comment)
		}
	}
	if strings.Contains(comment, "](") || strings.Contains(comment, ".multica/artifacts") {
		t.Errorf("comment contains local Markdown link/path:\n%s", comment)
	}

	var manifest struct {
		Artifacts []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(firstBytes["artifact-manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	var manifestPaths []string
	for _, artifact := range manifest.Artifacts {
		manifestPaths = append(manifestPaths, artifact.Path)
		if artifact.Size < 0 {
			t.Errorf("negative size for %s", artifact.Path)
		}
	}
	if slices.Contains(manifestPaths, filepath.Join(DefaultArtifactDir, "ui-test", taskID, "artifact-manifest.json")) {
		t.Errorf("manifest lists itself: %v", manifestPaths)
	}
	for _, name := range []string{"report.json", "report.md", "comment.md", "a-screen.png", "b-console.log"} {
		want := filepath.Join(DefaultArtifactDir, "ui-test", taskID, name)
		if !slices.Contains(manifestPaths, want) {
			t.Errorf("manifest missing %q: %v", want, manifestPaths)
		}
	}

	slices.Reverse(input["scenarios"].([]any))
	slices.Reverse(input["objective_checks"].([]any))
	slices.Reverse(input["advisory_findings"].([]any))
	slices.Reverse(input["artifacts"].([]any))
	second := runUIReport(t, workDir, taskID, input)
	if second.Status != StatusOK {
		t.Fatalf("second result = %+v", second)
	}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(raw, firstBytes[name]) {
			t.Errorf("%s changed when input order changed", name)
		}
	}
}

func TestUIReportFailedPublicationPreservesPriorOutputs(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-atomic"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const prior = "prior-good-report"
	if err := os.WriteFile(filepath.Join(runDir, "report.json"), []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(runDir, "report.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := runUIReport(t, workDir, taskID, uiReportFixture())
	if result.Status != StatusError || result.ErrorCode != CodeInternal {
		t.Fatalf("result = %+v, want error/INTERNAL_ERROR", result)
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != prior {
		t.Errorf("prior report was clobbered: %q", raw)
	}
	for _, name := range []string{"comment.md", "artifact-manifest.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
			t.Errorf("partial output %s exists after failed publication", name)
		}
	}
}

func TestUIReportTaskIDFromEnvAndManualFallback(t *testing.T) {
	t.Setenv("MULTICA_TASK_ID", "task-from-env")
	if got := OptionsFromEnv().TaskID; got != "task-from-env" {
		t.Errorf("TaskID = %q, want task-from-env", got)
	}
	t.Setenv("MULTICA_TASK_ID", " ")
	if got := OptionsFromEnv().TaskID; got != "manual" {
		t.Errorf("TaskID = %q, want manual", got)
	}
}
