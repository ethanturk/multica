package uitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageStateWritesAndValidatesPrivateEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "storage-state.json")
	if err := writeEmptyStorageState(path); err != nil {
		t.Fatalf("writeEmptyStorageState() error = %v", err)
	}
	if err := validateStorageState(path); err != nil {
		t.Fatalf("validateStorageState() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read storage state: %v", err)
	}
	if got := string(data); got != `{"cookies":[],"origins":[]}`+"\n" {
		t.Fatalf("empty state = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat storage state: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("storage state mode = %04o, want 0600", got)
	}
}

func TestStorageStateAcceptsLoopbackCookiesAndOrigins(t *testing.T) {
	path := writeStorageFixture(t, `{
		"cookies":[{
			"name":"session","value":"secret","domain":".localhost","path":"/",
			"expires":-1,"httpOnly":true,"secure":false,"sameSite":"Lax"
		}],
		"origins":[{
			"origin":"http://127.0.0.1:3000",
			"localStorage":[{"name":"multica:chat:isOpen","value":"true"}]
		}]
	}`)
	if err := validateStorageState(path); err != nil {
		t.Fatalf("validateStorageState() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat storage state: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("storage state mode = %04o, want 0600", got)
	}
}

func TestStorageStateRejectsUnsafeShapeAndDestinations(t *testing.T) {
	cookie := `"name":"session","value":"secret","domain":"localhost","path":"/","expires":-1,"httpOnly":true,"secure":false,"sameSite":"Lax"`
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "external cookie", data: `{"cookies":[{` + strings.Replace(cookie, `"localhost"`, `"example.com"`, 1) + `}],"origins":[]}`},
		{name: "cookie port", data: `{"cookies":[{` + strings.Replace(cookie, `"localhost"`, `"localhost:3000"`, 1) + `}],"origins":[]}`},
		{name: "external origin", data: `{"cookies":[],"origins":[{"origin":"https://example.com","localStorage":[]}]}`},
		{name: "origin credentials", data: `{"cookies":[],"origins":[{"origin":"http://user:pass@localhost:3000","localStorage":[]}]}`},
		{name: "authorization field", data: `{"cookies":[],"origins":[],"authorization":"Bearer secret"}`},
		{name: "credentials field", data: `{"cookies":[],"origins":[{"origin":"http://localhost:3000","localStorage":[],"credentials":"include"}]}`},
		{name: "cookie unknown field", data: `{"cookies":[{` + cookie + `,"partitionKey":"secret"}],"origins":[]}`},
		{name: "null cookie", data: `{"cookies":[null],"origins":[]}`},
		{name: "cookie missing value", data: `{"cookies":[{"name":"session","domain":"localhost","path":"/","expires":-1,"httpOnly":true,"secure":false,"sameSite":"Lax"}],"origins":[]}`},
		{name: "cookie missing expires", data: `{"cookies":[{"name":"session","value":"secret","domain":"localhost","path":"/","httpOnly":true,"secure":false,"sameSite":"Lax"}],"origins":[]}`},
		{name: "cookie missing httpOnly", data: `{"cookies":[{"name":"session","value":"secret","domain":"localhost","path":"/","expires":-1,"secure":false,"sameSite":"Lax"}],"origins":[]}`},
		{name: "cookie missing secure", data: `{"cookies":[{"name":"session","value":"secret","domain":"localhost","path":"/","expires":-1,"httpOnly":true,"sameSite":"Lax"}],"origins":[]}`},
		{name: "null local storage entry", data: `{"cookies":[],"origins":[{"origin":"http://localhost:3000","localStorage":[null]}]}`},
		{name: "local storage missing name", data: `{"cookies":[],"origins":[{"origin":"http://localhost:3000","localStorage":[{"value":"y"}]}]}`},
		{name: "local storage missing value", data: `{"cookies":[],"origins":[{"origin":"http://localhost:3000","localStorage":[{"name":"x"}]}]}`},
		{name: "local storage unknown field", data: `{"cookies":[],"origins":[{"origin":"http://localhost:3000","localStorage":[{"name":"x","value":"y","authorization":"z"}]}]}`},
		{name: "malformed", data: `{"cookies":[]`},
		{name: "multiple values", data: `{"cookies":[],"origins":[]} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeStorageFixture(t, test.data)
			if err := validateStorageState(path); err == nil {
				t.Fatal("validateStorageState() succeeded")
			}
		})
	}
}

func TestStorageStateRejectsOversizeAndSymlink(t *testing.T) {
	path := writeStorageFixture(t, strings.Repeat("x", maxStorageStateBytes+1))
	if err := validateStorageState(path); err == nil {
		t.Fatal("oversize storage state accepted")
	}

	target := writeStorageFixture(t, `{"cookies":[],"origins":[]}`)
	link := filepath.Join(t.TempDir(), "storage-state.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := validateStorageState(link); err == nil {
		t.Fatal("symlink storage state accepted")
	}
}

func writeStorageFixture(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "storage-state.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write storage fixture: %v", err)
	}
	return path
}
