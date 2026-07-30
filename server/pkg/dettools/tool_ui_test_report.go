package dettools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	uiTestReportToolName     = "ui_test_report"
	uiTestMaxPublishedBytes  = int64(50 << 20)
	uiTestMinViewport        = 320
	uiTestMaxViewportWidth   = 3840
	uiTestMaxViewportHeight  = 2160
	uiTestReportJSONName     = "report.json"
	uiTestReportMarkdownName = "report.md"
	uiTestManifestName       = "artifact-manifest.json"
	uiTestCommentName        = "comment.md"
)

var (
	uiTestTaskIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	uiTestArtifactTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_+./-]{0,63}$`)
	uiTestSecretArtifactName  = regexp.MustCompile(`(?i)(storage[-_. ]*state|authorization|set[-_. ]*cookie|cookie|access[-_. ]*token|id[-_. ]*token|refresh[-_. ]*token|token|password|secret)`)
)

type uiTestReportInput struct {
	SchemaVersion    string               `json:"schema_version"`
	ExecutionStatus  string               `json:"execution_status"`
	Target           uiTestTarget         `json:"target"`
	Environment      uiTestEnvironment    `json:"environment"`
	Scenarios        []uiTestScenario     `json:"scenarios"`
	ObjectiveChecks  []uiObjectiveCheck   `json:"objective_checks"`
	AdvisoryFindings []uiAdvisoryFinding  `json:"advisory_findings"`
	Artifacts        []uiEvidenceArtifact `json:"artifacts"`
}

type uiTestTarget struct {
	URL    string `json:"url"`
	Commit string `json:"commit"`
}

type uiTestEnvironment struct {
	Browser  string         `json:"browser"`
	Viewport uiTestViewport `json:"viewport"`
}

type uiTestViewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type uiTestScenario struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	MachineData map[string]any `json:"machine_data,omitempty"`
}

type uiObjectiveCheck struct {
	ID          string         `json:"id"`
	ScenarioID  string         `json:"scenario_id"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Source      string         `json:"source"`
	Details     string         `json:"details,omitempty"`
	Evidence    []string       `json:"evidence,omitempty"`
	MachineData map[string]any `json:"machine_data,omitempty"`
}

type uiAdvisoryFinding struct {
	ID          string         `json:"id"`
	ScenarioID  string         `json:"scenario_id"`
	Title       string         `json:"title"`
	Category    string         `json:"category"`
	Severity    string         `json:"severity"`
	Observation string         `json:"observation"`
	Impact      string         `json:"impact"`
	Suggestion  string         `json:"suggestion"`
	Evidence    []string       `json:"evidence,omitempty"`
	MachineData map[string]any `json:"machine_data,omitempty"`
}

