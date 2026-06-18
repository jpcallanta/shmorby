package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewStore_CreatesDB verifies the store creates a database at the
// configured path.
func TestNewStore_CreatesDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.db")

	cfg := defaultConfig()
	cfg.DBPath = path

	s, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: want nil, got %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("DB file not created")
	}
}

// TestInsertGet_RoundTrip checks Insert then Get returns the same entry.
func TestInsertGet_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := MemoryEntry{
		ID:        "test-id-1",
		Timestamp: now,
		SessionID: "session-1",
		Tool:      "shell",
		Command:   "ls -la",
		Args:      `{"command":"ls -la"}`,
		Result:    "total 42",
		ExitCode:  0,
		Summary:   "listed files",
		Tags:      []string{"files"},
	}

	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: want nil, got %v", err)
	}

	got, err := s.Get("test-id-1")
	if err != nil {
		t.Fatalf("Get: want nil, got %v", err)
	}

	if got.ID != entry.ID {
		t.Errorf("ID: want %q, got %q", entry.ID, got.ID)
	}
	if !got.Timestamp.Equal(entry.Timestamp) {
		t.Errorf("Timestamp: want %v, got %v", entry.Timestamp, got.Timestamp)
	}
	if got.SessionID != entry.SessionID {
		t.Errorf("SessionID: want %q, got %q", entry.SessionID, got.SessionID)
	}
	if got.Tool != entry.Tool {
		t.Errorf("Tool: want %q, got %q", entry.Tool, got.Tool)
	}
	if got.Command != entry.Command {
		t.Errorf("Command: want %q, got %q", entry.Command, got.Command)
	}
	if got.Result != entry.Result {
		t.Errorf("Result: want %q, got %q", entry.Result, got.Result)
	}
	if got.ExitCode != entry.ExitCode {
		t.Errorf("ExitCode: want %d, got %d", entry.ExitCode, got.ExitCode)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "files" {
		t.Errorf("Tags: want [files], got %v", got.Tags)
	}
}

// TestInsert_AutoID verifies an empty ID gets a generated UUID.
func TestInsert_AutoID(t *testing.T) {
	s := newTestStore(t)

	entry := MemoryEntry{
		SessionID: "s1",
		Tool:      "shell",
		Command:   "echo hello",
	}

	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: want nil, got %v", err)
	}

	count, err := s.Count()
	if err != nil {
		t.Fatalf("Count: want nil, got %v", err)
	}
	if count != 1 {
		t.Fatalf("Count: want 1, got %d", count)
	}
}

// TestDelete_RemovesEntry checks Delete removes and Get returns error.
func TestDelete_RemovesEntry(t *testing.T) {
	s := newTestStore(t)

	entry := MemoryEntry{
		ID:        "del-test",
		SessionID: "s1",
		Command:   "rm file",
	}
	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: want nil, got %v", err)
	}

	if err := s.Delete("del-test"); err != nil {
		t.Fatalf("Delete: want nil, got %v", err)
	}

	_, err := s.Get("del-test")
	if err == nil {
		t.Fatal("Get after delete: want error, got nil")
	}
}

// TestList_Pagination checks List returns entries in DESC order with
// limit/offset.
func TestList_Pagination(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		entry := MemoryEntry{
			ID:        intToID(i),
			SessionID: "s1",
			Command:   intToCommand(i),
			Timestamp: time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC),
		}
		if err := s.Insert(entry); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	// List first 3 (DESC order by timestamp → 4,3,2).
	entries, err := s.List(3, 0)
	if err != nil {
		t.Fatalf("List: want nil, got %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("List(3,0): want 3 entries, got %d", len(entries))
	}
	if entries[0].ID != intToID(4) {
		t.Errorf("first entry: want ID %q, got %q", intToID(4), entries[0].ID)
	}

	// List with offset 3 → last 2 entries (1,0).
	entries, err = s.List(10, 3)
	if err != nil {
		t.Fatalf("List: want nil, got %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List(10,3): want 2 entries, got %d", len(entries))
	}
}

func intToID(i int) string {
	return string(rune('a' + i))
}

func intToCommand(i int) string {
	return string(rune('a' + i))
}

