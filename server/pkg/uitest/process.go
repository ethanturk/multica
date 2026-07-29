package uitest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const processShutdownGrace = 5 * time.Second

type processRecord struct {
	TaskID    string    `json:"task_id"`
	ProxyPID  int       `json:"proxy_pid"`
	ChildPID  int       `json:"child_pid"`
	PGID      int       `json:"pgid"`
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"started_at"`
}

type managedProcess struct {
	cmd        *exec.Cmd
	controller *platformProcessController
	record     processRecord
	registry   *processRegistry

	terminateOnce sync.Once
	terminateErr  error
	done          chan struct{}
	waitErr       error
}

type processRegistry struct {
	mu        sync.Mutex
	taskID    string
	stateDir  string
	statePath string
	logger    *slog.Logger
	processes map[int]*managedProcess
	closed    bool
}

var liveProcessRegistries sync.Map

func newProcessRegistry(workDir, taskID string, logger *slog.Logger) (*processRegistry, error) {
	stateDir := filepath.Join(workDir, ".multica", "ui-test", taskID)
	registry := &processRegistry{
		taskID: taskID, stateDir: stateDir,
		statePath: filepath.Join(stateDir, "processes.json"),
		logger:    logger, processes: make(map[int]*managedProcess),
	}
	if _, loaded := liveProcessRegistries.LoadOrStore(stateDir, registry); loaded {
		return nil, fmt.Errorf("UI test task %q already has a live session", taskID)
	}
	return registry, nil
}

func startManagedProcess(registry *processRegistry, kind, command, workDir string, env []string, logPath string) (*managedProcess, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, fmt.Errorf("create %s log directory: %w", kind, err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s log: %w", kind, err)
	}
	if err := logFile.Chmod(0o600); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("restrict %s log: %w", kind, err)
	}

	cmd := platformShellCommand(command)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	controller, err := newPlatformProcessController(cmd)
	if err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("create %s process owner: %w", kind, err)
	}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		_ = controller.close()
		return nil, fmt.Errorf("start %s command: %w", kind, err)
	}
	_ = logFile.Close()
	if err := controller.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = controller.terminate(processShutdownGrace)
		_ = controller.close()
		return nil, fmt.Errorf("own %s process tree: %w", kind, err)
	}

	process := &managedProcess{
		cmd: cmd, controller: controller, registry: registry, done: make(chan struct{}),
		record: processRecord{
			TaskID: taskIDForRegistry(registry), ProxyPID: os.Getpid(),
			ChildPID: cmd.Process.Pid, PGID: platformProcessGroup(cmd),
			Kind: kind, StartedAt: time.Now().UTC(),
		},
	}
	if err := registry.add(process); err != nil {
		if terminateErr := controller.terminate(processShutdownGrace); terminateErr != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		_ = controller.close()
		return nil, fmt.Errorf("persist %s process ownership: %w", kind, err)
	}
	go process.reap()
	return process, nil
}

func taskIDForRegistry(registry *processRegistry) string {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.taskID
}

func (p *managedProcess) reap() {
	p.waitErr = p.cmd.Wait()
	p.terminate()
	closeErr := p.controller.close()
	if closeErr != nil {
		p.terminateErr = errors.Join(p.terminateErr, closeErr)
	}
	p.registry.remove(p.record.ChildPID)
	close(p.done)
}

func (p *managedProcess) terminate() error {
	p.terminateOnce.Do(func() {
		p.terminateErr = p.controller.terminate(processShutdownGrace)
	})
	return p.terminateErr
}

func (p *managedProcess) stop() error {
	terminateErr := p.terminate()
	if terminateErr != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	<-p.done
	return errors.Join(terminateErr, p.terminateErr)
}

func (p *managedProcess) result() error {
	<-p.done
	return errors.Join(p.waitErr, p.terminateErr)
}

func (r *processRegistry) add(process *managedProcess) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("process registry is closed")
	}
	r.processes[process.record.ChildPID] = process
	if err := r.writeLocked(); err != nil {
		delete(r.processes, process.record.ChildPID)
		return err
	}
	return nil
}

func (r *processRegistry) remove(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.processes, pid)
	if r.closed {
		return
	}
	if err := r.writeLocked(); err != nil && r.logger != nil {
		r.logger.Warn("ui-test: update process metadata failed", "error", err)
	}
}

func (r *processRegistry) writeLocked() error {
	records := make([]processRecord, 0, len(r.processes))
	for _, process := range r.processes {
		records = append(records, process.record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ChildPID < records[j].ChildPID })
	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("encode process metadata: %w", err)
	}
	data = append(data, '\n')
	return writeAtomic0600(r.statePath, data)
}

func (r *processRegistry) cleanup() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	processes := make([]*managedProcess, 0, len(r.processes))
	for _, process := range r.processes {
		processes = append(processes, process)
	}
	r.mu.Unlock()

	var waitGroup sync.WaitGroup
	errs := make(chan error, len(processes))
	for _, process := range processes {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := process.stop(); err != nil {
				errs <- err
			}
		}()
	}
	waitGroup.Wait()
	close(errs)

	var cleanupErr error
	for err := range errs {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if cleanupErr == nil {
		if err := os.RemoveAll(r.stateDir); err != nil {
			cleanupErr = fmt.Errorf("remove process metadata: %w", err)
		}
	}
	liveProcessRegistries.Delete(r.stateDir)
	return cleanupErr
}

func CleanupTask(workDir, taskID string, logger *slog.Logger) error {
	absoluteWorkDir, safeTaskID, err := validateTaskLocation(workDir, taskID)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(absoluteWorkDir, ".multica", "ui-test", safeTaskID)
	if value, ok := liveProcessRegistries.Load(stateDir); ok {
		return value.(*processRegistry).cleanup()
	}

	statePath := filepath.Join(stateDir, "processes.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read UI test process metadata: %w", err)
	}
	var records []processRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("decode UI test process metadata: %w", err)
	}
	var cleanupErr error
	for _, record := range records {
		if record.TaskID != safeTaskID {
			if logger != nil {
				logger.Warn("ui-test: skipped process metadata for another task", "record_task_id", record.TaskID)
			}
			continue
		}
		if err := terminateRecordedProcess(record, processShutdownGrace); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("terminate %s process %d: %w", record.Kind, record.ChildPID, err))
		}
	}
	if cleanupErr == nil {
		if err := os.RemoveAll(stateDir); err != nil {
			cleanupErr = fmt.Errorf("remove UI test process metadata: %w", err)
		}
	}
	return cleanupErr
}

func validateTaskLocation(workDir, taskID string) (string, string, error) {
	if taskID == "" || taskID == "." || taskID == ".." ||
		filepath.IsAbs(taskID) || filepath.Base(taskID) != taskID {
		return "", "", fmt.Errorf("invalid UI test task ID")
	}
	absoluteWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve UI test workdir: %w", err)
	}
	info, err := os.Stat(absoluteWorkDir)
	if err != nil {
		return "", "", fmt.Errorf("stat UI test workdir: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("UI test workdir is not a directory")
	}
	return absoluteWorkDir, taskID, nil
}

func writeAtomic0600(path string, data []byte) (returnErr error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create private state directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		if returnErr != nil {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict temporary state: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := replaceFileAtomic(tempPath, path); err != nil {
		return fmt.Errorf("publish private state: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1024))
	_ = body.Close()
}