type uiEvidenceArtifact struct {
	Path        string         `json:"path"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	MachineData map[string]any `json:"machine_data,omitempty"`
}

type uiTestReport struct {
	SchemaVersion    string               `json:"schema_version"`
	ExecutionStatus  string               `json:"execution_status"`
	Verdict          string               `json:"verdict"`
	Target           uiTestTarget         `json:"target"`
	Environment      uiTestEnvironment    `json:"environment"`
	Scenarios        []uiTestScenario     `json:"scenarios"`
	ObjectiveChecks  []uiObjectiveCheck   `json:"objective_checks"`
	AdvisoryFindings []uiAdvisoryFinding  `json:"advisory_findings"`
	Artifacts        []uiEvidenceArtifact `json:"artifacts"`
	Counts           uiTestCounts         `json:"counts"`
}

type uiTestCounts struct {
	Scenarios        uiTestStatusCounts `json:"scenarios"`
	ObjectiveChecks  uiObjectiveCounts  `json:"objective_checks"`
	AdvisoryFindings uiAdvisoryCounts   `json:"advisory_findings"`
}

type uiTestStatusCounts struct {
	Total  int            `json:"total"`
	Status map[string]int `json:"status"`
}

type uiObjectiveCounts struct {
	Total  int            `json:"total"`
	Status map[string]int `json:"status"`
	Source map[string]int `json:"source"`
}

type uiAdvisoryCounts struct {
	Total    int            `json:"total"`
	Category map[string]int `json:"category"`
	Severity map[string]int `json:"severity"`
}

type uiArtifactManifest struct {
	Artifacts []uiArtifactManifestEntry `json:"artifacts"`
}

type uiArtifactManifestEntry struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Size        int64  `json:"size"`
}

type uiReportOutput struct {
	Name    string
	Type    string
	Content []byte
}

type uiReportPublishState struct {
	output    uiReportOutput
	temp      string
	backup    string
	hadPrior  bool
	installed bool
}

var uiTestReportInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "schema_version": {"type": "string", "enum": ["1"]},
    "execution_status": {"type": "string", "enum": ["completed", "infrastructure_error", "blocked"]},
    "target": {
      "type": "object",
      "properties": {
        "url": {"type": "string"},
        "commit": {"type": "string"}
      },
      "required": ["url", "commit"],
      "additionalProperties": false
    },
    "environment": {
      "type": "object",
      "properties": {
        "browser": {"type": "string", "enum": ["chromium"]},
        "viewport": {
          "type": "object",
          "properties": {
            "width": {"type": "integer", "minimum": 320, "maximum": 3840},
            "height": {"type": "integer", "minimum": 320, "maximum": 2160}
          },
          "required": ["width", "height"],
          "additionalProperties": false
        }
      },
      "required": ["browser", "viewport"],
      "additionalProperties": false
    },
    "scenarios": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "name": {"type": "string"},
          "description": {"type": "string"},
          "status": {"type": "string", "enum": ["passed", "failed", "not_run"]},
          "machine_data": {"type": "object"}
        },
        "required": ["id", "name", "status"],
        "additionalProperties": false
      }
    },
    "objective_checks": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "scenario_id": {"type": "string"},
          "name": {"type": "string"},
          "status": {"type": "string", "enum": ["passed", "failed"]},
          "source": {"type": "string", "enum": ["assertion", "console", "network", "accessibility", "regression"]},
          "details": {"type": "string"},
          "evidence": {"type": "array", "items": {"type": "string"}},
          "machine_data": {"type": "object"}
        },
        "required": ["id", "scenario_id", "name", "status", "source"],
        "additionalProperties": false
      }
    },
    "advisory_findings": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "scenario_id": {"type": "string"},
          "title": {"type": "string"},
          "category": {"type": "string", "enum": ["hierarchy", "readability", "spacing", "copy", "discoverability", "consistency", "feedback", "accessibility"]},
          "severity": {"type": "string", "enum": ["high", "medium", "low"]},
          "observation": {"type": "string"},
          "impact": {"type": "string"},
          "suggestion": {"type": "string"},
          "evidence": {"type": "array", "items": {"type": "string"}},
          "machine_data": {"type": "object"}
        },
        "required": ["id", "scenario_id", "title", "category", "severity", "observation", "impact", "suggestion"],
        "additionalProperties": false
      }
    },
    "artifacts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "path": {"type": "string"},
          "type": {"type": "string"},
          "description": {"type": "string"},
          "machine_data": {"type": "object"}
        },
        "required": ["path", "type", "description"],
        "additionalProperties": false
      }
    }
  },
  "required": ["schema_version", "execution_status", "target", "environment", "scenarios", "objective_checks", "advisory_findings", "artifacts"],
  "additionalProperties": false
}`)

func uiTestReportTool() Tool {
	return Tool{
		Name:        uiTestReportToolName,
		Description: "Validate, normalize, redact, and publish deterministic UI-test results. Verdict is derived from execution and objective checks; callers must not supply it. Writes four task-scoped report artifacts.",
		InputSchema: uiTestReportInputSchema,
		Handler:     uiTestReportHandler,
	}
}

