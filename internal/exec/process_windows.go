//go:build windows

package exec

import (
	"os"
	osexec "os/exec"
	"strconv"
	"syscall"
)

// SetupProcessGroup configures a new process group for timeout
// isolation on Windows.
func SetupProcessGroup(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// KillProcessGroup terminates the process and its child tree on
// Windows using taskkill for tree-kill, with direct Kill as
// fallback.
func KillProcessGroup(pid int) {
	if pid <= 0 {
		return
	}

	// Try taskkill first to kill the entire tree. If it
	// succeeds, children are already gone; fallback to
	// direct Kill only when taskkill fails.
	if err := osexec.Command(
		"taskkill", "/T", "/F",
		"/PID", strconv.Itoa(pid),
	).Run(); err == nil {
		return
	}

	if p, err := os.FindProcess(pid); err == nil && p != nil {
		_ = p.Kill()
	}
}
