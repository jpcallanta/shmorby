//go:build windows

package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// acquireLock opens the lock file and takes an exclusive
// LockFileEx lock. The handle from os.OpenFile is synchronous
// (not OVERLAPPED), so LockFileEx blocks until granted. The
// entire file is locked (offset 0, length ^uint32(0) for both
// low/high) to match Unix flock whole-file semantics.
func acquireLock(dir string) (*os.File, error) {

	path := filepath.Join(dir, lockFile)

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {

		return nil, fmt.Errorf("open lock file: %w", err)
	}

	ol := new(windows.Overlapped)

	const allBytes = ^uint32(0)

	err = windows.LockFileEx(
		windows.Handle(fd.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK, 0, allBytes, allBytes, ol,
	)
	if err != nil {

		fd.Close()

		return nil, fmt.Errorf("LockFileEx: %w", err)
	}

	return fd, nil
}

// releaseLock unlocks and closes the lock file.
func releaseLock(fd *os.File) error {

	ol := new(windows.Overlapped)

	const allBytes = ^uint32(0)

	var errs []error

	if err := windows.UnlockFileEx(
		windows.Handle(fd.Fd()), 0, allBytes, allBytes, ol,
	); err != nil {

		errs = append(errs, fmt.Errorf("UnlockFileEx: %w", err))
	}

	if err := fd.Close(); err != nil {

		errs = append(errs, fmt.Errorf("close lock: %w", err))
	}

	return errors.Join(errs...)
}
