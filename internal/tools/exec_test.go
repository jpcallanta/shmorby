package tools

import (
	"context"
)

// mockExecutor records the last command run and returns canned output.
type mockExecutor struct {
	Name   string
	Args   []string
	Out    []byte
	RunErr error
}

// Records the call and returns canned results.
func (m *mockExecutor) Run(
	_ context.Context, name string, args ...string,
) ([]byte, error) {
	m.Name = name
	m.Args = append([]string{}, args...)

	return m.Out, m.RunErr
}
