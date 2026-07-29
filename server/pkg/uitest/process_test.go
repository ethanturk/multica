package uitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUITestHelperProcess(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index < 0 || index >= len(os.Args) {
		return
	}
	args := os.Args[index:]
	switch args[0] {
	case "descendant":
		mustWriteHelperFile(args[1], strconv.Itoa(os.Getpid()))
		waitForever()
	case "app":
		runAppHelper(args[1:])
	case "setup":
		runSetupHelper(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q", args[0])
		os.Exit(2)
	}
}

func runAppHelper(args []string) {
	address, events, descendantPID, behavior := args[0], args[1], args[2], args[3]
	if descendantPID != "-" {
		child := exec.Command(os.Args[0], "-test.run=^TestUITestHelperProcess$", "--", "descendant", descendantPID)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	appendHelperEvent(events, "app")
	fmt.Fprintln(os.Stdout, "app evidence")
	switch behavior {
	case "fail":
		waitHelperFile(descendantPID, 2*time.Second)
		os.Exit(7)
	case "no-health":
		waitForever()
	case "healthy":
		serveHelper(address, events, "")
	case "redirect":
		serveHelper(address, events, args[4])
	default:
		fmt.Fprintln(os.Stderr, "unknown app behavior")
		os.Exit(2)
	}
}

func waitForever() {
	for {
		time.Sleep(time.Hour)
	}
}

func serveHelper(address, events, redirect string) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		appendHelperEvent(events, "health")
		if redirect != "" {
			w.Header().Set("Location", redirect)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := http.Serve(listener, handler); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func runSetupHelper(args []string) {
	events, behavior := args[0], args[1]
	appendHelperEvent(events, "setup")
	statePath := os.Getenv("MULTICA_UI_TEST_STORAGE_STATE")
	if statePath == "" ||
		os.Getenv("MULTICA_UI_TEST_BASE_URL") == "" ||
		os.Getenv("MULTICA_UI_TEST_ARTIFACT_DIR") == "" ||
		os.Getenv("MULTICA_UI_TEST_TASK_ID") == "" {
		fmt.Fprintln(os.Stderr, "missing setup environment")
		os.Exit(8)
	}
	switch behavior {
	case "success":
		mustWriteHelperFile(statePath, `{"cookies":[],"origins":[]}`)
	case "external-cookie":
		mustWriteHelperFile(statePath, `{"cookies":[{"name":"token","value":"secret","domain":"example.com","path":"/","expires":-1,"httpOnly":true,"secure":false,"sameSite":"Lax"}],"origins":[]}`)
	case "fail":
		fmt.Fprintln(os.Stderr, "setup failed")
		os.Exit(9)
	default:
		fmt.Fprintln(os.Stderr, "unknown setup behavior")
		os.Exit(2)
	}
}

func mustWriteHelperFile(path, value string) {
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func appendHelperEvent(path, value string) {
	if path == "-" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_, writeErr := fmt.Fprintln(file, value)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "write event:", writeErr, closeErr)
		os.Exit(2)
	}
}

func waitHelperFile(path string, limit time.Duration) {
	if path == "-" {
		return
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "timed out waiting for helper file")
	os.Exit(2)
}

func TestProcessStateIsPrivateAndContainsNoCommandOrEnvironment(t *testing.T) {
	fixture := newSessionFixture(t, "process-state", "healthy", "", 2*time.Second, 0)
	if err := fixture.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}

	statePath := filepath.Join(fixture.workDir, ".multica", "ui-test", fixture.taskID, "processes.json")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat process state: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("process state mode = %04o, want 0600", got)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read process state: %v", err)
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode process state: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("process records = %d, want 1", len(records))
	}
	allowed := map[string]bool{
		"task_id": true, "proxy_pid": true, "child_pid": true,
		"pgid": true, "kind": true, "started_at": true,
	}
	for key := range records[0] {
		if !allowed[key] {
			t.Fatalf("process state leaked unexpected field %q", key)
		}
	}
	for key := range allowed {
		if _, ok := records[0][key]; !ok {
			t.Fatalf("process state missing %q", key)
		}
	}
}