func uiTestReportHandler(_ context.Context, args json.RawMessage, env ToolEnv) Result {
	var input uiTestReportInput
	if err := strictUIReportUnmarshal(args, &input); err != nil {
		return Errf(CodeInvalidInput, "invalid ui_test_report input: %v", err)
	}
	report, evidenceSizes, runRoot, err := normalizeUITestReport(input, env)
	if err != nil {
		return Errf(CodeInvalidInput, "invalid ui_test_report input: %v", err)
	}
	defer runRoot.Close()

	redacted, err := redactUITestReport(report)
	if err != nil {
		return Errf(CodeInternal, "redact UI test report: %v", err)
	}
	reportJSON, err := marshalIndented(redacted)
	if err != nil {
		return Errf(CodeInternal, "encode UI test report: %v", err)
	}
	reportMarkdown := renderUITestReportMarkdown(redacted)
	comment := renderUITestComment(redacted)

	runRel := uiTestRunRel(env.ArtifactDir, env.TaskID)
	manifestEntries := make([]uiArtifactManifestEntry, 0, len(redacted.Artifacts)+3)
	for _, artifact := range redacted.Artifacts {
		manifestEntries = append(manifestEntries, uiArtifactManifestEntry{
			Path:        artifact.Path,
			Type:        artifact.Type,
			Description: artifact.Description,
			Size:        evidenceSizes[artifact.Path],
		})
	}
	generated := []uiReportOutput{
		{Name: uiTestReportJSONName, Type: "json", Content: reportJSON},
		{Name: uiTestReportMarkdownName, Type: "markdown", Content: reportMarkdown},
		{Name: uiTestCommentName, Type: "markdown", Content: comment},
	}
	for _, output := range generated {
		manifestEntries = append(manifestEntries, uiArtifactManifestEntry{
			Path:        filepath.ToSlash(filepath.Join(runRel, output.Name)),
			Type:        output.Type,
			Description: uiTestGeneratedDescription(output.Name),
			Size:        int64(len(output.Content)),
		})
	}
	sort.Slice(manifestEntries, func(i, j int) bool { return manifestEntries[i].Path < manifestEntries[j].Path })
	manifestJSON, err := marshalIndented(uiArtifactManifest{Artifacts: manifestEntries})
	if err != nil {
		return Errf(CodeInternal, "encode UI test artifact manifest: %v", err)
	}
	outputs := []uiReportOutput{
		generated[0],
		generated[1],
		{Name: uiTestManifestName, Type: "json", Content: manifestJSON},
		generated[2],
	}

	total := int64(0)
	for _, size := range evidenceSizes {
		total += size
	}
	for _, output := range outputs {
		total += int64(len(output.Content))
	}
	if total > uiTestMaxPublishedBytes {
		return Errf(CodeInvalidInput, "published UI test artifacts total %d bytes; maximum is %d", total, uiTestMaxPublishedBytes)
	}
	if err := publishUITestOutputs(runRoot, outputs); err != nil {
		return Errf(CodeInternal, "publish UI test report: %v", err)
	}

	artifacts := make([]Artifact, 0, len(outputs))
	for _, output := range outputs {
		artifacts = append(artifacts, Artifact{
			Type: output.Type,
			Path: filepath.ToSlash(filepath.Join(runRel, output.Name)),
		})
	}
	return Result{
		Status:  StatusOK,
		Summary: fmt.Sprintf("UI test report: %s / %s", redacted.ExecutionStatus, redacted.Verdict),
		MachineData: map[string]any{
			"execution_status": redacted.ExecutionStatus,
			"verdict":          redacted.Verdict,
			"counts":           redacted.Counts,
		},
		Artifacts: artifacts,
	}
}

