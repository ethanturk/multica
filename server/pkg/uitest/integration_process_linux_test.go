//go:build uitest_integration && linux

package uitest

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type integrationLinuxProcess struct {
	tree     integrationProcessTreeEntry
	identity integrationProcessIdentity
}

func integrationChromiumDescendantProcesses(
	root integrationProcessIdentity,
) ([]integrationProcessIdentity, error) {
	processes, err := integrationLinuxProcessSnapshot()
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
	process, found, err := integrationLinuxProcessByPID(pid)
	if err != nil {
		return integrationProcessIdentity{}, false, fmt.Errorf(
			"read /proc/%d/stat: %w",
			pid,
			err,
		)
	}
	return process.identity, found, nil
}

func integrationLinuxProcessSnapshot() (map[int]integrationLinuxProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("list /proc: %w", err)
	}
	processes := make(map[int]integrationLinuxProcess)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		process, found, err := integrationLinuxProcessByPID(pid)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
		}
		if !found {
			continue
		}
		processes[pid] = process
	}
	return processes, nil
}

func integrationLinuxProcessByPID(
	pid int,
) (integrationLinuxProcess, bool, error) {
	if pid <= 0 {
		return integrationLinuxProcess{}, false, nil
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		if os.IsNotExist(err) {
			return integrationLinuxProcess{}, false, nil
		}
		return integrationLinuxProcess{}, false, err
	}
	line := string(stat)
	nameStart := strings.IndexByte(line, '(')
	nameEnd := strings.LastIndexByte(line, ')')
	if nameStart < 1 || nameEnd <= nameStart {
		return integrationLinuxProcess{}, false, fmt.Errorf("malformed process stat")
	}
	readPID, err := strconv.Atoi(strings.TrimSpace(line[:nameStart]))
	if err != nil || readPID != pid {
		return integrationLinuxProcess{}, false, fmt.Errorf("malformed process PID")
	}
	fields := strings.Fields(line[nameEnd+1:])
	if len(fields) < 20 {
		return integrationLinuxProcess{}, false, fmt.Errorf("process stat has %d fields after comm", len(fields))
	}
	if fields[0] == "Z" {
		return integrationLinuxProcess{}, false, nil
	}
	parentPID, err := strconv.Atoi(fields[1])
	if err != nil {
		return integrationLinuxProcess{}, false, fmt.Errorf("parse parent PID: %w", err)
	}
	executable := line[nameStart+1 : nameEnd]
	return integrationLinuxProcess{
		tree: integrationProcessTreeEntry{
			PID:        pid,
			ParentPID:  parentPID,
			Executable: executable,
		},
		identity: integrationProcessIdentity{
			PID:        pid,
			BirthToken: "linux:" + fields[19],
			Executable: executable,
		},
	}, true, nil
}
