//go:build !windows

package uitest

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type platformProcessController struct {
	pgid int
}

func platformShellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-lc", command)
}

func newPlatformProcessController(cmd *exec.Cmd) (*platformProcessController, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &platformProcessController{}, nil
}

func (c *platformProcessController) attach(cmd *exec.Cmd) error {
	c.pgid = cmd.Process.Pid
	return nil
}

func (*platformProcessController) resume(*exec.Cmd) error { return nil }

func (c *platformProcessController) abort(_ *exec.Cmd, grace time.Duration) error {
	return c.terminate(grace)
}

func (c *platformProcessController) terminate(grace time.Duration) error {
	return terminateUnixProcessGroup(c.pgid, grace)
}

func (*platformProcessController) close() error { return nil }

func platformProcessGroup(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}

func terminateRecordedProcess(record processRecord, grace time.Duration) error {
	if err := verifyRecordedProcessIdentity(record); err != nil {
		return err
	}
	pgid := record.PGID
	if pgid == 0 {
		pgid = record.ChildPID
	}
	return terminateUnixProcessGroup(pgid, grace)
}

func terminateUnixProcessGroup(pgid int, grace time.Duration) error {
	if pgid <= 0 {
		return nil
	}
	err := syscall.Kill(-pgid, syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	deadline := time.Now().Add(grace)
	for processGroupAlive(pgid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !processGroupAlive(pgid) {
		return nil
	}
	err = syscall.Kill(-pgid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func processGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func platformProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func replaceFileAtomic(source, destination string) error {
	return os.Rename(source, destination)
}
