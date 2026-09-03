package audit

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func intPtr(i int) *int { return &i }

func newTestStore(t *testing.T) *AuditStore {
	t.Helper()
	store, err := NewAuditStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestAuditStore_InsertEntry(t *testing.T) {
	store := newTestStore(t)

	entry := AuditEntry{
		SessionID:  "sess-1",
		Tool:       "shell",
		Args:       `{"command":"ls"}`,
		DurationMs: 42,
		ExitCode:   intPtr(0),
	}
	id, err := store.InsertEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	got, err := store.GetEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Tool != "shell" {
		t.Errorf("expected tool shell, got %s", got.Tool)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("expected session sess-1, got %s", got.SessionID)
	}
}

func TestAuditStore_BatchInsert(t *testing.T) {
	store := newTestStore(t)

	entries := make([]AuditEntry, 100)
	for i := range entries {
		entries[i] = AuditEntry{
			SessionID:  "sess-batch",
			Tool:       "shell",
			Args:       `{"command":"echo ` + strconv.Itoa(i) + `"}`,
			DurationMs: int64(i),
			ExitCode:   intPtr(0),
		}
	}

	if err := store.BatchInsert(entries); err != nil {
		t.Fatal(err)
	}

	got, err := store.QueryEntries(QueryFilter{
		SessionID: "sess-batch",
		Limit:     200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 {
		t.Errorf("expected 100 entries, got %d", len(got))
	}
}

func TestAuditStore_QueryByTool(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}"})
	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "ssh", Args: "{}"})
	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}"})

	entries, err := store.QueryEntries(QueryFilter{Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 shell entries, got %d", len(entries))
	}
}

func TestAuditStore_QueryBySession(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s1", Tool: "shell", Args: "{}"})
	store.InsertEntry(AuditEntry{SessionID: "s2", Tool: "shell", Args: "{}"})

	entries, err := store.QueryEntries(QueryFilter{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for s1, got %d", len(entries))
	}
}

func TestAuditStore_QueryByTimeRange(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}"})
	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}"})

	since := time.Now().Add(-1 * time.Hour)
	entries, err := store.QueryEntries(QueryFilter{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	before := time.Now().Add(-2 * time.Hour)
	entries, err = store.QueryEntries(QueryFilter{Before: before})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestAuditStore_QueryByExitCode(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}", ExitCode: intPtr(0)})
	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}", ExitCode: intPtr(1)})

	code0 := 0
	entries, err := store.QueryEntries(QueryFilter{ExitCode: &code0})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry with exit 0, got %d", len(entries))
	}
}

func TestAuditStore_PermissionInsert(t *testing.T) {
	store := newTestStore(t)

	entryID, _ := store.InsertEntry(AuditEntry{
		SessionID: "s", Tool: "shell", Args: "{}",
	})

	perm := PermissionAudit{
		SessionID:   "s",
		EntryID:     &entryID,
		Tool:        "shell",
		Command:     "rm -rf /",
		RulePattern: "*",
		RuleAction:  "deny",
		Decision:    "deny",
		Reason:      "destructive",
	}
	id, err := store.InsertPermission(perm)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	perms, err := store.QueryPermissions(QueryFilter{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(perms) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(perms))
	}
	if perms[0].Decision != "deny" {
		t.Errorf("expected decision deny, got %s", perms[0].Decision)
	}
}

func TestAuditStore_OutputCapture(t *testing.T) {
	store := newTestStore(t)

	entryID, _ := store.InsertEntry(AuditEntry{
		SessionID: "s", Tool: "shell", Args: "{}",
	})

	checksum := ComputeChecksum("hello world")
	output := OutputCapture{
		EntryID:    entryID,
		SessionID:  "s",
		Stdout:     "hello world",
		Stderr:     "",
		StdoutSize: 11,
		StderrSize: 0,
		Checksum:   checksum,
	}
	if err := store.InsertOutput(output); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetOutput(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected output, got nil")
	}
	if got.Stdout != "hello world" {
		t.Errorf("expected stdout 'hello world', got %q", got.Stdout)
	}
	if got.Checksum != checksum {
		t.Errorf("expected checksum %s, got %s", checksum, got.Checksum)
	}
}

