//go:build windows

package tools

import (
	"os/exec"
)

func setupProcessGroup(cmd *exec.Cmd) {
	// process-group isolation not supported on Windows
}

func killProcessGroup(pid int) {
	// process-group isolation not supported on Windows
}
