//go:build uitest_integration && !windows

package uitest

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type integrationUnixProcess struct {
	parent   int
	identity integrationProcessIdentity
}

func integrationChromiumDescendantProcesses(
	rootPID int,
) ([]integrationProcessIdentity, error) {
	processes, err := integrationUnixProcessSnapshot()
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
	processes, err := integrationUnixProcessSnapshot()
	if err != nil {
		return integrationProcessIdentity{}, false, err
	}
	process, found := processes[pid]
	return process.identity, found, nil
}

func integrationUnixProcessSnapshot() (map[int]integrationUnixProcess, error) {
	output, err := exec.Command(
		"ps",
		"-axo",
		"pid=,ppid=,lstart=,stat=,comm=",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("list process tree: %w", err)
	}
	processes := make(map[int]integrationUnixProcess)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 || strings.Contains(fields[7], "Z") {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		if pidErr != nil || parentErr != nil {
			continue
		}
		processes[pid] = integrationUnixProcess{
			parent: parent,
			identity: integrationProcessIdentity{
				PID:        pid,
				StartedAt:  strings.Join(fields[2:7], " "),
				Executable: strings.Join(fields[8:], " "),
			},
		}
	}
	return processes, nil
}
