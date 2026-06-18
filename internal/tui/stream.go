package tui

import (
	"strings"
	"time"
)

// StreamBuffer accumulates streaming tokens with line-level
// buffering and code block detection for anti-flicker rendering.
type StreamBuffer struct {
	current     string
	inCodeBlock bool
	tokens      int
	lastDelta   time.Time
}

// NewStreamBuffer creates a ready-to-use buffer.
func NewStreamBuffer() StreamBuffer {
	return StreamBuffer{}
}

// WriteToken appends a token and returns any complete lines.
// Complete lines are flushed; partial lines stay buffered.
func (b *StreamBuffer) WriteToken(token string) []string {
	b.current += token
	b.tokens += len(token)
	b.lastDelta = time.Now()

	var flushed []string
	for {
		line, ok := b.nextLine()
		if !ok {
			break
		}
		flushed = append(flushed, line)

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			b.inCodeBlock = !b.inCodeBlock
		}
	}
	return flushed
}

// Flush forces any remaining partial line out.
func (b *StreamBuffer) Flush() string {
	remaining := b.current
	if remaining == "" {
		return ""
	}
	b.current = ""
	return remaining
}

// nextLine extracts and consumes the next complete line (up to \n).
// Returns ("", false) when no newline is found.
func (b *StreamBuffer) nextLine() (string, bool) {
	idx := strings.Index(b.current, "\n")
	if idx < 0 {
		return "", false
	}
	line := b.current[:idx]
	// Advance past the newline.
	b.current = b.current[idx+1:]
	return line, true
}

// Tokens returns the total number of bytes written.
func (b *StreamBuffer) Tokens() int {
	return b.tokens
}

// LastDelta returns the time of the last WriteToken call.
func (b *StreamBuffer) LastDelta() time.Time {
	return b.lastDelta
}

// SettleElapsed returns how long since the last delta was received.
func (b *StreamBuffer) SettleElapsed() time.Duration {
	return time.Since(b.lastDelta)
}

// SetLastDelta sets the last delta time (used in tests to simulate settle).
func (b *StreamBuffer) SetLastDelta(t time.Time) {
	b.lastDelta = t
}
