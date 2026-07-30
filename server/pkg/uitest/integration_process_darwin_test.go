//go:build uitest_integration && darwin

package uitest

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const integrationDarwinZombieState = 5

type integrationDarwinProcess struct {
	tree     integrationProcessTreeEntry
	identity integrationProcessIdentity
}

func integrationChromiumDescendantProcesses(
	root integrationProcessIdentity,
) ([]integrationProcessIdentity, error) {
	processes, err := integrationDarwinProcessSnapshot()
	if err != nil {
		return nil, err
	}
	rootProcess, found := processes[root.PID]
	if !found || rootProcess.identity.BirthToken != root.BirthToken {
		return nil, fmt.Errorf("browser owner PID %d changed before process snapshot", root.PID)
	}
	descendants := map[int]bool{root.PID: true}
	for changed := true; changed; {
		changed = false
		for pid, process := range processes {
			if !descendants[pid] && descendants[process.tree.ParentPID] {
				descendants[pid] = true
				changed = true
			}
		}
	}
	var chromium []integrationProcessIdentity
	for pid := range descendants {
		if pid == root.PID {
			continue
		}
		identity := processes[pid].identity
		executable := strings.ToLower(identity.Executable)
		if strings.Contains(executable, "chromium") ||
			strings.Contains(executable, "chrome") {
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
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) ||
			errors.Is(err, unix.ENOENT) ||
			errors.Is(err, unix.EIO) {
			return integrationProcessIdentity{}, false, nil
		}
		return integrationProcessIdentity{}, false, fmt.Errorf("read process identity: %w", err)
	}
	parsed, found := integrationDarwinProcessFromKinfo(process)
	return parsed.identity, found, nil
}

func integrationDarwinProcessSnapshot() (map[int]integrationDarwinProcess, error) {
	entries, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("snapshot process tree: %w", err)
	}
	processes := make(map[int]integrationDarwinProcess, len(entries))
	for _, entry := range entries {
		process, found := integrationDarwinProcessFromKinfo(&entry)
		if found {
			processes[process.identity.PID] = process
		}
	}
	return processes, nil
}

func integrationDarwinProcessFromKinfo(
	process *unix.KinfoProc,
) (integrationDarwinProcess, bool) {
	if process == nil ||
		process.Proc.P_pid <= 0 ||
		process.Proc.P_stat == integrationDarwinZombieState {
		return integrationDarwinProcess{}, false
	}
	pid := int(process.Proc.P_pid)
	executable := strings.TrimRight(string(process.Proc.P_comm[:]), "\x00")
	return integrationDarwinProcess{
		tree: integrationProcessTreeEntry{
			PID:        pid,
			ParentPID:  int(process.Eproc.Ppid),
			Executable: executable,
		},
		identity: integrationProcessIdentity{
			PID: pid,
			BirthToken: fmt.Sprintf(
				"darwin:%d:%d",
				process.Proc.P_starttime.Sec,
				process.Proc.P_starttime.Usec,
			),
			Executable: executable,
		},
	}, true
}