func TestCleanupTaskKillsOnlyExactTaskAndPreservesEvidence(t *testing.T) {
	first := newSessionFixture(t, "cleanup-a", "healthy", "", 2*time.Second, 0)
	second := newSessionFixtureAtWorkDir(t, first.workDir, "cleanup-b", "healthy", "", 2*time.Second, 0)
	if err := first.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("first EnsureReady() error = %v", err)
	}
	if err := second.session.EnsureReady(context.Background()); err != nil {
		t.Fatalf("second EnsureReady() error = %v", err)
	}
	firstPID := readHelperPID(t, first.descendantPID)
	secondPID := readHelperPID(t, second.descendantPID)

	if err := CleanupTask(first.workDir, first.taskID, nil); err != nil {
		t.Fatalf("CleanupTask() error = %v", err)
	}
	waitProcessGone(t, firstPID, 7*time.Second)
	if !platformProcessAlive(secondPID) {
		t.Fatal("CleanupTask killed another task's descendant")
	}
	if _, err := os.Stat(filepath.Join(first.artifactDir, "app.log")); err != nil {
		t.Fatalf("app evidence was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first.workDir, ".multica", "ui-test", first.taskID)); !os.IsNotExist(err) {
		t.Fatalf("task process state remains: %v", err)
	}
}

type sessionFixture struct {
	session       *Session
	workDir       string
	taskID        string
	artifactDir   string
	events        string
	descendantPID string
}

func newSessionFixture(t *testing.T, taskID, behavior, setup string, startupLimit, sessionLimit time.Duration) sessionFixture {
	t.Helper()
	return newSessionFixtureAtWorkDir(t, t.TempDir(), taskID, behavior, setup, startupLimit, sessionLimit)
}

func newSessionFixtureAtWorkDir(t *testing.T, workDir, taskID, behavior, setup string, startupLimit, sessionLimit time.Duration) sessionFixture {
	t.Helper()
	address := freeLoopbackAddress(t)
	events := filepath.Join(workDir, taskID+"-events")
	descendantPID := filepath.Join(workDir, taskID+"-descendant.pid")
	artifactDir := filepath.Join(workDir, ".multica", "artifacts", "ui-test", taskID)
	baseURL := "http://" + address
	config, err := loadManifest([]byte(fmt.Sprintf(
		`{"start":%q,"url":%q,"health":"/health","setup":%q}`,
		helperCommand("app", address, events, descendantPID, behavior),
		baseURL,
		setup,
	)))
	if err != nil {
		t.Fatalf("load test manifest: %v", err)
	}
	session, err := NewSession(SessionOptions{
		WorkDir:      workDir,
		TaskID:       taskID,
		Config:       config,
		ArtifactDir:  artifactDir,
		StartupLimit: startupLimit,
		SetupLimit:   5 * time.Second,
		SessionLimit: sessionLimit,
	}, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return sessionFixture{
		session: session, workDir: workDir, taskID: taskID,
		artifactDir: artifactDir, events: events, descendantPID: descendantPID,
	}
}

func helperCommand(args ...string) string {
	parts := []string{commandQuote(os.Args[0]), "-test.run=^TestUITestHelperProcess$", "--"}
	for _, arg := range args {
		parts = append(parts, commandQuote(arg))
	}
	return strings.Join(parts, " ")
}

func commandQuote(value string) string {
	if runtime.GOOS == "windows" {
		return strconv.Quote(value)
	}
	return "'" + strings.ReplaceAll(value, "'", `'\"'\"'`) + "'"
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return address
}

func readHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse helper PID: %v", parseErr)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("read helper PID: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitProcessGone(t *testing.T, pid int, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for platformProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if platformProcessAlive(pid) {
		t.Fatalf("process %d remains alive", pid)
	}
}

func readEvents(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return strings.Fields(string(data))
}

func countEvent(events []string, target string) int {
	count := 0
	for _, event := range events {
		if event == target {
			count++
		}
	}
	return count
}