func strictUIReportUnmarshal(data json.RawMessage, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func normalizeUITestReport(input uiTestReportInput, env ToolEnv) (uiTestReport, map[string]int64, *os.Root, error) {
	if err := validateUITestTaskID(env.TaskID); err != nil {
		return uiTestReport{}, nil, nil, err
	}
	if err := validateUITestInput(input); err != nil {
		return uiTestReport{}, nil, nil, err
	}
	workDir, err := filepath.Abs(env.WorkDir)
	if err != nil {
		return uiTestReport{}, nil, nil, fmt.Errorf("resolve workdir: %w", err)
	}
	workRoot, err := os.OpenRoot(workDir)
	if err != nil {
		return uiTestReport{}, nil, nil, fmt.Errorf("open workdir: %w", err)
	}
	defer workRoot.Close()

	runRel := uiTestRunRel(env.ArtifactDir, env.TaskID)
	if filepath.IsAbs(runRel) || pathEscapesRoot(runRel) {
		return uiTestReport{}, nil, nil, fmt.Errorf("UI test artifact root escapes workdir")
	}
	if err := workRoot.MkdirAll(runRel, 0o755); err != nil {
		return uiTestReport{}, nil, nil, fmt.Errorf("create UI test artifact root: %w", err)
	}
	if err := rejectSymlinkComponents(workRoot, runRel); err != nil {
		return uiTestReport{}, nil, nil, fmt.Errorf("unsafe UI test artifact root: %w", err)
	}

	evidenceSizes := make(map[string]int64, len(input.Artifacts))
	evidencePaths := make(map[string]bool, len(input.Artifacts))
	var total int64
	for i := range input.Artifacts {
		artifact := &input.Artifacts[i]
		normalizedPath, size, err := validateUITestEvidence(workRoot, runRel, *artifact)
		if err != nil {
			return uiTestReport{}, nil, nil, fmt.Errorf("artifact %q: %w", artifact.Path, err)
		}
		if evidencePaths[normalizedPath] {
			return uiTestReport{}, nil, nil, fmt.Errorf("duplicate artifact path %q", normalizedPath)
		}
		evidencePaths[normalizedPath] = true
		evidenceSizes[normalizedPath] = size
		artifact.Path = normalizedPath
		total += size
		if total > uiTestMaxPublishedBytes {
			return uiTestReport{}, nil, nil, fmt.Errorf("evidence totals %d bytes; maximum is %d", total, uiTestMaxPublishedBytes)
		}
	}
	for _, check := range input.ObjectiveChecks {
		for _, path := range check.Evidence {
			if !evidencePaths[normalizeUITestPath(path)] {
				return uiTestReport{}, nil, nil, fmt.Errorf("objective check %q references unknown evidence %q", check.ID, path)
			}
		}
	}
	for _, finding := range input.AdvisoryFindings {
		for _, path := range finding.Evidence {
			if !evidencePaths[normalizeUITestPath(path)] {
				return uiTestReport{}, nil, nil, fmt.Errorf("advisory finding %q references unknown evidence %q", finding.ID, path)
			}
		}
	}

	sort.Slice(input.Scenarios, func(i, j int) bool { return input.Scenarios[i].ID < input.Scenarios[j].ID })
	sort.Slice(input.ObjectiveChecks, func(i, j int) bool { return input.ObjectiveChecks[i].ID < input.ObjectiveChecks[j].ID })
	sort.Slice(input.AdvisoryFindings, func(i, j int) bool { return input.AdvisoryFindings[i].ID < input.AdvisoryFindings[j].ID })
	sort.Slice(input.Artifacts, func(i, j int) bool { return input.Artifacts[i].Path < input.Artifacts[j].Path })
	for i := range input.ObjectiveChecks {
		sort.Strings(input.ObjectiveChecks[i].Evidence)
		for j, path := range input.ObjectiveChecks[i].Evidence {
			input.ObjectiveChecks[i].Evidence[j] = normalizeUITestPath(path)
		}
	}
	for i := range input.AdvisoryFindings {
		sort.Strings(input.AdvisoryFindings[i].Evidence)
		for j, path := range input.AdvisoryFindings[i].Evidence {
			input.AdvisoryFindings[i].Evidence[j] = normalizeUITestPath(path)
		}
	}

	report := uiTestReport{
		SchemaVersion:    input.SchemaVersion,
		ExecutionStatus:  input.ExecutionStatus,
		Verdict:          deriveUITestVerdict(input),
		Target:           input.Target,
		Environment:      input.Environment,
		Scenarios:        input.Scenarios,
		ObjectiveChecks:  input.ObjectiveChecks,
		AdvisoryFindings: input.AdvisoryFindings,
		Artifacts:        input.Artifacts,
		Counts:           deriveUITestCounts(input),
	}
	runRoot, err := workRoot.OpenRoot(runRel)
	if err != nil {
		return uiTestReport{}, nil, nil, fmt.Errorf("open UI test artifact root: %w", err)
	}
	return report, evidenceSizes, runRoot, nil
}

func validateUITestInput(input uiTestReportInput) error {
	if input.SchemaVersion != "1" {
		return fmt.Errorf("schema_version must be %q", "1")
	}
	if !oneOf(input.ExecutionStatus, "completed", "infrastructure_error", "blocked") {
		return fmt.Errorf("unsupported execution_status %q", input.ExecutionStatus)
	}
	if input.Scenarios == nil || input.ObjectiveChecks == nil || input.AdvisoryFindings == nil || input.Artifacts == nil {
		return fmt.Errorf("scenarios, objective_checks, advisory_findings, and artifacts are required arrays")
	}
	if input.ExecutionStatus == "completed" && len(input.Scenarios) == 0 {
		return fmt.Errorf("completed report requires at least one scenario")
	}
	if err := validateUITestTarget(input.Target); err != nil {
		return err
	}
	if input.Environment.Browser != "chromium" {
		return fmt.Errorf("browser must be %q", "chromium")
	}
	if input.Environment.Viewport.Width < uiTestMinViewport || input.Environment.Viewport.Width > uiTestMaxViewportWidth {
		return fmt.Errorf("viewport width must be between %d and %d", uiTestMinViewport, uiTestMaxViewportWidth)
	}
	if input.Environment.Viewport.Height < uiTestMinViewport || input.Environment.Viewport.Height > uiTestMaxViewportHeight {
		return fmt.Errorf("viewport height must be between %d and %d", uiTestMinViewport, uiTestMaxViewportHeight)
	}

	ids := map[string]bool{}
	scenarios := make(map[string]bool, len(input.Scenarios))
	for _, scenario := range input.Scenarios {
		if err := validateUITestIDAndName(scenario.ID, scenario.Name, "scenario", ids); err != nil {
			return err
		}
		if !oneOf(scenario.Status, "passed", "failed", "not_run") {
			return fmt.Errorf("scenario %q has unsupported status %q", scenario.ID, scenario.Status)
		}
		scenarios[scenario.ID] = true
	}
	for _, check := range input.ObjectiveChecks {
		if err := validateUITestIDAndName(check.ID, check.Name, "objective check", ids); err != nil {
			return err
		}
		if !scenarios[check.ScenarioID] {
			return fmt.Errorf("objective check %q references unknown scenario %q", check.ID, check.ScenarioID)
		}
		if !oneOf(check.Status, "passed", "failed") {
			return fmt.Errorf("objective check %q has unsupported status %q", check.ID, check.Status)
		}
		if !oneOf(check.Source, "assertion", "console", "network", "accessibility", "regression") {
			return fmt.Errorf("objective check %q has unsupported source %q", check.ID, check.Source)
		}
	}
	for _, finding := range input.AdvisoryFindings {
		if err := validateUITestIDAndName(finding.ID, finding.Title, "advisory finding", ids); err != nil {
			return err
		}
		if !scenarios[finding.ScenarioID] {
			return fmt.Errorf("advisory finding %q references unknown scenario %q", finding.ID, finding.ScenarioID)
		}
		if !oneOf(finding.Category, "hierarchy", "readability", "spacing", "copy", "discoverability", "consistency", "feedback", "accessibility") {
			return fmt.Errorf("advisory finding %q has unsupported category %q", finding.ID, finding.Category)
		}
		if !oneOf(finding.Severity, "high", "medium", "low") {
			return fmt.Errorf("advisory finding %q has unsupported severity %q", finding.ID, finding.Severity)
		}
		if strings.TrimSpace(finding.Observation) == "" || strings.TrimSpace(finding.Impact) == "" || strings.TrimSpace(finding.Suggestion) == "" {
			return fmt.Errorf("advisory finding %q requires observation, impact, and suggestion", finding.ID)
		}
	}
	for _, artifact := range input.Artifacts {
		if strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.Description) == "" {
			return fmt.Errorf("artifact path and description are required")
		}
		if !uiTestArtifactTypePattern.MatchString(artifact.Type) {
			return fmt.Errorf("artifact %q has invalid type %q", artifact.Path, artifact.Type)
		}
		if uiTestSecretArtifactName.MatchString(artifact.Type) {
			return fmt.Errorf("artifact %q has secret-bearing type %q", artifact.Path, artifact.Type)
		}
	}
	return nil
}

