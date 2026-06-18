// Package tui implements the Bubbletea-based terminal UI.
package tui

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// LogEntry is a single log record sent from TUILogHandler to the viewport.
type LogEntry struct {
	Level   slog.Level
	Time    time.Time
	Message string
	Attrs   []slog.Attr
}

// TUILogHandler wraps slog.Handler so records are written to stderr
// (via the inner handler) and optionally sent to the TUI viewport
// via a channel.
type TUILogHandler struct {
	inner slog.Handler
	logs  chan<- LogEntry
	level *atomic.Int32
}

// NewTUILogHandler creates a handler that writes to inner (stderr) and
// sends formatted entries on logs when non-nil.
func NewTUILogHandler(
	inner slog.Handler,
	logs chan<- LogEntry,
) *TUILogHandler {
	h := &TUILogHandler{
		inner: inner,
		logs:  logs,
		level: new(atomic.Int32),
	}
	h.level.Store(int32(slog.LevelInfo))
	return h
}

// SetLevel updates the minimum log level at runtime.
func (h *TUILogHandler) SetLevel(l slog.Level) {
	h.level.Store(int32(l))
}

// Enabled returns true when l is at or above the current threshold.
func (h *TUILogHandler) Enabled(
	ctx context.Context, l slog.Level,
) bool {
	return l >= slog.Level(h.level.Load()) &&
		h.inner.Enabled(ctx, l)
}

// Handle passes the record to the inner handler and, when the TUI
// channel is connected, sends a LogEntry.
func (h *TUILogHandler) Handle(
	ctx context.Context, r slog.Record,
) error {
	// Only write to inner handler (stderr) when no TUI channel
	// is connected (headless / --no-tui mode).
	if h.logs == nil && h.inner.Enabled(ctx, r.Level) {
		if err := h.inner.Handle(ctx, r); err != nil {
			return err
		}
	}

	if h.logs != nil {
		entry := LogEntry{
			Level:   r.Level,
			Time:    r.Time,
			Message: r.Message,
		}
		r.Attrs(func(a slog.Attr) bool {
			entry.Attrs = append(entry.Attrs, a)
			return true
		})
		select {
		case h.logs <- entry:
		default:
			// Drop rather than block when channel is full.
		}
	}
	return nil
}

// WithAttrs returns a new handler with additional attributes.
func (h *TUILogHandler) WithAttrs(
	attrs []slog.Attr,
) slog.Handler {
	return &TUILogHandler{
		inner: h.inner.WithAttrs(attrs),
		logs:  h.logs,
		level: h.level,
	}
}

// WithGroup returns a new handler with the given group name.
func (h *TUILogHandler) WithGroup(name string) slog.Handler {
	return &TUILogHandler{
		inner: h.inner.WithGroup(name),
		logs:  h.logs,
		level: h.level,
	}
}

// ThinkingBuffer accumulates reasoning tokens emitted by the LLM
// during a streaming response.
type ThinkingBuffer struct {
	lines  []string
	tokens int
	start  time.Time
	active bool
}

// Start begins a new thinking block.
func (b *ThinkingBuffer) Start() {
	b.lines = nil
	b.tokens = 0
	b.start = time.Now()
	b.active = true
}

// AddDelta appends a reasoning delta and counts tokens.
func (b *ThinkingBuffer) AddDelta(delta string) {
	if !b.active {
		b.Start()
	}
	b.tokens += len(delta)
	b.lines = append(b.lines, delta)
}

// End finalises the block and returns whether any content was collected.
func (b *ThinkingBuffer) End() bool {
	wasActive := b.active
	b.active = false
	return wasActive && len(b.lines) > 0
}

// Text returns the accumulated reasoning content.
func (b *ThinkingBuffer) Text() string {
	var s string
	for _, l := range b.lines {
		s += l
	}
	return s
}

// Elapsed returns the duration since the block started.
func (b *ThinkingBuffer) Elapsed() time.Duration {
	return time.Since(b.start)
}

// Tokens returns the total character count (approximate token count).
func (b *ThinkingBuffer) Tokens() int {
	return b.tokens
}

// Active reports whether the buffer is currently collecting.
func (b *ThinkingBuffer) Active() bool {
	return b.active
}

// Lines returns the accumulated lines.
func (b *ThinkingBuffer) Lines() []string {
	return b.lines
}