func TestAuditStore_SubagentLink(t *testing.T) {
	store := newTestStore(t)

	sa := SubagentAudit{
		ParentSessionID: "parent",
		ChildSessionID:  "child-1",
		Tool:            "task",
		Status:          "running",
	}
	id, err := store.InsertSubagent(sa)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	if err := store.CompleteSubagent("child-1", 1500, "completed"); err != nil {
		t.Fatal(err)
	}

	subs, err := store.GetSubagents("parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subagent, got %d", len(subs))
	}
	if subs[0].Status != "completed" {
		t.Errorf("expected status completed, got %s", subs[0].Status)
	}
	if subs[0].DurationMs == nil || *subs[0].DurationMs != 1500 {
		t.Errorf("expected duration 1500, got %v", subs[0].DurationMs)
	}
}

func TestAuditStore_Vacuum(t *testing.T) {
	store := newTestStore(t)

	// Insert an entry with a past timestamp via SQL to bypass
	// datetime('now') default.
	_, err := store.db.Exec(
		`INSERT INTO audit_entries (session_id, tool, args, captured_at)
		 VALUES (?, ?, ?, datetime('now', '-2 hours'))`,
		"s", "shell", "{}",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Verify entry exists before vacuum.
	var count int
	store.db.QueryRow(`SELECT COUNT(*) FROM audit_entries`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 entry before vacuum, got %d", count)
	}

	// Vacuum with 1 hour retention should remove the 2-hour-old entry.
	entries, perms, output, subs, vErr := store.Vacuum(1 * time.Hour)
	if vErr != nil {
		t.Fatal(vErr)
	}
	if entries != 1 {
		t.Errorf("expected 1 entry removed, got %d", entries)
	}
	if perms != 0 {
		t.Errorf("expected 0 permissions removed, got %d", perms)
	}
	if output != 0 {
		t.Errorf("expected 0 output removed, got %d", output)
	}
	if subs != 0 {
		t.Errorf("expected 0 subagents removed, got %d", subs)
	}
}

func TestAuditStore_ExportJSON(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: `{"command":"ls"}`})
	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "ssh", Args: `{"host":"x"}`})

	var buf bytes.Buffer
	if err := store.ExportJSON(&buf, QueryFilter{}); err != nil {
		t.Fatal(err)
	}

	var entries []AuditEntry
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var e AuditEntry
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestAuditStore_ExportCSV(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: `{"command":"ls"}`, ExitCode: intPtr(0)})

	var buf bytes.Buffer
	if err := store.ExportCSV(&buf, QueryFilter{}); err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (header + 1 row), got %d", len(lines))
	}
}

func TestAuditStore_IntegrityChecksum(t *testing.T) {
	data := "test output data"
	checksum := ComputeChecksum(data)

	expected := ComputeChecksum(data)
	if checksum != expected {
		t.Errorf("checksum: want %s, got %s", expected, checksum)
	}
}

func TestAuditStore_ConcurrentWrites(t *testing.T) {
	store := newTestStore(t)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 50; j++ {
				store.InsertEntry(AuditEntry{
					SessionID: "conc-" + strconv.Itoa(n),
					Tool:      "shell",
					Args:      "{}",
				})
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	entries, err := store.QueryEntries(QueryFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 500 {
		t.Errorf("expected 500 entries, got %d", len(entries))
	}
}

func TestAuditStore_Stats(t *testing.T) {
	store := newTestStore(t)

	store.InsertEntry(AuditEntry{SessionID: "s", Tool: "shell", Args: "{}"})

	stats, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
	if stats.DBSize <= 0 {
		t.Errorf("expected positive DB size, got %d", stats.DBSize)
	}
}