func validateUITestIDAndName(id, name, kind string, ids map[string]bool) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s id and name are required", kind)
	}
	if ids[id] {
		return fmt.Errorf("duplicate id %q", id)
	}
	ids[id] = true
	return nil
}

func validateUITestTarget(target uiTestTarget) error {
	if strings.TrimSpace(target.Commit) == "" {
		return fmt.Errorf("target commit is required")
	}
	parsed, err := url.Parse(target.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("target URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("target URL must use http or https")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("target URL must not contain credentials or a fragment")
	}
	host := strings.ToLower(parsed.Hostname())
	if !oneOf(host, "localhost", "127.0.0.1", "::1") {
		return fmt.Errorf("target URL must use localhost, 127.0.0.1, or ::1")
	}
	return nil
}

func validateUITestTaskID(taskID string) error {
	if !uiTestTaskIDPattern.MatchString(taskID) || taskID == "." || taskID == ".." {
		return fmt.Errorf("invalid task id %q", taskID)
	}
	return nil
}

func validateUITestEvidence(workRoot *os.Root, runRel string, artifact uiEvidenceArtifact) (string, int64, error) {
	if uiTestSecretArtifactName.MatchString(artifact.Path) || redactUITestString(artifact.Path) != artifact.Path {
		return "", 0, fmt.Errorf("secret-bearing artifact path is not publishable")
	}
	clean := filepath.Clean(filepath.FromSlash(artifact.Path))
	if filepath.IsAbs(clean) || pathEscapesRoot(clean) {
		return "", 0, fmt.Errorf("path escapes workdir")
	}
	relative, err := filepath.Rel(runRel, clean)
	if err != nil || relative == "." || pathEscapesRoot(relative) {
		return "", 0, fmt.Errorf("path must be a file under %s", filepath.ToSlash(runRel))
	}
	if isGeneratedUITestOutput(relative) {
		return "", 0, fmt.Errorf("path uses reserved generated report name")
	}
	if err := rejectSymlinkComponents(workRoot, clean); err != nil {
		return "", 0, err
	}
	file, err := workRoot.Open(clean)
	if err != nil {
		return "", 0, fmt.Errorf("open evidence: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat evidence: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("evidence is not a regular file")
	}
	return filepath.ToSlash(clean), info.Size(), nil
}

func rejectSymlinkComponents(root *os.Root, name string) error {
	clean := filepath.Clean(name)
	current := ""
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s contains a symbolic link", filepath.ToSlash(current))
		}
	}
	return nil
}

