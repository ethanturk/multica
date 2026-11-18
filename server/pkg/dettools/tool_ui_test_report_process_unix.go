//go:build !windows

package dettools

import (
	"errors"

	"golang.org/x/sys/unix"
)

func uiTestReportOwnerDead(pid int) (dead bool, definitive bool) {
	if pid <= 0 {
		return false, false
	}
	err := unix.Kill(pid, 0)
	switch {
	case err == nil:
		return false, true
	case errors.Is(err, unix.ESRCH):
		return true, true
	case errors.Is(err, unix.EPERM):
		return false, true
	default:
		return false, false
	}
}
