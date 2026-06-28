package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shmorby/internal/session"
)

// TestFormatMemoryContext_Empty returns empty string.
func TestFormatMemoryContext_Empty(t *testing.T) {
	got := FormatMemoryContext(nil)

	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// TestFormatMemoryContext_SingleEntry formats one entry.
func TestFormatMemoryContext_SingleEntry(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "systemctl restart nginx",
			ExitCode:  0,
		},
	}

	got := FormatMemoryContext(entries)

	if !strings.Contains(got, "2026-06-10") {
		t.Error("missing timestamp")
	}
	if !strings.Contains(got, "shell") {
		t.Error("missing tool")
	}
	if !strings.Contains(got, "systemctl restart nginx") {
		t.Error("missing command")
	}
	if !strings.Contains(got, "success") {
		t.Error("missing success status")
	}
	if !strings.Contains(got, "Relevant past actions") {
		t.Error("missing header")
	}
}

// TestFormatMemoryContext_ExitCodeShowsFailure formats non-zero exit.
func TestFormatMemoryContext_ExitCodeShowsFailure(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "rm /etc/passwd",
			ExitCode:  1,
		},
	}

	got := FormatMemoryContext(entries)

	if !strings.Contains(got, "exit 1") {
		t.Errorf("want exit status, got %q", got)
	}
}

// TestFormatMemoryContext_TagsAppended shows tags in parentheses.
func TestFormatMemoryContext_TagsAppended(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "ssh",
			Command:   "ssh admin@web01",
			ExitCode:  0,
			Tags:      []string{"host:web01"},
		},
	}

	got := FormatMemoryContext(entries)

	if !strings.Contains(got, "host:web01") {
		t.Errorf("want tag, got %q", got)
	}
}

// TestFormatMemoryContext_MultipleEntries formats all entries.
func TestFormatMemoryContext_MultipleEntries(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "systemctl restart nginx",
			ExitCode:  0,
		},
		{
			Timestamp: time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
			Tool:      "ssh",
			Command:   "apt update",
			ExitCode:  0,
		},
	}

	got := FormatMemoryContext(entries)

	if !strings.Contains(got, "nginx") {
		t.Error("missing first entry")
	}
	if !strings.Contains(got, "apt update") {
		t.Error("missing second entry")
	}
	// Context footer should appear once.
	if !strings.Contains(got, "Use this context") {
		t.Error("missing context footer")
	}
}

// TestInjectMemoryContext_EmptyReturnsOriginal handles empty context.
func TestInjectMemoryContext_EmptyReturnsOriginal(t *testing.T) {
	msgs := []session.Message{
		{Role: "user", Content: "hello"},
	}

	result := InjectMemoryContext(msgs, "")

	if len(result) != 1 {
		t.Fatalf("want 1 message, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("want user role, got %q", result[0].Role)
	}
}

// TestInjectMemoryContext_InsertsBeforeUser inserts before first user.
func TestInjectMemoryContext_InsertsBeforeUser(t *testing.T) {
	msgs := []session.Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "another"},
	}

	result := InjectMemoryContext(msgs, "memory context")

	if len(result) != 5 {
		t.Fatalf("want 5 messages, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "you are helpful" {
		t.Error("first message unchanged")
	}
	if result[1].Role != "system" || result[1].Content != "memory context" {
		t.Errorf("injected at index 1, got role=%q content=%q",
			result[1].Role, result[1].Content)
	}
	if result[2].Role != "user" || result[2].Content != "list files" {
		t.Error("original first user shifted by one")
	}
}

// TestInjectMemoryContext_NoUserMessage appends at end.
func TestInjectMemoryContext_NoUserMessage(t *testing.T) {
	msgs := []session.Message{
		{Role: "assistant", Content: "hello"},
	}

	result := InjectMemoryContext(msgs, "ctx")

	if len(result) != 2 {
		t.Fatalf("want 2 messages, got %d", len(result))
	}
	// When no user exists, appended at end.
	if result[1].Role != "system" {
		t.Errorf("appended at end, got role=%q", result[1].Role)
	}
}

// TestInjectMemoryContext_NilInput handles nil slice.
func TestInjectMemoryContext_NilInput(t *testing.T) {
	result := InjectMemoryContext(nil, "ctx")

	if len(result) != 1 {
		t.Fatalf("want 1 message, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "ctx" {
		t.Errorf("want system/ctx, got %s/%s", result[0].Role, result[0].Content)
	}
}

// TestCaptureToolResult_StoresEntry inserts an entry.
func TestCaptureToolResult_StoresEntry(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "mem.db")
	store, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	CaptureToolResult(store, "s1", "shell",
		"echo hello", `{"command":"echo hello"}`, "hello world", 0)

	entries, err := store.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Tool != "shell" {
		t.Errorf("want tool shell, got %q", entries[0].Tool)
	}
	if entries[0].Command != "echo hello" {
		t.Errorf("want command 'echo hello', got %q", entries[0].Command)
	}
	if entries[0].Result != "hello world" {
		t.Errorf("want result 'hello world', got %q", entries[0].Result)
	}
}

// TestCaptureToolResult_RespectsAutoCapture does not store when false.
func TestCaptureToolResult_RespectsAutoCapture(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "mem.db")
	cfg.AutoCapture = false
	store, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	CaptureToolResult(store, "s1", "shell",
		"echo hello", `{}`, "output", 0)

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0 entries with autoCapture=false, got %d", count)
	}
}

func TestCaptureToolResult_NilStore(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("CaptureToolResult with nil store panicked: %v", r)
		}
	}()
	CaptureToolResult(nil, "s1", "shell",
		"echo", `{}`, "output", 0)
}

// TestCaptureToolResult_TruncatesLongResults.
func TestCaptureToolResult_TruncatesLongResults(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "mem.db")
	store, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	long := string(make([]byte, MaxResultLen+500))
	CaptureToolResult(store, "s1", "shell",
		"echo long", `{}`, long, 0)

	entries, err := store.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if len(entries[0].Result) > MaxResultLen {
		t.Errorf("result length %d exceeds max %d",
			len(entries[0].Result), MaxResultLen)
	}
}