func deriveUITestVerdict(input uiTestReportInput) string {
	if input.ExecutionStatus != "completed" {
		return "not_run"
	}
	for _, check := range input.ObjectiveChecks {
		if check.Status == "failed" {
			return "fail"
		}
	}
	return "pass"
}

func deriveUITestCounts(input uiTestReportInput) uiTestCounts {
	counts := uiTestCounts{
		Scenarios: uiTestStatusCounts{Total: len(input.Scenarios), Status: countKeys("passed", "failed", "not_run")},
		ObjectiveChecks: uiObjectiveCounts{
			Total: len(input.ObjectiveChecks), Status: countKeys("passed", "failed"),
			Source: countKeys("assertion", "console", "network", "accessibility", "regression"),
		},
		AdvisoryFindings: uiAdvisoryCounts{
			Total: len(input.AdvisoryFindings),
			Category: countKeys(
				"hierarchy", "readability", "spacing", "copy", "discoverability", "consistency", "feedback", "accessibility",
			),
			Severity: countKeys("high", "medium", "low"),
		},
	}
	for _, scenario := range input.Scenarios {
		counts.Scenarios.Status[scenario.Status]++
	}
	for _, check := range input.ObjectiveChecks {
		counts.ObjectiveChecks.Status[check.Status]++
		counts.ObjectiveChecks.Source[check.Source]++
	}
	for _, finding := range input.AdvisoryFindings {
		counts.AdvisoryFindings.Category[finding.Category]++
		counts.AdvisoryFindings.Severity[finding.Severity]++
	}
	return counts
}

func countKeys(keys ...string) map[string]int {
	counts := make(map[string]int, len(keys))
	for _, key := range keys {
		counts[key] = 0
	}
	return counts
}

func redactUITestReport(report uiTestReport) (uiTestReport, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return uiTestReport{}, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return uiTestReport{}, err
	}
	raw, err = json.Marshal(redactUITestValue(generic))
	if err != nil {
		return uiTestReport{}, err
	}
	var redacted uiTestReport
	if err := json.Unmarshal(raw, &redacted); err != nil {
		return uiTestReport{}, err
	}
	return redacted, nil
}

