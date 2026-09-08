//go:build windows

package uitest

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type platformProcessController struct {
	job windows.Handle
}

type uiTestJobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func platformShellCommand(command string) *exec.Cmd {
	return exec.Command("cmd.exe", "/d", "/s", "/c", command)
}

func newPlatformProcessController(cmd *exec.Cmd) (*platformProcessController, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("set KILL_ON_JOB_CLOSE: %w", err)
	}
	return &platformProcessController{job: job}, nil
}

func (c *platformProcessController) attach(cmd *exec.Cmd) error {
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open child process: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(c.job, process); err != nil {
		return fmt.Errorf("assign child to job object: %w", err)
	}
	return nil
}

func (*platformProcessController) resume(cmd *exec.Cmd) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot child threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("enumerate child threads: %w", err)
	}
	resumed := 0
	for {
		if entry.OwnerProcessID == uint32(cmd.Process.Pid) {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return fmt.Errorf("open suspended child thread: %w", err)
			}
			_, resumeErr := windows.ResumeThread(thread)
			closeErr := windows.CloseHandle(thread)
			if resumeErr != nil || closeErr != nil {
				var threadErr error
				if resumeErr != nil {
					threadErr = errors.Join(threadErr, fmt.Errorf("resume suspended child thread: %w", resumeErr))
				}
				if closeErr != nil {
					threadErr = errors.Join(threadErr, fmt.Errorf("close child thread handle: %w", closeErr))
				}
				return threadErr
			}
			resumed++
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return fmt.Errorf("enumerate child threads: %w", err)
		}
	}
	if resumed == 0 {
		return fmt.Errorf("no suspended thread found for process %d", cmd.Process.Pid)
	}
	return nil
}

func (c *platformProcessController) abort(cmd *exec.Cmd, grace time.Duration) error {
	var abortErr error
	if err := c.terminate(grace); err != nil {
		abortErr = errors.Join(abortErr, err)
	}
	if cmd != nil && cmd.Process != nil && platformProcessAlive(cmd.Process.Pid) {
		if err := cmd.Process.Kill(); err != nil {
			abortErr = errors.Join(abortErr, fmt.Errorf("kill unowned child process: %w", err))
		}
	}
	return abortErr
}

func (c *platformProcessController) terminate(grace time.Duration) error {
	if c.job == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(c.job, 1); err != nil {
		active, queryErr := c.activeProcesses()
		if queryErr != nil || active != 0 {
			return err
		}
	}
	deadline := time.Now().Add(grace)
	for {
		active, err := c.activeProcesses()
		if err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job object still has %d active processes", active)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *platformProcessController) activeProcesses() (uint32, error) {
	var info uiTestJobAccounting
	if err := windows.QueryInformationJobObject(
		c.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); err != nil {
		return 0, fmt.Errorf("query job object members: %w", err)
	}
	return info.ActiveProcesses, nil
}

func (c *platformProcessController) close() error {
	if c.job == 0 {
		return nil
	}
	active, err := c.activeProcesses()
	if err != nil {
		return err
	}
	if active != 0 {
		return fmt.Errorf("refuse to close job object with %d active processes", active)
	}
	if err := windows.CloseHandle(c.job); err != nil {
		return err
	}
	c.job = 0
	return nil
}

func platformProcessGroup(_ *exec.Cmd) int { return 0 }

func terminateRecordedProcess(record processRecord, grace time.Duration) error {
	if err := verifyRecordedProcessIdentity(record); err != nil {
		return err
	}
	if record.ChildPID <= 0 {
		return nil
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
		false,
		uint32(record.ChildPID),
	)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.TerminateProcess(process, 1); err != nil && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}
	timeout := uint32(grace / time.Millisecond)
	result, err := windows.WaitForSingleObject(process, timeout)
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("process %d did not exit", record.ChildPID)
	}
	return nil
}

func platformProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	result, err := windows.WaitForSingleObject(process, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
}

func replaceFileAtomic(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePtr,
		destinationPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