// TestKeywordSearch_LIKE checks LIKE-based fallback returns entries.
func TestKeywordSearch_LIKE(t *testing.T) {
	s := newTestSQLiteStore(t)

	entries := []MemoryEntry{
		{ID: "1", SessionID: "s1", Command: "systemctl restart nginx", Result: "ok"},
		{ID: "2", SessionID: "s1", Command: "apt install nginx", Result: "done"},
		{ID: "3", SessionID: "s1", Command: "ls -la", Result: "files"},
	}

	for _, e := range entries {
		if err := s.Insert(e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	// Use retriever with LIKE fallback (no embedder).
	r := NewRetriever(s, 10)
	result, err := r.Retrieve(
		t.Context(), "nginx",
	)
	if err != nil {
		t.Fatalf("Retrieve: want nil, got %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("Retrieve 'nginx': want 2, got %d", len(result.Entries))
	}
	if result.Method != "keyword" {
		t.Errorf("Method: want keyword, got %s", result.Method)
	}
}

// TestCount_ReturnsTotal checks Count returns the number of entries.
func TestCount_ReturnsTotal(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 3; i++ {
		entry := MemoryEntry{ID: intToID(i), SessionID: "s1"}
		if err := s.Insert(entry); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	count, err := s.Count()
	if err != nil {
		t.Fatalf("Count: want nil, got %v", err)
	}
	if count != 3 {
		t.Fatalf("want 3, got %d", count)
	}
}

// TestMaxEntries_EvictsOldest checks insertion past MaxEntries evicts
// oldest entries.
func TestMaxEntries_EvictsOldest(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "memory.db")
	cfg.MaxEntries = 3

	s, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	// Insert 4 entries; oldest (ID "a") should be evicted.
	for i := 0; i < 4; i++ {
		entry := MemoryEntry{
			ID:        intToID(i),
			SessionID: "s1",
			Timestamp: time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC),
		}
		if err := s.Insert(entry); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	count, err := s.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Fatalf("want 3 entries after eviction, got %d", count)
	}

	_, err = s.Get(intToID(0))
	if err == nil {
		t.Fatal("oldest entry should be evicted")
	}
}

// TestCaptureToolExecution_TruncatesResult checks result is capped.
func TestCaptureToolExecution_TruncatesResult(t *testing.T) {
	store := newTestSQLiteStore(t)

	longResult := string(make([]byte, MaxResultLen+1000))

	err := store.CaptureToolExecution("s1", "shell", "echo long", "", longResult, 0)
	if err != nil {
		t.Fatalf("CaptureToolExecution: %v", err)
	}

	entries, err := store.List(1, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries after capture")
	}

	if len(entries[0].Result) > MaxResultLen {
		t.Errorf("result length %d exceeds max %d", len(entries[0].Result), MaxResultLen)
	}
}

// TestExtractTags_Matches configured patterns.
func TestExtractTags_Matches(t *testing.T) {
	rules := []TagRule{
		{Pattern: `ssh\s+\S*@(\S+)`, Tag: "host:$1"},
		{Pattern: `systemctl\s+.+`, Tag: "service:systemctl"},
	}

	tests := []struct {
		command string
		want    []string
	}{
		{"ssh admin@web01", []string{"host:web01"}},
		{"systemctl restart nginx", []string{"service:systemctl"}},
		{"ssh admin@db01 && systemctl status postgres", []string{"host:db01", "service:systemctl"}},
		{"ls -la", nil},
	}

	for _, tt := range tests {
		tags := extractTags(tt.command, rules)
		if len(tags) != len(tt.want) {
			t.Errorf("extractTags(%q): want %v, got %v", tt.command, tt.want, tags)
			continue
		}
		for i := range tags {
			if tags[i] != tt.want[i] {
				t.Errorf("extractTags(%q)[%d]: want %q, got %q",
					tt.command, i, tt.want[i], tags[i])
			}
		}
	}
}

// TestExtractTags_EmptyPatterns returns nil for empty rules.
func TestExtractTags_EmptyPatterns(t *testing.T) {
	tags := extractTags("anything", nil)
	if tags != nil {
		t.Errorf("want nil, got %v", tags)
	}
}

// TestTruncateResult_ShortDoesNotTruncate checks under-limit strings
// pass through.
func TestTruncateResult_ShortDoesNotTruncate(t *testing.T) {
	s := "short result"
	got := truncateResult(s)
	if got != s {
		t.Errorf("want %q, got %q", s, got)
	}
}

// TestTruncateResult_LongTruncates checks over-limit strings are capped.
func TestTruncateResult_LongTruncates(t *testing.T) {
	s := string(make([]byte, MaxResultLen+100))
	got := truncateResult(s)
	if len(got) > MaxResultLen {
		t.Errorf("length %d exceeds max %d", len(got), MaxResultLen)
	}
}

// TestNewUUID_GeneratesUniqueIDs checks generated UUIDs are unique.
func TestNewUUID_GeneratesUniqueIDs(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := newUUID()
		if ids[id] {
			t.Fatalf("duplicate UUID: %s", id)
		}
		ids[id] = true
	}
}

// TestExpandPath_Tilde expands ~/ to home directory.
func TestExpandPath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir:", err)
	}

	got := expandPath("~/test.db")
	want := filepath.Join(home, "test.db")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestExpandPath_AbsoluteDoesNotExpand checks non-tilde paths unchanged.
