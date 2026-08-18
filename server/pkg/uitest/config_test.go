package uitest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		files      map[string]string
		wantErr    bool
		wantSource string
		wantStart  string
		wantBase   string
		wantHealth string
		wantSetup  string
		wantWidth  int
		wantHeight int
	}{
		{
			name: "approved manifest",
			files: map[string]string{
				".multica/ui-test.json": `{"start":" pnpm dev:web ","url":" http://localhost:3000 ","health":" /ready ","setup":" pnpm seed "}`,
			},
			wantSource: "manifest", wantStart: "pnpm dev:web", wantBase: "http://localhost:3000", wantHealth: "http://localhost:3000/ready", wantSetup: "pnpm seed", wantWidth: 1440, wantHeight: 900,
		},
		{
			name: "manifest viewport override",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"http://127.0.0.1:3000","health":"/","viewport":{"width":1280,"height":720}}`,
			},
			wantSource: "manifest", wantStart: "pnpm dev:web", wantBase: "http://127.0.0.1:3000", wantHealth: "http://127.0.0.1:3000/", wantWidth: 1280, wantHeight: 720,
		},
		{
			name: "missing start",
			files: map[string]string{
				".multica/ui-test.json": `{"url":"http://localhost:3000","health":"/"}`,
			},
			wantErr: true,
		},
		{
			name: "missing url",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","health":"/"}`,
			},
			wantErr: true,
		},
		{
			name: "unknown manifest field",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"http://localhost:3000","health":"/","extra":true}`,
			},
			wantErr: true,
		},
		{
			name: "malformed manifest",
			files: map[string]string{
				".multica/ui-test.json": `{`,
			},
			wantErr: true,
		},
		{
			name: "reject unsafe urls and health values",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"ftp://localhost:3000","health":"/"}`,
			},
			wantErr: true,
		},
		{
			name: "reject health scheme",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"http://localhost:3000","health":"http://localhost:3000/ready"}`,
			},
			wantErr: true,
		},
		{
			name: "reject health host",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"http://localhost:3000","health":"//example.com/ready"}`,
			},
			wantErr: true,
		},
		{
			name: "reject non relative health",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"http://localhost:3000","health":"ready"}`,
			},
			wantErr: true,
		},
		{
			name: "reject invalid viewport",
			files: map[string]string{
				".multica/ui-test.json": `{"start":"pnpm dev:web","url":"http://localhost:3000","health":"/","viewport":{"width":319,"height":2161}}`,
			},
			wantErr: true,
		},
		{
			name: "infer pnpm dev web",
			files: map[string]string{
				"playwright.config.ts": "export default {}",
				"package.json":         `{"packageManager":"pnpm@10.0.0","scripts":{"dev:web":"next dev"}}`,
				"pnpm-lock.yaml":       "lockfileVersion: '9.0'",
			},
			wantSource: "inferred", wantStart: "pnpm dev:web", wantBase: "http://127.0.0.1:3000", wantHealth: "http://127.0.0.1:3000/", wantWidth: 1440, wantHeight: 900,
		},
		{
			name: "infer npm dev fallback",
			files: map[string]string{
				"playwright.config.js": "module.exports = {}",
				"package.json":         `{"packageManager":"npm@11.0.0","scripts":{"dev":"next dev"}}`,
				"package-lock.json":    `{}`,
			},
			wantSource: "inferred", wantStart: "npm run dev", wantBase: "http://127.0.0.1:3000", wantHealth: "http://127.0.0.1:3000/", wantWidth: 1440, wantHeight: 900,
		},
		{
			name: "no manifest inference when managers ambiguous",
			files: map[string]string{
				"playwright.config.ts": "export default {}",
				"package.json":         `{"scripts":{"dev":"next dev"}}`,
				"pnpm-lock.yaml":       "lockfileVersion: '9.0'",
				"yarn.lock":            "",
			},
			wantErr: true,
		},
		{
			name: "no manifest inference without start script",
			files: map[string]string{
				"playwright.config.ts": "export default {}",
				"package.json":         `{"packageManager":"pnpm@10.0.0","scripts":{}}`,
				"pnpm-lock.yaml":       "lockfileVersion: '9.0'",
			},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			for path, content := range test.files {
				writeTestFile(t, workDir, path, content)
			}

			got, err := LoadConfig(workDir)
			if test.wantErr {
				if err == nil {
					t.Fatalf("LoadConfig() succeeded with %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if got.Source != test.wantSource || got.StartCommand != test.wantStart || got.BaseURL.String() != test.wantBase || got.HealthURL.String() != test.wantHealth || got.SetupCommand != test.wantSetup || got.Viewport.Width != test.wantWidth || got.Viewport.Height != test.wantHeight {
				t.Fatalf("LoadConfig() = %+v, want source=%q start=%q base=%q health=%q setup=%q viewport=%dx%d", got, test.wantSource, test.wantStart, test.wantBase, test.wantHealth, test.wantSetup, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func writeTestFile(t *testing.T, workDir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(workDir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
