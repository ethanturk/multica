//go:build !windows

package uitest

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openFileShared(path string) (*os.File, error) {
	return os.Open(path)
}

func tryExclusiveFileLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return false, err
}

func unlockExclusiveFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func replaceFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
