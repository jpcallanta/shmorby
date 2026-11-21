//go:build windows

package execution

import (
	"os"
	"testing"
)

// TestProcessIdentity_CurrentProcess verifies Windows reports a
// stable identity for a live process.
func TestProcessIdentity_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	first := ProcessIdentity(pid)
	if first == "" {
		t.Fatal("ProcessIdentity returned empty for current process")
	}
	if second := ProcessIdentity(pid); second != first {
		t.Errorf("identity changed: want %q, got %q", first, second)
	}
	if got := ProcessIdentity(0); got != "" {
		t.Errorf("ProcessIdentity(0) = %q, want empty", got)
	}
}
