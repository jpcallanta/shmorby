//go:build !windows && !linux

package execution

import (
	"os/exec"
	"strconv"
	"strings"
)

// ProcessIdentity returns a stable token for pid using the process
// start time reported by ps. It returns "" when the process is gone
// or the platform cannot report an identity.
func ProcessIdentity(pid int) string {
	if pid <= 0 {
		return ""
	}

	out, err := exec.Command(
		"ps", "-o", "lstart=", "-p", strconv.Itoa(pid),
	).Output()
	if err != nil {
		return ""
	}

	start := strings.TrimSpace(string(out))
	if start == "" {
		return ""
	}

	return "ps:" + start
}
