package session

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"shmorby/internal/llm"
)

// Opens an in-memory store; fails the test on error.
func newTestStore(t *testing.T) Store {
	t.Helper()

	st, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return st
}

// Saves a metadata row with sane defaults for the given id.
func mustSave(t *testing.T, st Store, id string) Meta {
	t.Helper()

	m := Meta{
		ID:        id,
		Title:     "test",
		Directory: "/tmp/x",
		AgentMode: "operate",
		Provider:  "ollama",
		Model:     "llama3.2",
	}
	if err := st.Save(m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	return m
}

func TestStore_SaveLoadRoundTrip(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	msgs := []Message{
		{Role: "user", Content: "check disk usage"},
		{Role: "assistant", Content: "running df",
			ToolCalls: []llm.ToolCall{
				{ID: "c1", Name: "shell",
					Args: `{"command":"df -h"}`},
			}},
		{Role: "tool", Content: "/dev/sda 100G",
			ToolName: "shell", ToolCallID: "c1"},
	}
	if err := st.Append("s1", msgs); err != nil {
		t.Fatalf("Append: %v", err)
	}

	sess, m, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.ID != "s1" || m.Provider != "ollama" {
		t.Errorf("meta mismatch: %+v", m)
	}
	if m.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", m.MessageCount)
	}
	got := sess.Messages()
	if len(got) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(got))
	}
	if got[1].ToolCalls[0].Name != "shell" ||
		got[1].ToolCalls[0].ID != "c1" {
		t.Errorf("tool calls mismatch: %+v", got[1].ToolCalls)
	}
	if got[2].ToolCallID != "c1" || got[2].ToolName != "shell" {
		t.Errorf("tool result fields mismatch: %+v", got[2])
	}
}

func TestStore_LoadUnknownID(t *testing.T) {
	st := newTestStore(t)

	_, _, err := st.Load("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Load unknown = %v, want ErrNotFound", err)
	}
}

func TestStore_AppendAssignsAscendingSeq(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	for i := 0; i < 3; i++ {
		err := st.Append("s1", []Message{
			{Role: "user", Content: "msg"},
		})
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	_, m, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.LastSeq != 3 {
		t.Errorf("LastSeq = %d, want 3", m.LastSeq)
	}
}

func TestStore_ReplaceFromRewritesTail(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")
	if err := st.Append("s1", []Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Compression rewrite: replace everything with a stub + tail.
	if err := st.ReplaceFrom("s1", 0, []Message{
		{Role: "assistant", Content: "[compressed] summary"},
		{Role: "user", Content: "c"},
	}); err != nil {
		t.Fatalf("ReplaceFrom: %v", err)
	}

	sess, m, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := sess.Messages()
	if len(got) != 2 || got[0].Content != "[compressed] summary" {
		t.Fatalf("messages = %+v, want stub + tail", got)
	}
	if m.LastSeq != 2 {
		t.Errorf("LastSeq = %d, want 2", m.LastSeq)
	}

	// Appending continues after the rewritten window.
	if err := st.Append("s1", []Message{
		{Role: "assistant", Content: "d"},
	}); err != nil {
		t.Fatalf("Append after replace: %v", err)
	}
	sess, _, err = st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Messages()) != 3 {
		t.Errorf("len = %d, want 3", len(sess.Messages()))
	}
}

func TestStore_LoadTruncatesDanglingToolCalls(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	// Simulate SIGINT after the assistant tool_calls row landed but
	// before its tool result (AC6).
	if err := st.Append("s1", []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "shell", Args: "{}"},
		}},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	sess, m, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := sess.Messages()
	if len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("messages = %+v, want trailing turn truncated", got)
	}

	// The dangling rows must be gone from the store, too: a second
	// Load sees the same truncated view.
	sess2, _, err := st.Load("s1")
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if len(sess2.Messages()) != 1 {
		t.Errorf("store still holds dangling rows: %d messages",
			len(sess2.Messages()))
	}
	if m.LastSeq != 1 {
		t.Errorf("LastSeq = %d, want 1", m.LastSeq)
	}
}

func TestStore_LoadKeepsCompleteToolTurns(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	if err := st.Append("s1", []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "shell", Args: "{}"},
			{ID: "c2", Name: "shell", Args: "{}"},
		}},
		{Role: "tool", Content: "out1", ToolCallID: "c1"},
		{Role: "tool", Content: "out2", ToolCallID: "c2"},
		{Role: "assistant", Content: "done"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	sess, _, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Messages()) != 5 {
		t.Errorf("complete turns truncated: %d messages, want 5",
			len(sess.Messages()))
	}
}

