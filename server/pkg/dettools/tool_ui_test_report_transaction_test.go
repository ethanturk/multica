package dettools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUIReportRecoversInterruptedPublicationAtEveryBoundary(t *testing.T) {
	canonicalNames := []string{
		uiTestPublishedDir,
		uiTestReportJSONName,
		uiTestReportMarkdownName,
		uiTestManifestName,
		uiTestCommentName,
	}
	var tests []struct {
		point          string
		committedModel bool
	}
	for _, point := range []string{
		"before journal",
		"after journal",
		"before staging",
		"after staging",
	} {
		tests = append(tests, struct {
			point          string
			committedModel bool
		}{point: point})
	}
	for _, operation := range []string{"backup", "install"} {
		for _, name := range canonicalNames {
			for _, boundary := range []string{"before", "after"} {
				tests = append(tests, struct {
					point          string
					committedModel bool
				}{point: fmt.Sprintf("%s %s %s", boundary, operation, name)})
			}
		}
	}
	tests = append(tests,
		struct {
			point          string
			committedModel bool
		}{point: "before commit marker"},
		struct {
			point          string
			committedModel bool
		}{point: "after commit marker", committedModel: true},
	)
	for _, operation := range []string{"cleanup backup", "cleanup temp"} {
		for _, name := range canonicalNames {
			for _, boundary := range []string{"before", "after"} {
				tests = append(tests, struct {
					point          string
					committedModel bool
				}{point: fmt.Sprintf("%s %s %s", boundary, operation, name), committedModel: true})
			}
		}
	}
	for _, operation := range []string{"cleanup commit temp", "cleanup journal", "cleanup marker"} {
		for _, boundary := range []string{"before", "after"} {
			tests = append(tests, struct {
				point          string
				committedModel bool
			}{point: fmt.Sprintf("%s %s", boundary, operation), committedModel: true})
		}
	}

	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.point, " ", "_"), func(t *testing.T) {
			workDir := t.TempDir()
			taskID := "task-transaction"
			oldInput := uiTransactionModelInput(t, workDir, taskID, "old")
			if result := runUIReport(t, workDir, taskID, oldInput); result.Status != StatusOK {
				t.Fatalf("old publication = %+v", result)
			}

			newInput := uiTransactionModelInput(t, workDir, taskID, "new")
			raw, err := json.Marshal(newInput)
			if err != nil {
				t.Fatal(err)
			}
			triggered := false
			ctx := context.WithValue(
				context.Background(),
				uiTestPublicationFailpointContextKey{},
				uiTestPublicationFailpoint(func(point string) error {
					if point != tt.point || triggered {
						return nil
					}
					triggered = true
					return errUITestPublicationInterrupted
				}),
			)
			result := uiTestReportHandler(ctx, raw, ToolEnv{
				WorkDir: workDir, ArtifactDir: DefaultArtifactDir, TaskID: taskID,
			})
			if !triggered {
				t.Fatalf("failpoint %q was not reached", tt.point)
			}
			if tt.committedModel {
				if result.Status != StatusOK {
					t.Fatalf("committed interruption result = %+v", result)
				}
				if pending, _ := result.MachineData["publication_cleanup_pending"].(bool); !pending {
					t.Fatalf("committed interruption did not report pending cleanup: %+v", result.MachineData)
				}
				if len(result.Artifacts) != 4 {
					t.Fatalf("committed interruption artifacts = %d, want 4", len(result.Artifacts))
				}
			} else if result.Status != StatusError || result.ErrorCode != CodeInternal {
				t.Fatalf("precommit interruption result = %+v", result)
			}

			recoveryInput := uiReportFixture()
			recoveryInput["artifacts"] = []any{map[string]any{
				"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "missing.png")),
				"type": "screenshot", "description": "Missing after recovery",
			}}
			recoveryResult := runUIReport(t, workDir, taskID, recoveryInput)
			if recoveryResult.Status != StatusError || recoveryResult.ErrorCode != CodeInvalidInput {
				t.Fatalf("recovery invocation = %+v", recoveryResult)
			}
			wantModel := "old"
			if tt.committedModel {
				wantModel = "new"
			}
			assertUITransactionModel(t, workDir, taskID, wantModel)
			assertNoUITransactionResidue(t, uiReportRunDir(workDir, taskID))
		})
	}
}

