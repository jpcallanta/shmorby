//go:build !windows

package exec

import (
	osexec "os/exec"
	"syscall"
)

// SetupProcessGroup configures a new process group for timeout
// isolation on Unix systems. Parent-death signals are intentionally
// not used: Go delivers Pdeathsig when the creating thread exits,
// which can kill long-running children while shmorby is alive
// (go.dev/issue/27505). The execution reconciler detects survivors
// instead.
func SetupProcessGroup(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup terminates the process and its entire child
// tree on Unix by sending SIGKILL to the negative PID (process
// group).
func KillProcessGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}
