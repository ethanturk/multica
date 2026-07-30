//go:build !windows

package dettools

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func openUITestEvidence(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}

func uiTestEvidenceLinkCount(_ *os.File, info os.FileInfo) (uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("evidence file identity is unavailable")
	}
	return uint64(stat.Nlink), nil
}