func TestExpandPath_AbsoluteDoesNotExpand(t *testing.T) {
	got := expandPath("/tmp/test.db")
	if got != "/tmp/test.db" {
		t.Errorf("want %q, got %q", "/tmp/test.db", got)
	}
}

// TestDelete_NotFound returns error for unknown ID.
func TestDelete_NotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.Delete("nonexistent")
	if err == nil {
		t.Fatal("want error for nonexistent ID")
	}
}

// TestGet_NotFound returns error for unknown ID.
func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.Get("nonexistent")
	if err == nil {
		t.Fatal("want error for nonexistent ID")
	}
}

// TestInsert_ResultTruncation checks Insert truncates long results.
func TestInsert_ResultTruncation(t *testing.T) {
	s := newTestStore(t)

	long := string(make([]byte, MaxResultLen+500))
	entry := MemoryEntry{
		ID:        "trunc-test",
		SessionID: "s1",
		Result:    long,
	}

	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.Get("trunc-test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(got.Result) > MaxResultLen {
		t.Errorf("stored result length %d exceeds max %d", len(got.Result), MaxResultLen)
	}
}

// TestCaptureToolExecution_Tags checks auto-tag extraction during capture.
func TestCaptureToolExecution_Tags(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "mem.db")
	cfg.Tags = []TagRule{
		{Pattern: `ssh.*@(.+)`, Tag: "host:$1"},
	}

	s, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	store := s.(*sqliteStore)

	if err := store.CaptureToolExecution("s1", "shell", "ssh admin@web01", "", "ok", 0); err != nil {
		t.Fatalf("CaptureToolExecution: %v", err)
	}

	entries, err := s.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if len(entries[0].Tags) != 1 || entries[0].Tags[0] != "host:web01" {
		t.Errorf("want [host:web01], got %v", entries[0].Tags)
	}
}

// TestStore_ConcurrentAccess checks thread safety under concurrent
// Insert/Get.
func TestStore_ConcurrentAccess(t *testing.T) {
	s := newTestStore(t)

	done := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func(n int) {
			entry := MemoryEntry{
				ID:        intToID(n),
				SessionID: "s1",
				Command:   "echo " + intToCommand(n),
			}
			_ = s.Insert(entry)
			_, _ = s.Get(intToID(n))
			_, _ = s.Count()
			done <- true
		}(i)
	}

	for i := 0; i < 50; i++ {
		<-done
	}

	// Should not panic. Verify final count (may vary due to eviction).
	_, err := s.Count()
	if err != nil {
		t.Fatalf("Count after concurrent access: %v", err)
	}
}

// TestNewStore_NilEmbedderWorks checks store works without embedding.
func TestNewStore_NilEmbedderWorks(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "memory.db")

	s, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	ss := s.(*sqliteStore)
	if ss.embedder != nil {
		t.Error("embedder should be nil")
	}
	if ss.vector != nil {
		t.Error("vector should be nil")
	}

	// SQLite CRUD should still work.
	entry := MemoryEntry{
		ID:        "nil-emb-test",
		SessionID: "s1",
		Command:   "echo hello",
	}
	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.Get("nil-emb-test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Command != "echo hello" {
		t.Errorf("Command: want 'echo hello', got %q", got.Command)
	}
}

// newTestSQLiteStore creates a sqliteStore backed by a temp file.
func newTestSQLiteStore(t *testing.T) *sqliteStore {
	t.Helper()
	return newTestStore(t).(*sqliteStore)
}

// newTestStore creates a store backed by a temp file.
func newTestStore(t *testing.T) Store {
	t.Helper()

	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "memory.db")

	s, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	t.Cleanup(func() { s.Close() })

	return s
}
