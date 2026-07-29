//go:build uitest_integration && windows

package uitest

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type integrationWindowsProcess struct {
	parent int
	name   string
}

func integrationChromiumDescendantProcesses(
	rootPID int,
) ([]integrationProcessIdentity, error) {
	processes, err := integrationWindowsProcessSnapshot()
	if err != nil {
		return nil, err
	}
	descendants := map[int]bool{rootPID: true}
	for changed := true; changed; {
		changed = false
		for pid, process := range processes {
			if !descendants[pid] && descendants[process.parent] {
				descendants[pid] = true
				changed = true
			}
		}
	}
	var chromium []integrationProcessIdentity
	for pid := range descendants {
		if pid == rootPID {
			continue
		}
		name := strings.ToLower(processes[pid].name)
		if !strings.Contains(name, "chrome") && !strings.Contains(name, "chromium") {
			continue
		}
		identity, found, identityErr := integrationProcessIdentityByPID(pid)
		if identityErr != nil {
			return nil, fmt.Errorf("read Chromium process %d identity: %w", pid, identityErr)
		}
		if found {
			chromium = append(chromium, identity)
		}
	}
	sort.Slice(chromium, func(i, j int) bool {
		return chromium[i].PID < chromium[j].PID
	})
	return chromium, nil
}

func integrationProcessIdentityByPID(
	pid int,
) (integrationProcessIdentity, bool, error) {
	if pid <= 0 {
		return integrationProcessIdentity{}, false, nil
	}
	process, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid),
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return integrationProcessIdentity{}, false, nil
		}
		return integrationProcessIdentity{}, false, fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(process)
	wait, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		return integrationProcessIdentity{}, false, fmt.Errorf("check process state: %w", err)
	}
	if wait != uint32(windows.WAIT_TIMEOUT) {
		return integrationProcessIdentity{}, false, nil
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(
		process,
		&creation,
		&exit,
		&kernel,
		&user,
	); err != nil {
		return integrationProcessIdentity{}, false, fmt.Errorf("read process creation time: %w", err)
	}
	executable := make([]uint16, 32768)
	size := uint32(len(executable))
	if err := windows.QueryFullProcessImageName(
		process,
		0,
		&executable[0],
		&size,
	); err != nil {
		return integrationProcessIdentity{}, false, fmt.Errorf("read process executable: %w", err)
	}
	return integrationProcessIdentity{
		PID:        pid,
		StartedAt:  fmt.Sprintf("%08x%08x", creation.HighDateTime, creation.LowDateTime),
		Executable: windows.UTF16ToString(executable[:size]),
	}, true, nil
}

func integrationWindowsProcessSnapshot() (map[int]integrationWindowsProcess, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot process tree: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	processes := make(map[int]integrationWindowsProcess)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("enumerate process tree: %w", err)
	}
	for {
		processes[int(entry.ProcessID)] = integrationWindowsProcess{
			parent: int(entry.ParentProcessID),
			name:   windows.UTF16ToString(entry.ExeFile[:]),
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("enumerate process tree: %w", err)
		}
	}
	return processes, nil
}