func marshalIndented(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func renderUITestReportMarkdown(report uiTestReport) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "# UI Test Report\n\n")
	fmt.Fprintf(&out, "- Verdict: %s\n", markdownInline(report.Verdict))
	fmt.Fprintf(&out, "- Execution status: %s\n", markdownInline(report.ExecutionStatus))
	fmt.Fprintf(&out, "- Target: %s\n", markdownInline(report.Target.URL))
	fmt.Fprintf(&out, "- Commit: %s\n", markdownInline(report.Target.Commit))
	fmt.Fprintf(&out, "- Browser: %s\n", markdownInline(report.Environment.Browser))
	fmt.Fprintf(&out, "- Viewport: %dx%d\n\n", report.Environment.Viewport.Width, report.Environment.Viewport.Height)

	fmt.Fprintf(&out, "## Scenarios\n\n")
	for _, scenario := range report.Scenarios {
		fmt.Fprintf(&out, "- %s [%s]: %s", markdownInline(scenario.ID), markdownInline(scenario.Status), markdownInline(scenario.Name))
		if scenario.Description != "" {
			fmt.Fprintf(&out, " — %s", markdownInline(scenario.Description))
		}
		out.WriteString("\n")
		writeUITestMachineData(&out, scenario.MachineData)
	}
	if len(report.Scenarios) == 0 {
		out.WriteString("- None\n")
	}

	fmt.Fprintf(&out, "\n## Objective Checks\n\n")
	for _, check := range report.ObjectiveChecks {
		fmt.Fprintf(&out, "- %s [%s/%s] %s: %s\n",
			markdownInline(check.ID), markdownInline(check.Status), markdownInline(check.Source),
			markdownInline(check.ScenarioID), markdownInline(check.Name),
		)
		if check.Details != "" {
			fmt.Fprintf(&out, "  - Details: %s\n", markdownInline(check.Details))
		}
		writeUITestEvidenceRefs(&out, check.Evidence)
		writeUITestMachineData(&out, check.MachineData)
	}
	if len(report.ObjectiveChecks) == 0 {
		out.WriteString("- None\n")
	}

	fmt.Fprintf(&out, "\n## Advisory Findings\n\n")
	for _, finding := range report.AdvisoryFindings {
		fmt.Fprintf(&out, "- %s [%s/%s] %s: %s\n",
			markdownInline(finding.ID), markdownInline(finding.Severity), markdownInline(finding.Category),
			markdownInline(finding.ScenarioID), markdownInline(finding.Title),
		)
		fmt.Fprintf(&out, "  - Observation: %s\n", markdownInline(finding.Observation))
		fmt.Fprintf(&out, "  - Impact: %s\n", markdownInline(finding.Impact))
		fmt.Fprintf(&out, "  - Suggested direction: %s\n", markdownInline(finding.Suggestion))
		writeUITestEvidenceRefs(&out, finding.Evidence)
		writeUITestMachineData(&out, finding.MachineData)
	}
	if len(report.AdvisoryFindings) == 0 {
		out.WriteString("- None\n")
	}

	fmt.Fprintf(&out, "\n## Evidence\n\n")
	for _, artifact := range report.Artifacts {
		fmt.Fprintf(&out, "- `%s` [%s]: %s\n",
			markdownCode(artifact.Path), markdownInline(artifact.Type), markdownInline(artifact.Description),
		)
		writeUITestMachineData(&out, artifact.MachineData)
	}
	if len(report.Artifacts) == 0 {
		out.WriteString("- None\n")
	}
	return []byte(out.String())
}

func renderUITestComment(report uiTestReport) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "## UI Test Result\n\n")
	fmt.Fprintf(&out, "- Verdict: %s\n", markdownInline(report.Verdict))
	fmt.Fprintf(&out, "- Execution status: %s\n", markdownInline(report.ExecutionStatus))
	fmt.Fprintf(&out, "- Target: %s\n", markdownInline(report.Target.URL))
	fmt.Fprintf(&out, "- Commit: %s\n", markdownInline(report.Target.Commit))
	fmt.Fprintf(&out, "- Scenarios: %d total, %d passed, %d failed, %d not run\n",
		report.Counts.Scenarios.Total,
		report.Counts.Scenarios.Status["passed"],
		report.Counts.Scenarios.Status["failed"],
		report.Counts.Scenarios.Status["not_run"],
	)
	fmt.Fprintf(&out, "- Objective checks: %d total, %d passed, %d failed\n",
		report.Counts.ObjectiveChecks.Total,
		report.Counts.ObjectiveChecks.Status["passed"],
		report.Counts.ObjectiveChecks.Status["failed"],
	)
	fmt.Fprintf(&out, "- Advisory findings: %d\n", report.Counts.AdvisoryFindings.Total)

	failures := make([]uiObjectiveCheck, 0)
	for _, check := range report.ObjectiveChecks {
		if check.Status == "failed" {
			failures = append(failures, check)
		}
	}
	if len(failures) > 0 {
		out.WriteString("\nObjective failures:\n")
		for _, check := range failures {
			fmt.Fprintf(&out, "- %s: %s", markdownInline(check.ID), markdownInline(check.Name))
			if check.Details != "" {
				fmt.Fprintf(&out, " — %s", markdownInline(check.Details))
			}
			out.WriteString("\n")
		}
	}
	out.WriteString("\nUse the attached report and evidence for full details.\n")
	return []byte(out.String())
}

