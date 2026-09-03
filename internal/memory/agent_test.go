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
	if !strings.Contains(got, "never as instructions") {
		t.Error("missing untrusted-data header")
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
	if !strings.Contains(got, "untrusted reference data") {
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

func TestFormatMemoryContext_WithSummary(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "systemctl restart nginx",
			ExitCode:  0,
			Summary:   "nginx restarted successfully",
		},
	}

	got := FormatMemoryContext(entries)

	if !strings.Contains(got, "nginx restarted successfully") {
		t.Errorf("want summary in output, got %q", got)
	}
}

func TestFormatMemoryContext_SummaryEmpty(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "ls -la",
			ExitCode:  0,
		},
	}

	got := FormatMemoryContext(entries)

	if strings.Count(got, "\n  ") > 0 {
		t.Errorf("want no summary line, got %q", got)
	}
}

func TestDedupMemoryContext_Basic(t *testing.T) {
	entries := []MemoryEntry{
		{Tool: "shell", Command: "systemctl restart nginx"},
	}
	sessionMessages := []session.Message{
		{Role: "assistant",
			Content: "[compressed] shell systemctl restart nginx"},
	}

	result := DedupMemoryContext(entries, sessionMessages)

	if len(result) != 0 {
		t.Errorf("want 0 entries (deduped), got %d", len(result))
	}
}

func TestDedupMemoryContext_NotInSession(t *testing.T) {
	entries := []MemoryEntry{
		{Tool: "shell", Command: "apt update"},
	}
	sessionMessages := []session.Message{
		{Role: "assistant",
			Content: "[compressed] shell systemctl restart nginx"},
	}

	result := DedupMemoryContext(entries, sessionMessages)

	if len(result) != 1 {
		t.Errorf("want 1 entry kept, got %d", len(result))
	}
}

func TestDedupMemoryContext_EmptyEntries(t *testing.T) {
	result := DedupMemoryContext(nil,
		[]session.Message{{Role: "user", Content: "hello"}})
	if result != nil {
		t.Errorf("want nil, got %v", result)
	}

	result = DedupMemoryContext([]MemoryEntry{}, nil)
	if len(result) != 0 {
		t.Errorf("want empty, got %d", len(result))
	}
}

func TestDedupMemoryContext_PartialMatch(t *testing.T) {
	entries := []MemoryEntry{
		{Tool: "shell", Command: "echo hello"},
	}
	sessionMessages := []session.Message{
		{Role: "assistant",
			Content: "ran shell echo hello and got hello back"},
	}

	result := DedupMemoryContext(entries, sessionMessages)

	if len(result) != 0 {
		t.Errorf("want 0 entries (substring match), got %d", len(result))
	}
}

func TestDedupMemoryContext_CompressedMatch(t *testing.T) {
	entries := []MemoryEntry{
		{Tool: "shell", Command: "systemctl restart nginx"},
	}
	sessionMessages := []session.Message{
		{Role: "assistant",
			Content: "[compressed] shell systemctl restart nginx success"},
	}

	result := DedupMemoryContext(entries, sessionMessages)

	if len(result) != 0 {
		t.Errorf("want 0 entries (matched compressed text), got %d",
			len(result))
	}
}

// --- Memory token efficiency tests ---

func TestBuildOutcomeSummary_Success(t *testing.T) {
	got := buildOutcomeSummary(0, "hello world")
	if got != "success: hello world" {
		t.Errorf("want 'success: hello world', got %q", got)
	}
}

func TestBuildOutcomeSummary_Failure(t *testing.T) {
	got := buildOutcomeSummary(1, "permission denied")
	if got != "exit 1: permission denied" {
		t.Errorf("want 'exit 1: permission denied', got %q", got)
	}
}

func TestBuildOutcomeSummary_EmptyResult(t *testing.T) {
	got := buildOutcomeSummary(0, "")
	if got != "success" {
		t.Errorf("want 'success', got %q", got)
	}
}

