//go:build uitest_integration && !windows

package uitest

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func integrationChromiumDescendantPIDs(rootPID int) ([]int, error) {
	output, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("list process tree: %w", err)
	}
	type process struct {
		parent  int
		command string
	}
	processes := make(map[int]process)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		if pidErr != nil || parentErr != nil {
			continue
		}
		processes[pid] = process{
			parent:  parent,
			command: strings.Join(fields[2:], " "),
		}
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
	var chromium []int
	for pid := range descendants {
		if pid == rootPID || !platformProcessAlive(pid) {
			continue
		}
		command := strings.ToLower(processes[pid].command)
		if strings.Contains(command, "chromium") ||
			strings.Contains(command, "chrome") {
			chromium = append(chromium, pid)
		}
	}
	sort.Ints(chromium)
	return chromium, nil
}