func writeUITestEvidenceRefs(out *strings.Builder, evidence []string) {
	for _, path := range evidence {
		fmt.Fprintf(out, "  - Evidence: `%s`\n", markdownCode(path))
	}
}

func writeUITestMachineData(out *strings.Builder, data map[string]any) {
	if len(data) == 0 {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(out, "  - Machine data: `%s`\n", markdownCode(string(raw)))
}

func markdownInline(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "file://", "file:[REDACTED]")
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`[`, `\[`,
		`]`, `\]`,
		`(`, `\(`,
		`)`, `\)`,
		`<`, `\<`,
		`>`, `\>`,
		`*`, `\*`,
		`_`, `\_`,
		"`", "'",
	)
	return replacer.Replace(value)
}

func markdownCode(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}

func publishUITestOutputs(root *os.Root, outputs []uiReportOutput) error {
	suffix, err := uiTestTempSuffix()
	if err != nil {
		return err
	}
	states := make([]uiReportPublishState, len(outputs))
	for i, output := range outputs {
		if info, statErr := root.Lstat(output.Name); statErr == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("existing output %s is not a regular file", output.Name)
			}
			states[i].hadPrior = true
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect existing output %s: %w", output.Name, statErr)
		}
		states[i].output = output
		states[i].temp = "." + output.Name + ".tmp-" + suffix
		states[i].backup = "." + output.Name + ".bak-" + suffix
	}
	defer func() {
		for _, item := range states {
			_ = root.Remove(item.temp)
			_ = root.Remove(item.backup)
		}
	}()

	for _, item := range states {
		file, openErr := root.OpenFile(item.temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if openErr != nil {
			return fmt.Errorf("stage %s: %w", item.output.Name, openErr)
		}
		_, writeErr := file.Write(item.output.Content)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return fmt.Errorf("stage %s: %w", item.output.Name, writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("stage %s: %w", item.output.Name, closeErr)
		}
	}

	backedUp := 0
	for i := range states {
		if !states[i].hadPrior {
			continue
		}
		if err := root.Rename(states[i].output.Name, states[i].backup); err != nil {
			restoreUITestBackups(root, states[:backedUp])
			return fmt.Errorf("backup %s: %w", states[i].output.Name, err)
		}
		backedUp = i + 1
	}
	for i := range states {
		if err := root.Rename(states[i].temp, states[i].output.Name); err != nil {
			for j := 0; j < i; j++ {
				_ = root.Remove(states[j].output.Name)
			}
			restoreUITestBackups(root, states)
			return fmt.Errorf("install %s: %w", states[i].output.Name, err)
		}
		states[i].installed = true
	}
	for _, item := range states {
		if item.hadPrior {
			if err := root.Remove(item.backup); err != nil {
				return fmt.Errorf("remove backup for %s: %w", item.output.Name, err)
			}
		}
	}
	return nil
}

func restoreUITestBackups(root *os.Root, states []uiReportPublishState) {
	for _, item := range states {
		if item.hadPrior {
			_ = root.Rename(item.backup, item.output.Name)
		}
	}
}

func uiTestTempSuffix() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate staging suffix: %w", err)
	}
	return hex.EncodeToString(random[:]), nil
}

func uiTestRunRel(artifactDir, taskID string) string {
	return filepath.Clean(filepath.Join(artifactDir, "ui-test", taskID))
}

func normalizeUITestPath(path string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
}

func pathEscapesRoot(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func isGeneratedUITestOutput(path string) bool {
	switch filepath.Base(path) {
	case uiTestReportJSONName, uiTestReportMarkdownName, uiTestManifestName, uiTestCommentName:
		return filepath.Dir(path) == "."
	default:
		return false
	}
}

func uiTestGeneratedDescription(name string) string {
	switch name {
	case uiTestReportJSONName:
		return "Normalized UI test report"
	case uiTestReportMarkdownName:
		return "Human-readable UI test report"
	case uiTestCommentName:
		return "Issue comment summary"
	default:
		return "Generated UI test artifact"
	}
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
