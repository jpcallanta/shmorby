package memory

import "time"

// MemoryEntry represents a single stored memory of a tool execution.
type MemoryEntry struct {
	ID        string
	Timestamp time.Time
	SessionID string
	Tool      string
	Command   string
	Args      string
	Result    string
	ExitCode  int
	Summary   string
	Tags      []string
}
