//go:build !windows

package execution

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// Starts a child in its own process group and kills it on cleanup.
func startGroupedSleep(t *testing.T) *os.Process {
	t.Helper()

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}

	t.Cleanup(func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	return cmd.Process
}
