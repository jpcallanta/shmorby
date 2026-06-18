package tui

import (
	"strings"
	"testing"
	"time"
)

// Tests that WriteToken accumulates partial lines.
func TestStreamBuffer_WriteToken_Partial(t *testing.T) {
	b := NewStreamBuffer()
	lines := b.WriteToken("hello")
	if len(lines) != 0 {
		t.Errorf("want 0 flushed lines, got %d", len(lines))
	}
	if b.Tokens() != 5 {
		t.Errorf("want 5 tokens, got %d", b.Tokens())
	}
}

// Tests that WriteToken flushes on newline.
func TestStreamBuffer_WriteToken_Newline(t *testing.T) {
	b := NewStreamBuffer()
	lines := b.WriteToken("hello\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 flushed line, got %d", len(lines))
	}
	if lines[0] != "hello" {
		t.Errorf("want %q, got %q", "hello", lines[0])
	}
}

// Tests that WriteToken flushes multiple lines.
func TestStreamBuffer_WriteToken_MultipleLines(t *testing.T) {
	b := NewStreamBuffer()
	lines := b.WriteToken("line1\nline2\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 flushed lines, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("want %q, got %q", "line1", lines[0])
	}
	if lines[1] != "line2" {
		t.Errorf("want %q, got %q", "line2", lines[1])
	}
}

// Tests Flush returns remaining partial line.
func TestStreamBuffer_Flush(t *testing.T) {
	b := NewStreamBuffer()
	b.WriteToken("partial")
	remaining := b.Flush()
	if remaining != "partial" {
		t.Errorf("want %q, got %q", "partial", remaining)
	}
}

// Tests Flush returns empty when nothing to flush.
func TestStreamBuffer_Flush_Empty(t *testing.T) {
	b := NewStreamBuffer()
	remaining := b.Flush()
	if remaining != "" {
		t.Errorf("want empty, got %q", remaining)
	}
}

// Tests code block detection.
func TestStreamBuffer_CodeBlock(t *testing.T) {
	b := NewStreamBuffer()
	b.WriteToken("```\n")
	if !b.inCodeBlock {
		t.Error("should enter code block after opening fence")
	}
	b.WriteToken("code line\n")
	b.WriteToken("```\n")
	if b.inCodeBlock {
		t.Error("should exit code block after closing fence")
	}
}

// Tests that token count is accurate.
func TestStreamBuffer_TokenCount(t *testing.T) {
	b := NewStreamBuffer()
	b.WriteToken("a")
	b.WriteToken("b")
	b.WriteToken("c\n")
	// "a" + "b" + "c\n" = 1+1+2 = 4 bytes
	if b.Tokens() != 4 {
		t.Errorf("want 4 tokens, got %d", b.Tokens())
	}
}

// Tests streamDeltaMsg appends to output incrementally.
func TestModelUpdate_StreamDelta(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.streamBuf = NewStreamBuffer()

	updated, _ := m.Update(streamDeltaMsg{delta: "hello"})
	m = updated.(Model)

	if m.tokensDown != 5 {
		t.Errorf("want 5 tokens, got %d", m.tokensDown)
	}
	if len(m.output) != 0 {
		t.Errorf("want 0 output (partial), got %d", len(m.output))
	}
}

// Tests streamDeltaMsg flushes on newline.
func TestModelUpdate_StreamDelta_Newline(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.streamBuf = NewStreamBuffer()

	updated, _ := m.Update(streamDeltaMsg{delta: "line\n"})
	m = updated.(Model)

	if len(m.output) != 1 {
		t.Fatalf("want 1 output, got %d", len(m.output))
	}
	if m.output[0].text != "line" {
		t.Errorf("want %q, got %q", "line", m.output[0].text)
	}
}

// Tests streamDoneMsg flushes remaining and stops spinner.
func TestModelUpdate_StreamDone(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.running = true
	m.streamBuf = NewStreamBuffer()
	m.streamBuf.WriteToken("partial")
	m.streamBuf.SetLastDelta(time.Now().Add(-100 * time.Millisecond))
	m.spinner.Start("thinking…")

	updated, _ := m.Update(streamDoneMsg{})
	m = updated.(Model)

	if m.running {
		t.Error("should not be running after stream done")
	}
	if len(m.output) != 1 {
		t.Fatalf("want 1 output, got %d", len(m.output))
	}
	if !strings.Contains(m.output[0].text, "partial") {
		t.Errorf("output missing expected text, got %q", m.output[0].text)
	}
}

// Tests multiple deltas build up correctly.
func TestModelUpdate_StreamMultipleDeltas(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.streamBuf = NewStreamBuffer()

	updated, _ := m.Update(streamDeltaMsg{delta: "hel"})
	m = updated.(Model)
	updated, _ = m.Update(streamDeltaMsg{delta: "lo\n"})
	m = updated.(Model)

	// "hel" + "lo\n" = 3+3 = 6 bytes
	if m.tokensDown != 6 {
		t.Errorf("want 6 tokens, got %d", m.tokensDown)
	}
	if len(m.output) != 1 {
		t.Fatalf("want 1 output, got %d", len(m.output))
	}
	if m.output[0].text != "hello" {
		t.Errorf("want %q, got %q", "hello", m.output[0].text)
	}
}

// Tests that streamDoneMsg with empty buffer doesn't add empty entry.
func TestModelUpdate_StreamDone_Empty(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.streamBuf = NewStreamBuffer()
	m.spinner.Start("thinking…")

	updated, _ := m.Update(streamDoneMsg{})
	m = updated.(Model)

	if len(m.output) != 0 {
		t.Errorf("want 0 output (nothing to flush), got %d", len(m.output))
	}
}