func TestUIReportRecoversInterruptedInitialPublication(t *testing.T) {
	points := []string{"before commit marker"}
	for _, name := range []string{
		uiTestPublishedDir,
		uiTestReportJSONName,
		uiTestReportMarkdownName,
		uiTestManifestName,
		uiTestCommentName,
	} {
		points = append(points, "after install "+name)
	}
	for _, point := range points {
		t.Run(strings.ReplaceAll(point, " ", "_"), func(t *testing.T) {
			workDir := t.TempDir()
			taskID := "task-initial-transaction"
			input := uiTransactionModelInput(t, workDir, taskID, "new")
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			triggered := false
			ctx := context.WithValue(
				context.Background(),
				uiTestPublicationFailpointContextKey{},
				uiTestPublicationFailpoint(func(candidate string) error {
					if candidate != point || triggered {
						return nil
					}
					triggered = true
					return errUITestPublicationInterrupted
				}),
			)
			result := uiTestReportHandler(ctx, raw, ToolEnv{
				WorkDir: workDir, ArtifactDir: DefaultArtifactDir, TaskID: taskID,
			})
			if !triggered || result.Status != StatusError || result.ErrorCode != CodeInternal {
				t.Fatalf("interrupted initial publication = %+v, triggered=%v", result, triggered)
			}

			recoveryInput := uiReportFixture()
			recoveryInput["artifacts"] = []any{map[string]any{
				"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "missing.png")),
				"type": "screenshot", "description": "Missing after recovery",
			}}
			if recovery := runUIReport(t, workDir, taskID, recoveryInput); recovery.Status != StatusError ||
				recovery.ErrorCode != CodeInvalidInput {
				t.Fatalf("recovery invocation = %+v", recovery)
			}
			runDir := uiReportRunDir(workDir, taskID)
			for _, name := range []string{
				uiTestPublishedDir,
				uiTestReportJSONName,
				uiTestReportMarkdownName,
				uiTestManifestName,
				uiTestCommentName,
			} {
				if _, err := os.Lstat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
					t.Fatalf("partial canonical item %s remains: %v", name, err)
				}
			}
			assertNoUITransactionResidue(t, runDir)
			if clean := runUIReport(t, workDir, taskID, input); clean.Status != StatusOK {
				t.Fatalf("clean publication after recovery = %+v", clean)
			}
			assertUITransactionModel(t, workDir, taskID, "new")
		})
	}
}

