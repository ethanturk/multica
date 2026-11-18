package dettools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	uiTestReportLockDir       = ".ui-test-report.lock"
	uiTestReportLockOwnerName = "owner.json"
	uiTestReportLockPoll      = 10 * time.Millisecond
)

type uiTestReportLockOwner struct {
	PID       int       `json:"pid"`
	Token     string    `json:"token"`
	StartedAt time.Time `json:"started_at"`
}

type uiTestReportLock struct {
	root    *os.Root
	owner   uiTestReportLockOwner
	release sync.Once
	err     error
}

func acquireUITestReportLock(ctx context.Context, root *os.Root, minimumStaleAge time.Duration) (*uiTestReportLock, error) {
	if minimumStaleAge < time.Minute {
		minimumStaleAge = time.Minute
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := uiTestTempSuffix()
		if err != nil {
			return nil, err
		}
		owner := uiTestReportLockOwner{PID: os.Getpid(), Token: token, StartedAt: time.Now().UTC()}
		claimed, err := claimUITestReportLock(root, owner)
		if err != nil {
			return nil, err
		}
		if claimed {
			return &uiTestReportLock{root: root, owner: owner}, nil
		}
		recovered, err := recoverStaleUITestReportLock(root, minimumStaleAge, token)
		if err != nil {
			return nil, err
		}
		if recovered {
			continue
		}
		timer := time.NewTimer(uiTestReportLockPoll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func claimUITestReportLock(root *os.Root, owner uiTestReportLockOwner) (bool, error) {
	claimDir := ".ui-test-report.claim-" + owner.Token
	if err := root.Mkdir(claimDir, 0o700); err != nil {
		return false, fmt.Errorf("create UI test report lock claim: %w", err)
	}
	cleanupClaim := func() error {
		return removeUITestLockDirectory(root, claimDir)
	}
	if err := writeUITestLockOwner(root, claimDir, owner); err != nil {
		return false, errors.Join(err, cleanupClaim())
	}
	if err := root.Rename(claimDir, uiTestReportLockDir); err != nil {
		cleanupErr := cleanupClaim()
		if errors.Is(err, os.ErrExist) {
			return false, cleanupErr
		}
		if _, statErr := root.Lstat(uiTestReportLockDir); statErr == nil {
			return false, cleanupErr
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, errors.Join(fmt.Errorf("inspect UI test report lock: %w", statErr), cleanupErr)
		}
		return false, errors.Join(fmt.Errorf("claim UI test report lock: %w", err), cleanupErr)
	}
	return true, nil
}

func recoverStaleUITestReportLock(root *os.Root, minimumStaleAge time.Duration, recoveryToken string) (bool, error) {
	owner, err := readUITestLockOwner(root, uiTestReportLockDir)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, nil
	}
	age := time.Since(owner.StartedAt)
	if owner.StartedAt.IsZero() || age < minimumStaleAge {
		return false, nil
	}
	dead, definitive := uiTestReportOwnerDead(owner.PID)
	if !definitive || !dead {
		return false, nil
	}

	quarantine := ".ui-test-report.stale-" + recoveryToken
	if err := root.Rename(uiTestReportLockDir, quarantine); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, nil
	}
	quarantinedOwner, err := readUITestLockOwner(root, quarantine)
	if err != nil || quarantinedOwner.Token != owner.Token {
		restoreErr := root.Rename(quarantine, uiTestReportLockDir)
		return false, errors.Join(fmt.Errorf("UI test report lock ownership changed during stale recovery"), err, restoreErr)
	}
	if err := removeUITestLockDirectory(root, quarantine); err != nil {
		return false, fmt.Errorf("remove stale UI test report lock: %w", err)
	}
	return true, nil
}

func (lock *uiTestReportLock) Release() error {
	lock.release.Do(func() {
		current, err := readUITestLockOwner(lock.root, uiTestReportLockDir)
		if err != nil {
			lock.err = fmt.Errorf("read UI test report lock owner during release: %w", err)
			return
		}
		if current.Token != lock.owner.Token {
			lock.err = fmt.Errorf("UI test report lock ownership changed before release")
			return
		}
		releasedDir := ".ui-test-report.release-" + lock.owner.Token
		if err := lock.root.Rename(uiTestReportLockDir, releasedDir); err != nil {
			lock.err = fmt.Errorf("quarantine UI test report lock for release: %w", err)
			return
		}
		current, err = readUITestLockOwner(lock.root, releasedDir)
		if err != nil || current.Token != lock.owner.Token {
			restoreErr := lock.root.Rename(releasedDir, uiTestReportLockDir)
			lock.err = errors.Join(fmt.Errorf("UI test report lock ownership changed during release"), err, restoreErr)
			return
		}
		if err := removeUITestLockDirectory(lock.root, releasedDir); err != nil {
			lock.err = fmt.Errorf("remove released UI test report lock: %w", err)
		}
	})
	return lock.err
}

func writeUITestLockOwner(root *os.Root, dir string, owner uiTestReportLockOwner) error {
	raw, err := json.Marshal(owner)
	if err != nil {
		return fmt.Errorf("encode UI test report lock owner: %w", err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(dir, uiTestReportLockOwnerName)
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create UI test report lock owner: %w", err)
	}
	written, writeErr := file.Write(raw)
	if writeErr == nil && written != len(raw) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write UI test report lock owner: %w", err)
	}
	return nil
}

func readUITestLockOwner(root *os.Root, dir string) (uiTestReportLockOwner, error) {
	file, err := root.Open(filepath.Join(dir, uiTestReportLockOwnerName))
	if err != nil {
		return uiTestReportLockOwner{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return uiTestReportLockOwner{}, err
	}
	if len(raw) > 4096 {
		return uiTestReportLockOwner{}, fmt.Errorf("UI test report lock owner is too large")
	}
	var owner uiTestReportLockOwner
	if err := json.Unmarshal(raw, &owner); err != nil {
		return uiTestReportLockOwner{}, err
	}
	if owner.PID <= 0 || owner.Token == "" || owner.StartedAt.IsZero() {
		return uiTestReportLockOwner{}, fmt.Errorf("UI test report lock owner is invalid")
	}
	return owner, nil
}

func removeUITestLockDirectory(root *os.Root, dir string) error {
	ownerErr := root.Remove(filepath.Join(dir, uiTestReportLockOwnerName))
	if errors.Is(ownerErr, os.ErrNotExist) {
		ownerErr = nil
	}
	dirErr := root.Remove(dir)
	if errors.Is(dirErr, os.ErrNotExist) {
		dirErr = nil
	}
	return errors.Join(ownerErr, dirErr)
}
