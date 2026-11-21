package audit

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
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

func TestAuditStore_AdditiveMigrationKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE audit_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			tool TEXT NOT NULL,
			args TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			exit_code INTEGER,
			error TEXT,
			output_hash TEXT,
			captured_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE audit_permissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			entry_id INTEGER REFERENCES audit_entries(id),
			tool TEXT NOT NULL,
			command TEXT NOT NULL,
			rule_pattern TEXT,
			rule_action TEXT,
			decision TEXT NOT NULL,
			reason TEXT,
			decided_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO audit_entries (session_id, tool, args)
		VALUES ('old-session', 'shell', '{}');
		INSERT INTO audit_permissions (session_id, tool, command, decision)
		VALUES ('old-session', 'shell', 'ls', 'allow');
	`)
	if err != nil {
		db.Close()
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewAuditStore(path)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	entries, err := store.QueryEntries(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].SessionID != "old-session" {
		t.Errorf("entries = %+v, want legacy entry", entries)
	}

	perms, err := store.QueryPermissions(QueryFilter{})
	if err != nil {
		t.Fatalf("QueryPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].Decision != "allow" {
		t.Errorf("permissions = %+v, want legacy decision", perms)
	}
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
		SessionID:      "s",
		EntryID:        &entryID,
		Tool:           "shell",
		Command:        "rm -rf /",
		RulePattern:    "*",
		RuleAction:     "deny",
		Decision:       "deny",
		Reason:         "destructive",
		DecidedBy:      "local",
		DecisionSource: "interactive",
		Scope:          "call",
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
	if perms[0].DecidedBy != "local" ||
		perms[0].DecisionSource != "interactive" ||
		perms[0].Scope != "call" {
		t.Errorf("audit attribution = %+v, want local interactive call",
			perms[0])
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

func TestAuditStore_VerifyChain(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.InsertEntry(AuditEntry{
		SessionID: "chain", Tool: "shell", Args: `{}`,
	}); err != nil {
		t.Fatalf("InsertEntry: %v", err)
	}
	if _, err := store.InsertPermission(PermissionAudit{
		SessionID: "chain", Tool: "shell", Command: "true",
		Decision: "allow",
	}); err != nil {
		t.Fatalf("InsertPermission: %v", err)
	}
	if err := store.AppendLifecycleEvent(LifecycleEvent{
		Type: "execution_finished", ExecutionID: "exec-1",
		Status: "succeeded",
	}); err != nil {
		t.Fatalf("AppendLifecycleEvent: %v", err)
	}

	result, err := store.VerifyChain(1, 0)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.From != 1 || result.To != 3 || result.Verified != 3 {
		t.Errorf("result = %+v, want rows 1-3 verified", result)
	}
	found, err := store.HasLifecycleEvent("exec-1", "execution_finished")
	if err != nil || !found {
		t.Errorf("HasLifecycleEvent = %v, %v; want true, nil", found, err)
	}

	if _, err := store.db.Exec(
		`UPDATE audit_chain SET payload = '{}' WHERE seq = 2`,
	); err != nil {
		t.Fatalf("tamper chain: %v", err)
	}
	_, err = store.VerifyChain(1, 0)
	chainErr, ok := err.(*ChainVerificationError)
	if !ok || chainErr.Sequence != 2 {
		t.Errorf("VerifyChain error = %v, want first bad row 2", err)
	}
}

func TestAuditStore_VerifyChainDetectsSourceRowEdit(t *testing.T) {
	store := newTestStore(t)
	id, err := store.InsertEntry(AuditEntry{
		SessionID: "chain-edit", Tool: "shell", Args: "original",
	})
	if err != nil {
		t.Fatalf("InsertEntry: %v", err)
	}
	if _, err := store.db.Exec(
		`UPDATE audit_entries SET args = 'modified' WHERE id = ?`, id,
	); err != nil {
		t.Fatalf("tamper audit entry: %v", err)
	}

	_, err = store.VerifyChain(1, 0)
	chainErr, ok := err.(*ChainVerificationError)
	if !ok || chainErr.Sequence != 1 {
		t.Errorf("VerifyChain error = %v, want first bad row 1", err)
	}
}

func TestAuditStore_VerifyChainDetectsOutputEdit(t *testing.T) {
	store := newTestStore(t)
	entryID, err := store.InsertEntry(AuditEntry{
		SessionID: "chain-output", Tool: "shell", Args: "{}",
	})
	if err != nil {
		t.Fatalf("InsertEntry: %v", err)
	}
	if err := store.InsertOutput(OutputCapture{
		EntryID: entryID, SessionID: "chain-output", Stdout: "original",
	}); err != nil {
		t.Fatalf("InsertOutput: %v", err)
	}
	if _, err := store.db.Exec(
		`UPDATE audit_output SET stdout = 'modified' WHERE entry_id = ?`,
		entryID,
	); err != nil {
		t.Fatalf("tamper output: %v", err)
	}

	_, err = store.VerifyChain(1, 0)
	chainErr, ok := err.(*ChainVerificationError)
	if !ok || chainErr.Sequence != 2 {
		t.Errorf("VerifyChain error = %v, want first bad row 2", err)
	}
}

func TestAuditStore_VerifyChainRange(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 3; i++ {
		if err := store.AppendLifecycleEvent(LifecycleEvent{
			Type: "execution_finished", ExecutionID: strconv.Itoa(i),
		}); err != nil {
			t.Fatalf("AppendLifecycleEvent: %v", err)
		}
	}

	result, err := store.VerifyChain(2, 3)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.From != 2 || result.To != 3 || result.Verified != 2 {
		t.Errorf("result = %+v, want rows 2-3 verified", result)
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
	result, err := store.VerifyChain(1, 0)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.Verified != 500 {
		t.Errorf("verified records = %d, want 500", result.Verified)
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

// TestAuditStore_InputIDRoundTrip verifies the input idempotency key
// is persisted with tool and permission audit entries.
func TestAuditStore_InputIDRoundTrip(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.InsertEntry(AuditEntry{
		SessionID: "s1", InputID: "input-1",
		Tool: "shell", Args: "{}",
	}); err != nil {
		t.Fatalf("InsertEntry: %v", err)
	}
	if _, err := store.InsertPermission(PermissionAudit{
		SessionID: "s1", InputID: "input-1",
		Tool: "shell", Command: "ls", Decision: "allow",
	}); err != nil {
		t.Fatalf("InsertPermission: %v", err)
	}

	entries, err := store.QueryEntries(QueryFilter{SessionID: "s1"})
	if err != nil {
		t.Fatalf("QueryEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].InputID != "input-1" {
		t.Errorf("entries = %+v, want InputID input-1", entries)
	}

	perms, err := store.QueryPermissions(QueryFilter{SessionID: "s1"})
	if err != nil {
		t.Fatalf("QueryPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].InputID != "input-1" {
		t.Errorf("permissions = %+v, want InputID input-1", perms)
	}
}

// TestAuditStore_ExecutionLink verifies entries and permissions keep
// the durable execution ID.
func TestAuditStore_ExecutionLink(t *testing.T) {
	store := newTestStore(t)

	id, err := store.InsertEntry(AuditEntry{
		SessionID:   "s",
		InputID:     "input-1",
		ExecutionID: "exec-1",
		MessageID:   "input-1",
		ToolCallID:  "call-1",
		Tool:        "shell",
		Args:        "{}",
	})
	if err != nil {
		t.Fatalf("InsertEntry: %v", err)
	}

	got, err := store.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.ExecutionID != "exec-1" || got.MessageID != "input-1" ||
		got.ToolCallID != "call-1" {
		t.Errorf("entry = %+v, want execution, message, and call IDs",
			got)
	}

	if _, err := store.InsertPermission(PermissionAudit{
		SessionID:   "s",
		ExecutionID: "exec-1",
		ApprovalID:  "approval-1",
		Tool:        "shell",
		Command:     "ls",
		Decision:    "allow",
	}); err != nil {
		t.Fatalf("InsertPermission: %v", err)
	}
	if _, err := store.InsertPermission(PermissionAudit{
		SessionID: "s", ExecutionID: "exec-2",
		ApprovalID: "approval-2", Tool: "shell", Command: "pwd",
		Decision: "allow",
	}); err != nil {
		t.Fatalf("InsertPermission: %v", err)
	}

	perms, err := store.QueryPermissions(QueryFilter{
		SessionID: "s", ExecutionID: "exec-1", ApprovalID: "approval-1",
	})
	if err != nil {
		t.Fatalf("QueryPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].ExecutionID != "exec-1" ||
		perms[0].ApprovalID != "approval-1" {
		t.Errorf("permissions = %+v, want execution exec-1 and approval-1",
			perms)
	}

	byExecution, err := store.QueryPermissions(
		QueryFilter{ExecutionID: "exec-2"},
	)
	if err != nil {
		t.Fatalf("QueryPermissions by execution: %v", err)
	}
	if len(byExecution) != 1 || byExecution[0].ApprovalID != "approval-2" {
		t.Errorf("execution filter = %+v, want approval-2", byExecution)
	}
	byApproval, err := store.QueryPermissions(
		QueryFilter{ApprovalID: "approval-1"},
	)
	if err != nil {
		t.Fatalf("QueryPermissions by approval: %v", err)
	}
	if len(byApproval) != 1 || byApproval[0].ExecutionID != "exec-1" {
		t.Errorf("approval filter = %+v, want exec-1", byApproval)
	}

	entries, err := store.QueryEntries(QueryFilter{ExecutionID: "exec-1"})
	if err != nil {
		t.Fatalf("QueryEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].ToolCallID != "call-1" {
		t.Errorf("entries = %+v, want call-1 for exec-1", entries)
	}
}

// TestAuditStore_CompleteSubagentNotFound checks completing an unknown
// child session returns a not-found error.
func TestAuditStore_CompleteSubagentNotFound(t *testing.T) {
	store := newTestStore(t)

	err := store.CompleteSubagent("missing-child", 10, "completed")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("CompleteSubagent error = %v, want not found", err)
	}
}

// TestAuditStore_VerifyChainSubagentRows checks completed subagent rows
// verify, and a source-table edit is detected at the finished record.
func TestAuditStore_VerifyChainSubagentRows(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.InsertSubagent(SubagentAudit{
		ParentSessionID: "parent",
		ChildSessionID:  "child-1",
		Tool:            "task",
		Status:          "running",
	}); err != nil {
		t.Fatalf("InsertSubagent: %v", err)
	}
	err := store.CompleteSubagent("child-1", 10, "completed")
	if err != nil {
		t.Fatalf("CompleteSubagent: %v", err)
	}

	result, err := store.VerifyChain(1, 0)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.Verified != 2 {
		t.Errorf("Verified = %d, want 2", result.Verified)
	}

	if _, err := store.db.Exec(
		`UPDATE audit_subagents SET status = 'failed'
		 WHERE child_session_id = 'child-1'`,
	); err != nil {
		t.Fatalf("tamper subagent: %v", err)
	}
	_, err = store.VerifyChain(1, 0)
	chainErr, ok := err.(*ChainVerificationError)
	if !ok || chainErr.Sequence != 2 {
		t.Errorf("VerifyChain error = %v, want first bad row 2", err)
	}
}

// TestAuditStore_VerifyChainDetectsSubagentStartedEdit checks an edit
// to a started-only subagent record is detected.
func TestAuditStore_VerifyChainDetectsSubagentStartedEdit(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.InsertSubagent(SubagentAudit{
		ParentSessionID: "parent",
		ChildSessionID:  "child-1",
		Tool:            "task",
		Status:          "running",
	}); err != nil {
		t.Fatalf("InsertSubagent: %v", err)
	}
	if _, err := store.db.Exec(
		`UPDATE audit_subagents SET status = 'changed'
		 WHERE child_session_id = 'child-1'`,
	); err != nil {
		t.Fatalf("tamper subagent: %v", err)
	}

	_, err := store.VerifyChain(1, 0)
	chainErr, ok := err.(*ChainVerificationError)
	if !ok || chainErr.Sequence != 1 {
		t.Errorf("VerifyChain error = %v, want first bad row 1", err)
	}
}