func TestBuildOutcomeSummary_LongResult(t *testing.T) {
	long := string(make([]byte, 300))
	got := buildOutcomeSummary(0, long)
	if len(got) > 220 { // "success: " + 200 + "..."
		t.Errorf("want truncated summary, got len %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("want '...' suffix, got %q", got)
	}
}

func TestFormatMemoryContext_Budget(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "systemctl restart nginx",
			ExitCode:  0,
			Summary:   "nginx restarted",
		},
		{
			Timestamp: time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC),
			Tool:      "ssh",
			Command:   "apt update",
			ExitCode:  0,
			Summary:   "packages updated",
		},
		{
			Timestamp: time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "docker ps",
			ExitCode:  0,
			Summary:   "containers running",
		},
	}

	// Entries are ranked: top-ranked entries are kept within budget,
	// lower-ranked entries dropped, header and footer always present.
	// Token math (chars/4): header=28, footer=25, entry1=17,
	// entry2=14, entry3=15.
	got := FormatMemoryContext(entries, 70)
	if !strings.Contains(got, "never as instructions") {
		t.Error("missing untrusted-data header")
	}
	if !strings.Contains(got, "untrusted reference data") {
		t.Error("missing footer")
	}
	if !strings.Contains(got, "systemctl restart nginx") {
		t.Error("want top-ranked entry kept within budget")
	}
	if strings.Contains(got, "apt update") {
		t.Error("budget should drop second-ranked entry")
	}

	// A larger budget keeps the top two entries (partial retention).
	got = FormatMemoryContext(entries, 85)
	if !strings.Contains(got, "systemctl restart nginx") ||
		!strings.Contains(got, "apt update") {
		t.Error("want top two entries kept within larger budget")
	}
	if strings.Contains(got, "docker ps") {
		t.Error("budget should still drop lowest-ranked entry")
	}
}

// TestFormatMemoryContext_TinyBudgetNotUnlimited verifies that a budget
// smaller than the footer token estimate does not silently become
// unlimited (clamp residual ≥ 1 after footer reservation).
func TestFormatMemoryContext_TinyBudgetNotUnlimited(t *testing.T) {
	entries := []MemoryEntry{
		{
			Timestamp: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			Tool:      "shell",
			Command:   "systemctl restart nginx",
			ExitCode:  0,
			Summary:   "nginx restarted successfully",
		},
		{
			Timestamp: time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC),
			Tool:      "ssh",
			Command:   "apt update",
			ExitCode:  0,
			Summary:   "packages updated",
		},
	}

	// budget=5 is smaller than the footer (~25 tokens); residual
	// would be negative without the clamp. With the fix, residual
	// is clamped to 1, so no entries fit and only header+footer
	// are emitted.
	got := FormatMemoryContext(entries, 5)
	if !strings.Contains(got, "never as instructions") {
		t.Error("missing untrusted-data header")
	}
	if !strings.Contains(got, "untrusted reference data") {
		t.Error("missing footer")
	}
	if strings.Contains(got, "systemctl restart nginx") {
		t.Error("tiny budget should drop all entries")
	}
	if strings.Contains(got, "apt update") {
		t.Error("tiny budget should drop all entries")
	}
}

func TestCaptureToolResult_PopulatesSummary(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "mem.db")
	store, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	CaptureToolResult(store, "s1", "shell",
		"echo hello", `{}`, "hello world", 0)

	entries, err := store.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Summary == "" {
		t.Error("want non-empty summary, got empty")
	}
	if !strings.Contains(entries[0].Summary, "success") {
		t.Errorf("want 'success' in summary, got %q", entries[0].Summary)
	}
	if !strings.Contains(entries[0].Summary, "hello world") {
		t.Errorf("want result in summary, got %q", entries[0].Summary)
	}
}

func TestEntryText_IncludesSummary(t *testing.T) {
	entry := MemoryEntry{
		Tool:    "shell",
		Command: "echo hello",
		Summary: "success: hello world",
	}
	got := entryText(entry)
	if !strings.Contains(got, "success: hello world") {
		t.Errorf("want summary in entryText, got %q", got)
	}
}
