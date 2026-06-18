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

// OffloadedMessage represents a message that was offloaded from the session
// to make room for new context.
type OffloadedMessage struct {
	ID         string
	SessionID  string
	Role       string
	Content    string
	Tokens     int
	Timestamp  time.Time
	Compressed bool
}
