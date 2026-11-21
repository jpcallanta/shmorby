//go:build !windows

package execution

import (
	"errors"
	"syscall"
)

// ProcessGroupAlive reports whether the process group led by pid
// still has at least one live member.
func ProcessGroupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(-pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}