func TestUIReportRecoveryRejectsForgedOrMalformedJournalWithoutCanonicalWrites(t *testing.T) {
	tests := []struct {
		name    string
		journal string
		marker  string
	}{
		{
			name: "forged path",
			journal: `{"version":1,"token":"0123456789abcdef","items":[
				{"name":"../report.json","directory":true,"had_prior":true},
				{"name":"report.json","directory":false,"had_prior":true},
				{"name":"report.md","directory":false,"had_prior":true},
				{"name":"artifact-manifest.json","directory":false,"had_prior":true},
				{"name":"comment.md","directory":false,"had_prior":true}
			]}`,
		},
		{
			name:    "unknown journal field",
			journal: `{"version":1,"token":"0123456789abcdef","items":[],"path":"../outside"}`,
		},
		{
			name: "token mismatch",
			journal: `{"version":1,"token":"0123456789abcdef","items":[
				{"name":"published-evidence","directory":true,"had_prior":true},
				{"name":"report.json","directory":false,"had_prior":true},
				{"name":"report.md","directory":false,"had_prior":true},
				{"name":"artifact-manifest.json","directory":false,"had_prior":true},
				{"name":"comment.md","directory":false,"had_prior":true}
			]}`,
			marker: `{"version":1,"token":"fedcba9876543210"}`,
		},
		{
			name: "unknown marker field",
			journal: `{"version":1,"token":"0123456789abcdef","items":[
				{"name":"published-evidence","directory":true,"had_prior":true},
				{"name":"report.json","directory":false,"had_prior":true},
				{"name":"report.md","directory":false,"had_prior":true},
				{"name":"artifact-manifest.json","directory":false,"had_prior":true},
				{"name":"comment.md","directory":false,"had_prior":true}
			]}`,
			marker: `{"version":1,"token":"0123456789abcdef","path":"../outside"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			taskID := "task-forged-journal"
			oldInput := uiTransactionModelInput(t, workDir, taskID, "old")
			if result := runUIReport(t, workDir, taskID, oldInput); result.Status != StatusOK {
				t.Fatalf("old publication = %+v", result)
			}
			runDir := uiReportRunDir(workDir, taskID)
			before := snapshotUICanonicalFiles(t, runDir)
			if err := os.WriteFile(filepath.Join(runDir, uiTestPublicationJournalName), []byte(tt.journal), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.marker != "" {
				if err := os.WriteFile(filepath.Join(runDir, uiTestPublicationCommitName), []byte(tt.marker), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			result := runUIReport(t, workDir, taskID, uiReportFixture())
			if result.Status != StatusError || result.ErrorCode != CodeInternal {
				t.Fatalf("result = %+v, want fail-closed INTERNAL_ERROR", result)
			}
			after := snapshotUICanonicalFiles(t, runDir)
			if !equalUICanonicalSnapshots(before, after) {
				t.Fatal("malformed transaction recovery touched canonical outputs")
			}
		})
	}
}

func uiTransactionModelInput(t *testing.T, workDir, taskID, model string) map[string]any {
	t.Helper()
	runDir := uiReportRunDir(workDir, taskID)
	sourceDir := filepath.Join(runDir, "sources")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, model+".png")
	if err := os.WriteFile(source, []byte("evidence-"+model), 0o600); err != nil {
		t.Fatal(err)
	}
	input := uiReportFixture()
	input["target"].(map[string]any)["commit"] = model
	input["scenarios"] = []any{map[string]any{
		"id": "scenario-model", "name": model, "status": "passed",
	}}
	input["artifacts"] = []any{map[string]any{
		"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "sources", model+".png")),
		"type": "screenshot", "description": model,
	}}
	return input
}

func assertUITransactionModel(t *testing.T, workDir, taskID, model string) {
	t.Helper()
	runDir := uiReportRunDir(workDir, taskID)
	reportRaw, err := os.ReadFile(filepath.Join(runDir, uiTestReportJSONName))
	if err != nil {
		t.Fatal(err)
	}
	var report uiTestReport
	if err := json.Unmarshal(reportRaw, &report); err != nil {
		t.Fatal(err)
	}
	wantEvidencePath := filepath.ToSlash(filepath.Join(
		DefaultArtifactDir, "ui-test", taskID, uiTestPublishedDir, "sources", model+".png",
	))
	if len(report.Scenarios) != 1 || report.Scenarios[0].Name != model ||
		len(report.Artifacts) != 1 || report.Artifacts[0].Path != wantEvidencePath {
		t.Fatalf("report is not %q model: %+v", model, report)
	}
	evidence, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(wantEvidencePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(evidence) != "evidence-"+model {
		t.Fatalf("evidence = %q, want model %q", evidence, model)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(runDir, uiTestManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest uiArtifactManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == wantEvidencePath {
			found = true
		}
		if strings.Contains(artifact.Path, "/old.png") && model != "old" ||
			strings.Contains(artifact.Path, "/new.png") && model != "new" {
			t.Fatalf("manifest mixes models: %+v", manifest.Artifacts)
		}
	}
	if !found {
		t.Fatalf("manifest missing %q: %+v", wantEvidencePath, manifest.Artifacts)
	}
	for _, name := range []string{uiTestReportMarkdownName, uiTestCommentName} {
		raw, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), model) {
			t.Fatalf("%s does not contain model %q", name, model)
		}
	}
}

func assertNoUITransactionResidue(t *testing.T, runDir string) {
	t.Helper()
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == uiTestPublicationJournalName || name == uiTestPublicationCommitName ||
			strings.Contains(name, ".tmp-") || strings.Contains(name, ".bak-") {
			t.Fatalf("transaction residue remains: %s", name)
		}
	}
}

func snapshotUICanonicalFiles(t *testing.T, runDir string) map[string][]byte {
	t.Helper()
	snapshot := make(map[string][]byte)
	for _, name := range []string{
		uiTestReportJSONName,
		uiTestReportMarkdownName,
		uiTestManifestName,
		uiTestCommentName,
	} {
		raw, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			t.Fatal(err)
		}
		snapshot[name] = raw
	}
	err := filepath.WalkDir(filepath.Join(runDir, uiTestPublishedDir), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(runDir, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(relative)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func equalUICanonicalSnapshots(first, second map[string][]byte) bool {
	if len(first) != len(second) {
		return false
	}
	for name, firstRaw := range first {
		secondRaw, ok := second[name]
		if !ok || !bytes.Equal(firstRaw, secondRaw) {
			return false
		}
	}
	return true
}
