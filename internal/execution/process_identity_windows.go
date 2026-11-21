//go:build windows

package execution

import (
	"fmt"
	"syscall"
)

// ProcessIdentity returns a stable token for pid using the process
// creation time. It returns "" when the process is gone or cannot be
// queried.
func ProcessIdentity(pid int) string {
	if pid <= 0 {
		return ""
	}

	h, err := syscall.OpenProcess(
		syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid),
	)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)

	var (
		creation syscall.Filetime
		exit     syscall.Filetime
		kernel   syscall.Filetime
		user     syscall.Filetime
	)
	if err := syscall.GetProcessTimes(
		h, &creation, &exit, &kernel, &user,
	); err != nil {
		return ""
	}

	return fmt.Sprintf("win:%d", creation.Nanoseconds())
}