func TestStore_LatestPerDirectory(t *testing.T) {
	st := newTestStore(t)
	for _, dir := range []string{"/a", "/b"} {
		m := Meta{ID: "id-" + dir, Directory: dir}
		if err := st.Save(m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	m, ok := st.Latest("/a")
	if !ok || m.ID != "id-/a" {
		t.Fatalf("Latest(/a) = %+v, %v", m, ok)
	}
	if _, ok := st.Latest("/missing"); ok {
		t.Error("Latest(/missing) found a session")
	}
}

func TestStore_ListScopesAndExcludesChildren(t *testing.T) {
	st := newTestStore(t)
	save := func(m Meta) {
		t.Helper()
		if err := st.Save(m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	save(Meta{ID: "root1", Directory: "/a", Title: "one"})
	save(Meta{ID: "root2", Directory: "/b", Title: "two"})
	// Child session in /a must never be listed.
	save(Meta{ID: "child", Directory: "/a",
		ParentID: "root1", Title: "c"})

	metas, err := st.List(Query{Directory: "/a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != "root1" {
		t.Fatalf("List(/a) = %+v, want only root1", metas)
	}

	metas, err = st.List(Query{})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(metas) != 2 {
		t.Errorf("List all dirs = %d, want 2", len(metas))
	}
}

func TestStore_ListNewestFirst(t *testing.T) {
	st := newTestStore(t)
	if err := st.Save(Meta{ID: "older", Directory: "/a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Force a later updated_at on the second row.
	if err := st.Save(Meta{ID: "newer", Directory: "/a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Force a later updated_at on the second row.
	if _, err := st.(*sqliteStore).db.Exec(
		`UPDATE sessions SET updated_at = updated_at + 100 WHERE id = 'newer'`,
	); err != nil {
		t.Fatalf("bump: %v", err)
	}

	metas, err := st.List(Query{Directory: "/a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 || metas[0].ID != "newer" {
		t.Fatalf("order wrong: %+v", metas)
	}
}

func TestStore_ArchiveDelete(t *testing.T) {
	st := newTestStore(t)
	if err := st.Save(Meta{ID: "s1", Directory: "/tmp/arch"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	mustSave(t, st, "s2")
	if err := st.Save(Meta{ID: "child", ParentID: "s2"}); err != nil {
		t.Fatalf("Save child: %v", err)
	}
	err := st.Append("s2", []Message{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := st.Archive("s1"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	metas, _ := st.List(Query{})
	for _, m := range metas {
		if m.ID == "s1" {
			t.Error("archived session listed by default")
		}
	}
	metas, _ = st.List(Query{IncludeArchived: true})
	if len(metas) != 2 {
		t.Errorf("IncludeArchived list = %d, want 2", len(metas))
	}

	// Latest must skip archived rows so -c never resumes one.
	if _, ok := st.Latest("/tmp/arch"); ok {
		t.Error("Latest returned archived s1")
	}

	// Delete cascades to children.
	if err := st.Delete("s2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := st.Load("child"); !errors.Is(err, ErrNotFound) {
		t.Errorf("child after cascade delete = %v, want ErrNotFound", err)
	}
}

func TestStore_Rename(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	if err := st.Rename("s1", "production debug run"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	_, m, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Title != "production debug run" {
		t.Errorf("Title = %q", m.Title)
	}

	// A later Save with an empty title must not clobber the rename.
	if err := st.Save(Meta{ID: "s1", Directory: "/tmp/x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, m, _ = st.Load("s1")
	if m.Title != "production debug run" {
		t.Errorf("Save clobbered title: %q", m.Title)
	}
}

func TestStore_ListLimit(t *testing.T) {
	st := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := st.Save(Meta{ID: id, Directory: "/l"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	// Make ordering deterministic: c newest, then b, then a.
	db := st.(*sqliteStore).db
	if _, err := db.Exec(
		`UPDATE sessions SET updated_at = updated_at +
		 CASE id WHEN 'b' THEN 10 WHEN 'c' THEN 20 ELSE 0 END`,
	); err != nil {
		t.Fatalf("bump: %v", err)
	}

	metas, err := st.List(Query{Directory: "/l", Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 || metas[0].ID != "c" || metas[1].ID != "b" {
		t.Errorf("List limit newest-first = %+v", metas)
	}
}

// Review regression: age and cap passes must resolve to one
// all-or-nothing batch; ids selected by both passes are deleted
// (and counted) exactly once.
func TestStore_PruneCombinedDedupesAndCounts(t *testing.T) {
	st := newTestStore(t)
	db := st.(*sqliteStore).db

	for _, id := range []string{"x1", "x2", "x3"} {
		mustSave(t, st, id)
	}
	// Age all three out of retention; cap of 1 then also selects
	// the two older ones — overlapping passes must dedupe.
	if _, err := db.Exec(
		`UPDATE sessions SET updated_at = updated_at - 200*86400`,
	); err != nil {
		t.Fatalf("age: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE sessions SET updated_at = updated_at + 1 WHERE id = 'x3'`,
	); err != nil {
		t.Fatalf("order: %v", err)
	}

	n, err := st.Prune(90*24*time.Hour, 1)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	// Retention alone deletes all three (each < now-90d); the cap
	// pass finds nothing left outside the newest 1.
	if n != 3 {
		t.Errorf("Prune = %d, want 3", n)
	}
	var left int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sessions`,
	).Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 0 {
		t.Errorf("sessions left = %d, want 0", left)
	}
	var msgs int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM session_messages`,
	).Scan(&msgs); err != nil {
		t.Fatalf("count msgs: %v", err)
	}
	if msgs != 0 {
		t.Errorf("messages left = %d, want 0", msgs)
	}
}

func TestStore_PruneAgeCapAndGuard(t *testing.T) {
	st := newTestStore(t)
	db := st.(*sqliteStore).db

	// Two old sessions (archived + inactive) and two fresh ones
	// written "now" (inside the active guard window).
	ids := []string{"old-arch", "old-live", "guard1", "guard2"}
	for _, id := range ids {
		mustSave(t, st, id)
	}
	if _, err := db.Exec(
		`UPDATE sessions SET archived_at =
		 updated_at - 100*86400 WHERE id = 'old-arch'`,
	); err != nil {
		t.Fatalf("archive: %v", err)
	}
	// Aged rows: updated_at outside both retention and the guard.
	if _, err := db.Exec(
		`UPDATE sessions SET updated_at = updated_at - 200*86400
		 WHERE id IN ('old-live', 'old-arch')`,
	); err != nil {
		t.Fatalf("age: %v", err)
	}

	n, err := st.Prune(90*24*time.Hour, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 2 {
		t.Errorf("Prune(older) = %d, want 2", n)
	}
	for _, id := range []string{"guard1", "guard2"} {
		if _, ok, _ := st.(*sqliteStore).metaByID(id); !ok {
			t.Errorf("guarded session %s was pruned", id)
		}
	}

	// Cap pass: maxKeep=1 must keep the newest guarded rows and
	// delete the older of the survivors (guard-protected stay).
	n, err = st.Prune(0, 1)
	if err != nil {
		t.Fatalf("Prune cap: %v", err)
	}
	// guard1/guard2 are both inside the guard window: they survive
	// regardless of the cap.
	if n != 0 {
		t.Errorf("cap pass deleted %d guarded sessions, want 0", n)
	}

	// Ageing one guarded row makes it eligible.
	if _, err := db.Exec(
		`UPDATE sessions SET updated_at = updated_at - 10*86400 WHERE id = 'guard1'`,
	); err != nil {
		t.Fatalf("age guard1: %v", err)
	}
	n, err = st.Prune(0, 1)
	if err != nil {
		t.Fatalf("Prune cap 2: %v", err)
	}
	if n != 1 {
		t.Errorf("Prune cap = %d, want 1", n)
	}
}

func TestStore_RejectsNewerSchema(t *testing.T) {
	path := t.TempDir() + "/sessions.db"

	st, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	raw := st.(*sqliteStore).db
	if _, err := raw.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	st.Close()

	if _, err := NewStore(path); err == nil ||
		!strings.Contains(err.Error(), "newer") {
		t.Fatalf("reopen newer schema = %v, want refusal", err)
	}
}

func TestStore_RedactsSecretsOnWrite(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	secret := "AKIAABCDEFGHIJKLMNOP"
	msgs := []Message{
		{Role: "user", Content: "run: " + secret},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "shell",
				Args: `{"command":"aws get secret` +
					`","token":"hunter2secretvalue"}`},
		}},
		{Role: "tool",
			Content:    "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			ToolCallID: "c1"},
	}
	if err := st.Append("s1", msgs); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Raw DB inspection: no secret patterns survive.
	db := st.(*sqliteStore).db
	rows, err := db.Query(
		`SELECT content FROM session_messages WHERE session_id='s1'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	secretRe := regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if secretRe.MatchString(s) ||
			strings.Contains(s, "wJalrXUtnFEMI") {
			t.Errorf("unredacted secret in row: %q", s)
		}
	}

	// Round-trip keeps redacted values and valid JSON args.
	sess, _, err := st.Load("s1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(
		sess.Messages()[0].Content, "[REDACTED]") {
		t.Errorf("content not redacted: %q",
			sess.Messages()[0].Content)
	}
}

func TestDeriveTitle(t *testing.T) {
	got := DeriveTitle("check nginx status\non prod")
	if got != "check nginx status" {
		t.Errorf("multiline = %q", got)
	}
	long := strings.Repeat("x", 100)
	if got := DeriveTitle(long); len([]rune(got)) != 60 {
		t.Errorf("long title len = %d, want 60", len([]rune(got)))
	}
	if got := DeriveTitle("   \n  "); got != "New session" {
		t.Errorf("blank = %q", got)
	}
}
