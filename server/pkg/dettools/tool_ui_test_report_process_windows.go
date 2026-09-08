//go:build windows

package dettools

import (
	"errors"

	"golang.org/x/sys/windows"
)

const uiTestWindowsStillActive = 259

func uiTestReportOwnerDead(pid int) (dead bool, definitive bool) {
	if pid <= 0 {
		return false, false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return true, true
		}
		return false, false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, false
	}
	return exitCode != uiTestWindowsStillActive, true
}
