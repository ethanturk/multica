package uitest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultViewportWidth  = 1440
	defaultViewportHeight = 900
	manifestPath          = ".multica/ui-test.json"
)

type Manifest struct {
	Start    string    `json:"start"`
	URL      string    `json:"url"`
	Health   string    `json:"health"`
	Setup    string    `json:"setup,omitempty"`
	Viewport *Viewport `json:"viewport,omitempty"`
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ResolvedConfig struct {
	StartCommand string
	BaseURL      *url.URL
	HealthURL    *url.URL
	SetupCommand string
	Viewport     Viewport
	Source       string
}

func LoadConfig(workDir string) (ResolvedConfig, error) {
	data, err := os.ReadFile(filepath.Join(workDir, manifestPath))
	if err == nil {
		return loadManifest(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ResolvedConfig{}, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	return inferConfig(workDir)
}

func loadManifest(data []byte) (ResolvedConfig, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return ResolvedConfig{}, fmt.Errorf("decode %s: %w", manifestPath, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ResolvedConfig{}, fmt.Errorf("decode %s: multiple JSON values", manifestPath)
		}
		return ResolvedConfig{}, fmt.Errorf("decode %s: %w", manifestPath, err)
	}

	manifest.Start = strings.TrimSpace(manifest.Start)
	manifest.URL = strings.TrimSpace(manifest.URL)
	manifest.Health = strings.TrimSpace(manifest.Health)
	manifest.Setup = strings.TrimSpace(manifest.Setup)
	if manifest.Start == "" || manifest.URL == "" || manifest.Health == "" {
		return ResolvedConfig{}, fmt.Errorf("%s requires start, url, and health", manifestPath)
	}

	baseURL, err := ValidateLoopbackURL(manifest.URL)
	if err != nil {
		return ResolvedConfig{}, err
	}
	healthURL, err := resolveHealthURL(baseURL, manifest.Health)
	if err != nil {
		return ResolvedConfig{}, err
	}
	viewport := Viewport{Width: defaultViewportWidth, Height: defaultViewportHeight}
	if manifest.Viewport != nil {
		viewport = *manifest.Viewport
	}
	if err := validateViewport(viewport); err != nil {
		return ResolvedConfig{}, err
	}

	return ResolvedConfig{
		StartCommand: manifest.Start,
		BaseURL:      baseURL,
		HealthURL:    healthURL,
		SetupCommand: manifest.Setup,
		Viewport:     viewport,
		Source:       "manifest",
	}, nil
}

func resolveHealthURL(baseURL *url.URL, raw string) (*url.URL, error) {
	if !strings.HasPrefix(raw, "/") {
		return nil, fmt.Errorf("UI test health must be a relative path")
	}
	health, err := url.Parse(raw)
	if err != nil || health.Scheme != "" || health.Host != "" || health.User != nil {
		return nil, fmt.Errorf("UI test health must be a relative path")
	}
	return baseURL.ResolveReference(health), nil
}

func validateViewport(viewport Viewport) error {
	if viewport.Width < 320 || viewport.Width > 3840 || viewport.Height < 320 || viewport.Height > 2160 {
		return fmt.Errorf("UI test viewport must be 320-3840 by 320-2160")
	}
	return nil
}

type packageJSON struct {
	PackageManager string            `json:"packageManager"`
	Scripts        map[string]string `json:"scripts"`
}

func inferConfig(workDir string) (ResolvedConfig, error) {
	if !hasExactlyOnePlaywrightConfig(workDir) {
		return ResolvedConfig{}, inferenceError()
	}
	data, err := os.ReadFile(filepath.Join(workDir, "package.json"))
	if err != nil {
		return ResolvedConfig{}, inferenceError()
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ResolvedConfig{}, inferenceError()
	}
	manager, ok := inferPackageManager(workDir, pkg.PackageManager)
	if !ok {
		return ResolvedConfig{}, inferenceError()
	}
	script := "dev:web"
	if strings.TrimSpace(pkg.Scripts[script]) == "" {
		script = "dev"
	}
	if strings.TrimSpace(pkg.Scripts[script]) == "" {
		return ResolvedConfig{}, inferenceError()
	}
	baseURL, _ := url.Parse("http://127.0.0.1:3000")
	healthURL, _ := url.Parse("http://127.0.0.1:3000/")
	return ResolvedConfig{
		StartCommand: managerCommand(manager, script),
		BaseURL:      baseURL,
		HealthURL:    healthURL,
		Viewport:     Viewport{Width: defaultViewportWidth, Height: defaultViewportHeight},
		Source:       "inferred",
	}, nil
}

func hasExactlyOnePlaywrightConfig(workDir string) bool {
	count := 0
	for _, extension := range []string{"ts", "js", "mts", "mjs", "cts", "cjs"} {
		info, err := os.Stat(filepath.Join(workDir, "playwright.config."+extension))
		if err == nil && !info.IsDir() {
			count++
		}
	}
	return count == 1
}

func inferPackageManager(workDir, declared string) (string, bool) {
	manager, _, _ := strings.Cut(strings.TrimSpace(declared), "@")
	lockFiles := map[string][]string{
		"pnpm": {"pnpm-lock.yaml"},
		"npm":  {"package-lock.json"},
		"yarn": {"yarn.lock"},
		"bun":  {"bun.lock", "bun.lockb"},
	}
	if _, ok := lockFiles[manager]; !ok {
		return "", false
	}
	matched := ""
	for candidate, names := range lockFiles {
		for _, name := range names {
			if _, err := os.Stat(filepath.Join(workDir, name)); err == nil {
				if matched != "" && matched != candidate {
					return "", false
				}
				matched = candidate
			}
		}
	}
	return manager, matched == manager
}

func managerCommand(manager, script string) string {
	if manager == "npm" || manager == "bun" {
		return manager + " run " + script
	}
	return manager + " " + script
}

func inferenceError() error {
	return fmt.Errorf("cannot infer UI test configuration; add %s", manifestPath)
}
