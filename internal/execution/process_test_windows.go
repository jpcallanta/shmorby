//go:build windows

package execution

import (
	"os"
	"testing"
)

// Skips process-group tests where Windows has no POSIX group probe.
func startGroupedSleep(t *testing.T) *os.Process {
	t.Helper()
	t.Skip("process-group liveness is Unix-only")

	return nil
}
