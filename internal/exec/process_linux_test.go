//go:build linux

package exec

import (
	"os/exec"
	"testing"
)

// TestSetupProcessGroup_NoPdeathsig verifies children get their own
// process group without a parent-death signal. Pdeathsig follows the
// creating thread, not the process, so it can fire while shmorby is
// still alive (go.dev/issue/27505).
func TestSetupProcessGroup_NoPdeathsig(t *testing.T) {
	cmd := exec.Command("true")

	SetupProcessGroup(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid = false, want true")
	}
	if cmd.SysProcAttr.Pdeathsig != 0 {
		t.Errorf("Pdeathsig = %v, want 0",
			cmd.SysProcAttr.Pdeathsig)
	}
}
