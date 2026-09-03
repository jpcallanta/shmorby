//go:build unix

package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireLock opens the lock file and takes an exclusive flock.
func acquireLock(dir string) (*os.File, error) {

	path := filepath.Join(dir, lockFile)

	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {

		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := unix.Flock(int(fd.Fd()), unix.LOCK_EX); err != nil {

		fd.Close()

		return nil, fmt.Errorf("flock: %w", err)
	}

	return fd, nil
}

// releaseLock unlocks and closes the lock file.
func releaseLock(fd *os.File) error {

	var errs []error

	if err := unix.Flock(int(fd.Fd()), unix.LOCK_UN); err != nil {

		errs = append(errs, fmt.Errorf("unlock: %w", err))
	}

	if err := fd.Close(); err != nil {

		errs = append(errs, fmt.Errorf("close lock: %w", err))
	}

	return errors.Join(errs...)
}
