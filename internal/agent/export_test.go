package agent

// SetTerminalForTest overrides stdoutIsTerminal for testing.
// Returns a restore function.
func SetTerminalForTest(v bool) func() {
	old := stdoutIsTerminal.Load()
	stdoutIsTerminal.Store(v)
	return func() { stdoutIsTerminal.Store(old) }
}
