package tools

import (
	"context"
	"os/exec"
)

// Executor runs commands for testable tool implementations.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OSExecutor uses the real os/exec package.
type OSExecutor struct{}

// Runs a command with context and returns combined output.
func (OSExecutor) Run(
	ctx context.Context, name string, args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	return cmd.CombinedOutput()
}
