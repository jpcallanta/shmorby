//go:build linux

package execution

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// ProcessIdentity returns a stable token for pid using the kernel
// boot id and the process start time. It returns "" when the process
// is gone or /proc is unavailable.
func ProcessIdentity(pid int) string {
	if pid <= 0 {
		return ""
	}

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}

	// Fields after the parenthesized command start at field 3
	// (state); field 22 is the start time in clock ticks.
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return ""
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return ""
	}

	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(boot)) + ":" + fields[19]
}
