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

func integrationChromiumDescendantPIDs(rootPID int) ([]int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot process tree: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	type process struct {
		parent int
		name   string
	}
	processes := make(map[int]process)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("enumerate process tree: %w", err)
	}
	for {
		processes[int(entry.ProcessID)] = process{
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
		name := strings.ToLower(processes[pid].name)
		if strings.Contains(name, "chrome") || strings.Contains(name, "chromium") {
			chromium = append(chromium, pid)
		}
	}
	sort.Ints(chromium)
	return chromium, nil
}
