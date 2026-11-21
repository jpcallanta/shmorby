//go:build windows

package execution

import (
	"syscall"
)

// ProcessGroupAlive reports whether the process with pid still
// exists. Windows has no process-group liveness probe in the
// standard library, so this checks the leader only.
func ProcessGroupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	h, err := syscall.OpenProcess(
		syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid),
	)
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)

	return true
}
