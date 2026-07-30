package dettools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestUIReportRejectsEvidenceGrowthDuringCaptureAndPreservesPrior(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-growth"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	priorEvidence := filepath.Join(runDir, "prior.png")
	if err := os.WriteFile(priorEvidence, []byte("prior-sealed-evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	priorInput := uiReportFixture()
	priorInput["artifacts"] = []any{map[string]any{
		"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "prior.png")),
		"type": "screenshot", "description": "Prior evidence",
	}}
	if result := runUIReport(t, workDir, taskID, priorInput); result.Status != StatusOK {
		t.Fatalf("prior publication = %+v", result)
	}
	priorReport, err := os.ReadFile(filepath.Join(runDir, uiTestReportJSONName))
	if err != nil {
		t.Fatal(err)
	}
	priorSealed, err := os.ReadFile(filepath.Join(runDir, uiTestPublishedDir, "prior.png"))
	if err != nil {
		t.Fatal(err)
	}

	evidence := filepath.Join(runDir, "growing-trace.zip")
	if err := os.WriteFile(evidence, []byte("initial-trace"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "growing-trace.zip")),
		"type": "trace", "description": "Growing trace",
	}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), uiTestEvidenceCaptureHookContextKey{}, uiTestEvidenceCaptureHook(func() error {
		file, err := os.OpenFile(evidence, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString("-grew-during-capture")
		syncErr := file.Sync()
		closeErr := file.Close()
		return errors.Join(writeErr, syncErr, closeErr)
	}))
	result := uiTestReportHandler(ctx, raw, ToolEnv{
		WorkDir: workDir, ArtifactDir: DefaultArtifactDir, TaskID: taskID,
	})
	if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
		t.Fatalf("result = %+v, want growing evidence rejection", result)
	}
	reportAfter, err := os.ReadFile(filepath.Join(runDir, uiTestReportJSONName))
	if err != nil {
		t.Fatal(err)
	}
	sealedAfter, err := os.ReadFile(filepath.Join(runDir, uiTestPublishedDir, "prior.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reportAfter, priorReport) || !reflect.DeepEqual(sealedAfter, priorSealed) {
		t.Fatal("unstable capture replaced prior good publication")
	}
	if _, err := os.Stat(filepath.Join(runDir, uiTestPublishedDir, "growing-trace.zip")); !os.IsNotExist(err) {
		t.Fatalf("unstable evidence was published: %v", err)
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
	for _, name := range []string{"report.json", "report.md", "comment.md"} {
		want := filepath.Join(DefaultArtifactDir, "ui-test", taskID, name)
		if !slices.Contains(manifestPaths, want) {
			t.Errorf("manifest missing %q: %v", want, manifestPaths)
		}
	}
	for _, name := range []string{"a-screen.png", "b-console.log"} {
		want := filepath.Join(DefaultArtifactDir, "ui-test", taskID, uiTestPublishedDir, name)
		if !slices.Contains(manifestPaths, want) {
			t.Errorf("manifest missing sealed evidence %q: %v", want, manifestPaths)
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

func TestUIReportConcurrentPublishersLeaveOneCoherentSnapshot(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-concurrent"
	runDir := uiReportRunDir(workDir, taskID)
	sourceDir := filepath.Join(runDir, "sources")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const publishers = 8
	inputs := make([]json.RawMessage, publishers)
	wantByPath := make(map[string]struct {
		scenario string
		content  string
	}, publishers)
	for i := range publishers {
		name := fmt.Sprintf("model-%02d", i)
		content := "sealed-content-" + name
		sourcePath := filepath.Join(sourceDir, name+".bin")
		if err := os.WriteFile(sourcePath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		artifactPath := filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "sources", name+".bin"))
		publishedPath := filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, uiTestPublishedDir, "sources", name+".bin"))
		input := uiReportFixture()
		input["scenarios"] = []any{map[string]any{
			"id": "scenario-" + name, "name": name, "status": "passed",
		}}
		input["artifacts"] = []any{map[string]any{
			"path": artifactPath, "type": "screenshot", "description": name,
		}}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		inputs[i] = raw
		wantByPath[publishedPath] = struct {
			scenario string
			content  string
		}{scenario: name, content: content}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan Result, publishers)
	var group sync.WaitGroup
	for _, raw := range inputs {
		group.Add(1)
		go func(raw json.RawMessage) {
			defer group.Done()
			<-start
			results <- uiTestReportHandler(ctx, raw, ToolEnv{
				WorkDir:     workDir,
				ArtifactDir: DefaultArtifactDir,
				TaskID:      taskID,
				Timeout:     5 * time.Second,
			})
		}(raw)
	}
	close(start)
	group.Wait()
	close(results)
	for result := range results {
		if result.Status != StatusOK {
			t.Fatalf("concurrent result = %+v", result)
		}
	}

	reportRaw, err := os.ReadFile(filepath.Join(runDir, uiTestReportJSONName))
	if err != nil {
		t.Fatal(err)
	}
	var report uiTestReport
	if err := json.Unmarshal(reportRaw, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Scenarios) != 1 || len(report.Artifacts) != 1 {
		t.Fatalf("final report is mixed: scenarios=%+v artifacts=%+v", report.Scenarios, report.Artifacts)
	}
	want, ok := wantByPath[report.Artifacts[0].Path]
	if !ok || report.Scenarios[0].Name != want.scenario {
		t.Fatalf("final report is incoherent: scenario=%q artifact=%q", report.Scenarios[0].Name, report.Artifacts[0].Path)
	}
	evidenceRaw, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(report.Artifacts[0].Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(evidenceRaw) != want.content {
		t.Fatalf("sealed evidence = %q, want %q for %s", evidenceRaw, want.content, report.Artifacts[0].Path)
	}

	manifestRaw, err := os.ReadFile(filepath.Join(runDir, uiTestManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest uiArtifactManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifestHasEvidence := false
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == report.Artifacts[0].Path {
			manifestHasEvidence = true
			break
		}
	}
	if !manifestHasEvidence {
		t.Fatalf("manifest/report evidence mismatch: manifest=%+v report=%+v", manifest.Artifacts, report.Artifacts)
	}
	if err := filepath.WalkDir(runDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		for _, fragment := range []string{".tmp-", ".bak-", ".claim-", ".release-", ".stale-"} {
			if strings.Contains(name, fragment) {
				t.Errorf("publication residue remains: %s", path)
			}
		}
		if name == uiTestReportLockDir {
			t.Errorf("task lock remains: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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

func TestUIReportPublishesOnlySealedRedactedEvidence(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-sealed"
	runDir := uiReportRunDir(workDir, taskID)
	rawDir := filepath.Join(runDir, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		bearerSecret = "sealed-bearer-secret"
		jwtSecret    = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzZWFsZWQifQ.signature123"
		cookieSecret = "sealed-cookie-secret"
		tokenSecret  = "sealed-token-secret"
	)
	consoleSource := filepath.Join(rawDir, "console.log")
	consoleRaw := "Authorization: Bearer " + bearerSecret + "\nCookie: session=" + cookieSecret + "\nJWT " + jwtSecret + "\n"
	if err := os.WriteFile(consoleSource, []byte(consoleRaw), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonSource := filepath.Join(rawDir, "network.json")
	jsonRaw := `{"Set-Cookie":"session=` + cookieSecret + `","nested":{"token":"` + tokenSecret + `"}}`
	if err := os.WriteFile(jsonSource, []byte(jsonRaw), 0o600); err != nil {
		t.Fatal(err)
	}

	input := uiReportFixture()
	input["artifacts"] = []any{
		map[string]any{
			"path": ".multica/artifacts/ui-test/task-sealed/raw/console.log", "type": "console",
			"description": "Authorization: Bearer " + bearerSecret,
		},
		map[string]any{
			"path": ".multica/artifacts/ui-test/task-sealed/raw/network.json", "type": "json",
			"description": "Focused network excerpt",
		},
	}
	result := runUIReport(t, workDir, taskID, input)
	if result.Status != StatusOK {
		t.Fatalf("result = %+v", result)
	}

	wantPaths := []string{
		".multica/artifacts/ui-test/task-sealed/report.json",
		".multica/artifacts/ui-test/task-sealed/report.md",
		".multica/artifacts/ui-test/task-sealed/artifact-manifest.json",
		".multica/artifacts/ui-test/task-sealed/comment.md",
	}
	var gotPaths []string
	for _, artifact := range result.Artifacts {
		gotPaths = append(gotPaths, artifact.Path)
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("published paths = %v, want %v", gotPaths, wantPaths)
	}

	allPublishedPaths := append([]string{
		".multica/artifacts/ui-test/task-sealed/published-evidence/raw/console.log",
		".multica/artifacts/ui-test/task-sealed/published-evidence/raw/network.json",
	}, wantPaths...)
	for _, path := range allPublishedPaths {
		raw, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{bearerSecret, jwtSecret, cookieSecret, tokenSecret} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s contains secret %q", path, secret)
			}
		}
	}
	sourceAfter, err := os.ReadFile(consoleSource)
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceAfter) != consoleRaw {
		t.Errorf("source evidence was modified: %q", sourceAfter)
	}
}

func TestUIReportSealedBinarySnapshotSurvivesSourceReplacement(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-snapshot"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(runDir, "screen.png")
	original := []byte("original-binary-snapshot")
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": ".multica/artifacts/ui-test/task-snapshot/screen.png", "type": "screenshot", "description": "Screen",
	}}
	result := runUIReport(t, workDir, taskID, input)
	if result.Status != StatusOK {
		t.Fatalf("result = %+v", result)
	}
	if err := os.WriteFile(source, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(filepath.Join(runDir, "published-evidence", "screen.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sealed, original) {
		t.Errorf("sealed evidence = %q, want %q", sealed, original)
	}
}

func TestUIReportRejectsHardlinkedSecretEvidenceWithoutReadingIt(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-hardlink"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	storageState := filepath.Join(runDir, "storage-state.json")
	if err := os.WriteFile(storageState, []byte(`{"cookies":[{"value":"must-never-publish"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(runDir, "safe-name.json")
	if err := os.Link(storageState, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": ".multica/artifacts/ui-test/task-hardlink/safe-name.json", "type": "json", "description": "Snapshot",
	}}
	result := runUIReport(t, workDir, taskID, input)
	if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
		t.Fatalf("result = %+v, want error/INVALID_INPUT", result)
	}
}

func TestUIReportRejectsAmbiguousEvidenceMetadata(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		artifactType string
		description  string
	}{
		{name: "unknown type", path: "evidence.bin", artifactType: "blob", description: "Unknown"},
		{name: "backslash path", path: `raw\evidence.log`, artifactType: "log", description: "Log"},
		{name: "control path", path: "raw/evidence\n.log", artifactType: "log", description: "Log"},
		{name: "control description", path: "evidence.log", artifactType: "log", description: "Log\n## Injected"},
		{name: "case folded generated path", path: "REPORT.JSON", artifactType: "json", description: "Report alias"},
		{name: "case folded publication path", path: "Published-Evidence/old.png", artifactType: "screenshot", description: "Old evidence"},
		{name: "case folded lock temporary path", path: ".UI-TEST-REPORT.CLAIM-evil/evidence.log", artifactType: "log", description: "Claim alias"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			taskID := "task-metadata"
			runDir := uiReportRunDir(workDir, taskID)
			path := filepath.Join(runDir, filepath.FromSlash(tt.path))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			input := uiReportFixture()
			input["artifacts"] = []any{map[string]any{
				"path": filepath.ToSlash(filepath.Join(".multica/artifacts/ui-test/task-metadata", tt.path)),
				"type": tt.artifactType, "description": tt.description,
			}}
			result := runUIReport(t, workDir, taskID, input)
			if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
				t.Fatalf("result = %+v, want error/INVALID_INPUT", result)
			}
		})
	}
}

func TestUIReportEvidenceCollisionKeysAreCaseInsensitive(t *testing.T) {
	first := uiTestEvidenceCollisionKey(".multica/artifacts/ui-test/task/raw/SCREEN.PNG")
	second := uiTestEvidenceCollisionKey(".multica/artifacts/ui-test/task/raw/screen.png")
	if first != second {
		t.Fatalf("collision keys differ: %q != %q", first, second)
	}
}

func TestUIReportLockSerializesConcurrentOwnersAndCleansUp(t *testing.T) {
	runDir := t.TempDir()
	root, err := os.OpenRoot(runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	var active atomic.Int32
	var overlap atomic.Bool
	const workers = 8
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			lock, err := acquireUITestReportLock(ctx, root, time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if active.Add(1) != 1 {
				overlap.Store(true)
			}
			time.Sleep(5 * time.Millisecond)
			active.Add(-1)
			errs <- lock.Release()
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("lock operation: %v", err)
		}
	}
	if overlap.Load() {
		t.Error("filesystem lock admitted concurrent owners")
	}
	if _, err := os.Stat(filepath.Join(runDir, uiTestReportLockDir)); !os.IsNotExist(err) {
		t.Errorf("lock residue remains: %v", err)
	}
}

func TestUIReportLockWaitHonorsContextCancellation(t *testing.T) {
	runDir := t.TempDir()
	root, err := os.OpenRoot(runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	first, err := acquireUITestReportLock(context.Background(), root, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := acquireUITestReportLock(ctx, root, time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquisition error = %v, want context deadline", err)
	}
}

func TestUIReportLockReclaimsOnlyOldDeadOwner(t *testing.T) {
	deadPID := exitedProcessPID(t)
	tests := []struct {
		name     string
		owner    uiTestReportLockOwner
		wantLock bool
	}{
		{
			name: "old dead owner",
			owner: uiTestReportLockOwner{
				PID: deadPID, Token: "dead-owner", StartedAt: time.Now().Add(-10 * time.Minute),
			},
			wantLock: true,
		},
		{
			name: "recent dead owner",
			owner: uiTestReportLockOwner{
				PID: deadPID, Token: "recent-dead", StartedAt: time.Now(),
			},
		},
		{
			name: "old live or pid reused owner",
			owner: uiTestReportLockOwner{
				PID: os.Getpid(), Token: "live-owner", StartedAt: time.Now().Add(-10 * time.Minute),
			},
		},
		{
			name: "old indeterminate owner",
			owner: uiTestReportLockOwner{
				PID: 0, Token: "indeterminate-owner", StartedAt: time.Now().Add(-10 * time.Minute),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDir := t.TempDir()
			writeUITestReportLockOwner(t, runDir, tt.owner)
			root, err := os.OpenRoot(runDir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			lock, err := acquireUITestReportLock(ctx, root, time.Minute)
			if tt.wantLock {
				if err != nil {
					t.Fatalf("acquire reclaimed lock: %v", err)
				}
				if err := lock.Release(); err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("acquire error = %v, want context deadline", err)
			}
		})
	}
}

func TestUIReportLockReleaseVerifiesTokenOwnership(t *testing.T) {
	runDir := t.TempDir()
	root, err := os.OpenRoot(runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := acquireUITestReportLock(context.Background(), root, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	owner := lock.owner
	owner.Token = "replacement-owner"
	raw, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, uiTestReportLockDir, uiTestReportLockOwnerName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err == nil {
		t.Fatal("release succeeded after ownership token changed")
	}
	if _, err := os.Stat(filepath.Join(runDir, uiTestReportLockDir)); err != nil {
		t.Fatalf("non-owned lock was removed: %v", err)
	}
}

func writeUITestReportLockOwner(t *testing.T, runDir string, owner uiTestReportLockOwner) {
	t.Helper()
	lockDir := filepath.Join(runDir, uiTestReportLockDir)
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, uiTestReportLockOwnerName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func exitedProcessPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	return pid
}
