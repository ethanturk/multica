package dettools

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUIReportRejectsDeclaredArtifactTypeBypasses(t *testing.T) {
	storageState := []byte(`{"cookies":[{"name":"session","value":"storage-secret"}],"origins":[]}`)
	validPNG := uiTestPNGFixture(t, color.RGBA{R: 0x21, G: 0x43, B: 0x65, A: 0xff})
	safeTrace := uiTestZIPFixture(t, map[string][]byte{"trace.trace": []byte("safe trace")})
	tests := []struct {
		name         string
		artifactType string
		content      []byte
	}{
		{name: "storage state declared screenshot", artifactType: "screenshot", content: storageState},
		{name: "storage state declared trace", artifactType: "trace", content: storageState},
		{name: "text declared screenshot", artifactType: "screenshot", content: []byte("plain text")},
		{name: "zip declared screenshot", artifactType: "screenshot", content: safeTrace},
		{
			name:         "png with trailing storage state declared screenshot",
			artifactType: "screenshot",
			content:      append(append([]byte(nil), validPNG...), storageState...),
		},
		{name: "png declared trace", artifactType: "trace", content: validPNG},
		{name: "safe trace rejected in v1", artifactType: "trace", content: safeTrace},
		{
			name:         "trace containing secret HAR rejected",
			artifactType: "trace",
			content: uiTestZIPFixture(t, map[string][]byte{
				"network.har": []byte(`{"headers":[{"name":"Authorization","value":"trace-secret"}]}`),
			}),
		},
		{
			name:         "trace containing storage state rejected",
			artifactType: "trace",
			content:      uiTestZIPFixture(t, map[string][]byte{"resources/state.json": storageState}),
		},
		{
			name:         "trace traversal entry rejected",
			artifactType: "trace",
			content:      uiTestZIPFixture(t, map[string][]byte{"../outside.txt": []byte("outside")}),
		},
		{
			name:         "trace oversized entry rejected",
			artifactType: "trace",
			content:      uiTestZIPFixture(t, map[string][]byte{"large.bin": bytes.Repeat([]byte("x"), 1<<20)}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			taskID := "task-type-bypass"
			runDir := uiReportRunDir(workDir, taskID)
			if err := os.MkdirAll(runDir, 0o755); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(runDir, "safe-evidence.bin")
			if err := os.WriteFile(source, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			input := uiReportFixture()
			input["artifacts"] = []any{map[string]any{
				"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "safe-evidence.bin")),
				"type": tt.artifactType, "description": "Safe metadata",
			}}
			result := runUIReport(t, workDir, taskID, input)
			if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
				t.Fatalf("result = %+v, want INVALID_INPUT", result)
			}
			for _, secret := range []string{"storage-secret", "trace-secret"} {
				err := filepath.WalkDir(runDir, func(path string, entry os.DirEntry, walkErr error) error {
					if walkErr != nil || entry.IsDir() || path == source {
						return walkErr
					}
					raw, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if strings.Contains(string(raw), secret) {
						t.Fatalf("%s contains rejected secret %q", path, secret)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUIReportAcceptsValidatedPNGScreenshot(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-valid-png"
	runDir := uiReportRunDir(workDir, taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := uiTestPNGFixture(t, color.RGBA{R: 0x91, G: 0x82, B: 0x73, A: 0xff})
	source := filepath.Join(runDir, "screen.png")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	input := uiReportFixture()
	input["scenarios"].([]any)[0].(map[string]any)["machine_data"] = map[string]any{
		"Name": "Authorization", "Value": "report-secret",
	}
	input["artifacts"] = []any{map[string]any{
		"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "screen.png")),
		"type": "screenshot", "description": "Validated screenshot",
	}}
	result := runUIReport(t, workDir, taskID, input)
	if result.Status != StatusOK {
		t.Fatalf("result = %+v", result)
	}
	sealed, err := os.ReadFile(filepath.Join(runDir, uiTestPublishedDir, "screen.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sealed, content) {
		t.Fatal("validated PNG changed while sealing")
	}
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
		if strings.Contains(string(raw), "report-secret") {
			t.Fatalf("%s contains report secret", name)
		}
	}
}

func TestUIReportRejectsAmbiguousStorageStateAliasesDeterministically(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-storage-alias"
	if result := runUIReport(t, workDir, taskID, uiReportFixture()); result.Status != StatusOK {
		t.Fatalf("prior publication = %+v", result)
	}
	runDir := uiReportRunDir(workDir, taskID)
	source := filepath.Join(runDir, "safe.json")
	content := []byte(`{"cookies":"wrong","Cookies":[],"origins":[{"origin":"http://127.0.0.1","localStorage":[{"name":"theme","value":"local-secret"}]}]}`)
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	wantState := snapshotUITransactionTree(t, runDir)
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "safe.json")),
		"type": "json", "description": "Safe name",
	}}
	var want Result
	for iteration := 0; iteration < 128; iteration++ {
		result := runUIReport(t, workDir, taskID, input)
		if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
			t.Fatalf("iteration %d result = %+v, want INVALID_INPUT", iteration, result)
		}
		if iteration == 0 {
			want = result
		} else if !reflect.DeepEqual(result, want) {
			t.Fatalf("iteration %d result = %#v, want %#v", iteration, result, want)
		}
		if state := snapshotUITransactionTree(t, runDir); !reflect.DeepEqual(state, wantState) {
			t.Fatalf("iteration %d changed prior publication", iteration)
		}
	}
}

func TestUIReportRejectsDuplicateEvidenceKeysDeterministically(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-duplicate-evidence-key"
	if result := runUIReport(t, workDir, taskID, uiReportFixture()); result.Status != StatusOK {
		t.Fatalf("prior publication = %+v", result)
	}
	runDir := uiReportRunDir(workDir, taskID)
	source := filepath.Join(runDir, "safe.json")
	content := []byte(`{"cookies":[],"cookies":"wrong","origins":[{"origin":"http://127.0.0.1","localStorage":[{"name":"theme","value":"local-secret"}]}]}`)
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	wantState := snapshotUITransactionTree(t, runDir)
	input := uiReportFixture()
	input["artifacts"] = []any{map[string]any{
		"path": filepath.ToSlash(filepath.Join(DefaultArtifactDir, "ui-test", taskID, "safe.json")),
		"type": "json", "description": "Safe name",
	}}
	var want Result
	for iteration := 0; iteration < 128; iteration++ {
		result := runUIReport(t, workDir, taskID, input)
		if result.Status != StatusError || result.ErrorCode != CodeInvalidInput {
			t.Fatalf("iteration %d result = %+v, want INVALID_INPUT", iteration, result)
		}
		if iteration == 0 {
			want = result
		} else if !reflect.DeepEqual(result, want) {
			t.Fatalf("iteration %d result = %#v, want %#v", iteration, result, want)
		}
		if state := snapshotUITransactionTree(t, runDir); !reflect.DeepEqual(state, wantState) {
			t.Fatalf("iteration %d changed prior publication", iteration)
		}
	}
}

func TestUITestEvidenceRejectsDuplicateObjectNamesRecursively(t *testing.T) {
	tests := []struct {
		name    string
		content string
		reject  bool
	}{
		{
			name:    "root cookies",
			content: `{"cookies":[],"cookies":"wrong","origins":[]}`,
			reject:  true,
		},
		{
			name:    "nested local storage",
			content: `{"origins":[{"localStorage":[],"localStorage":"wrong"}]}`,
			reject:  true,
		},
		{
			name:    "nested name",
			content: `{"entries":[{"name":"first","name":"second"}]}`,
			reject:  true,
		},
		{
			name:    "nested value in arrays",
			content: `{"rows":[[{"value":1,"value":2}]]}`,
			reject:  true,
		},
		{
			name:    "benign arrays and primitives",
			content: `[null,true,false,1,"text",[1,2],{"name":"duration","value":"42ms"}]`,
		},
		{
			name:    "unique nested objects",
			content: `{"cookies":"enabled","origins":"source","nested":{"name":"duration","value":"42ms"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validateErr := validateUITestEvidenceContent("json", []byte(test.content))
			_, redactErr := redactUITestEvidenceContent(uiTestEvidenceJSON, []byte(test.content))
			if test.reject {
				if validateErr == nil {
					t.Fatal("validation accepted duplicate object name")
				}
				if redactErr == nil {
					t.Fatal("redaction accepted duplicate object name")
				}
				return
			}
			if validateErr != nil {
				t.Fatalf("validation rejected valid JSON: %v", validateErr)
			}
			if redactErr != nil {
				t.Fatalf("redaction rejected valid JSON: %v", redactErr)
			}
		})
	}
}

func uiTestPNGFixture(t *testing.T, pixel color.RGBA) []byte {
	t.Helper()
	var out bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	picture.SetRGBA(0, 0, pixel)
	if err := png.Encode(&out, picture); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func uiTestZIPFixture(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
