package session

import (
	"strings"
	"testing"

	"shmorby/internal/llm"
)

// boundSession returns a session bound to a fresh in-memory store
// with the given start settings.
func boundSession(t *testing.T) (*Session, Store) {
	t.Helper()

	st := newTestStore(t)
	sess := New()
	sess.BindStore(st, Meta{
		ID:        sess.ID(),
		Directory: "/work",
		AgentMode: "operate",
		Provider:  "ollama",
		Model:     "llama3.2",
	})

	return sess, st
}

func TestBindStore_LazyRowOnFirstUserMessage(t *testing.T) {
	sess, st := boundSession(t)

	// Assistant-only appends before any user message must not
	// create a row (no empty-session clutter).
	sess.AppendAssistant("hmm", nil)
	if _, ok := st.Latest("/work"); ok {
		t.Fatal("row created before first user message")
	}

	// The first user message creates the row with a derived title
	// and the full turn is persisted.
	sess.AppendMessages([]Message{
		{Role: "user", Content: "check disk usage\nplease"},
		{Role: "assistant", Content: "done"},
	})
	m, ok := st.Latest("/work")
	if !ok {
		t.Fatal("row not created after user message")
	}
	if m.Title != "check disk usage" {
		t.Errorf("Title = %q, want %q", m.Title, "check disk usage")
	}
}

func TestWriteThrough_LoadRoundTrip(t *testing.T) {
	sess, st := boundSession(t)

	sess.Append("user", "hello")
	sess.AppendAssistant("hi", []llm.ToolCall{
		{ID: "c1", Name: "shell", Args: `{"command":"ls"}`},
	})
	sess.AppendTool("tool", "output", "shell", "c1")

	loaded, m, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Provider != "ollama" || m.Directory != "/work" {
		t.Errorf("meta mismatch: %+v", m)
	}
	got := loaded.Messages()
	want := sess.Messages()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Role != want[i].Role ||
			got[i].Content != want[i].Content ||
			got[i].ToolCallID != want[i].ToolCallID {
			t.Errorf("msg %d mismatch: %+v vs %+v", i, got[i], want[i])
		}
	}
	wantArgs := `{"command":"ls"}`
	if len(got[1].ToolCalls) != 1 ||
		got[1].ToolCalls[0].Args != wantArgs {
		t.Errorf("tool calls mismatch: %+v", got[1].ToolCalls)
	}
}

func TestSetMessages_PersistsRewrite(t *testing.T) {
	sess, st := boundSession(t)

	sess.AppendMessages([]Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "four"},
	})

	// Compression-style rewrite: stub + recent tail.
	sess.SetMessages([]Message{
		{Role: "assistant", Content: "[compressed] summary"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "four"},
	})

	loaded, _, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loaded.Messages()
	if len(got) != 3 || got[0].Content != "[compressed] summary" {
		t.Fatalf("store did not mirror rewrite: %+v", got)
	}
}

func TestReset_MirrorsEmptyWindow(t *testing.T) {
	sess, st := boundSession(t)

	sess.Append("user", "secret plans")
	loaded, _, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Messages()) != 1 {
		t.Fatalf("message not persisted")
	}

	sess.Reset()

	loaded, _, err = st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load after reset: %v", err)
	}
	if len(loaded.Messages()) != 0 {
		t.Errorf("store kept %d messages after reset",
			len(loaded.Messages()))
	}

	// The row survives, so the session id keeps working.
	if _, ok := st.Latest("/work"); !ok {
		t.Error("row vanished after reset")
	}
}

// Review regression: resume a session that was reset before
// the process exited (empty window, but the row exists). A
// subsequent assistant-only append must still persist because the
// row exists, instead of silently waiting for a user message.
func TestResumeAfterReset_EmptyRowStillPersists(t *testing.T) {
	sess, st := boundSession(t)
	sess.Append("user", "hello")
	sess.Reset()

	// Simulate restart: Load returns the empty-but-existing row
	// (LastSeq=0, MessageCount=0); rebinding must know the row
	// exists.
	restored, m, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.LastSeq != 0 || m.MessageCount != 0 {
		t.Fatalf("precondition: %+v", m)
	}
	restored.BindStore(st, m)

	restored.AppendAssistant("assistant before next user turn", nil)

	again, _, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load after assistant-only append: %v", err)
	}
	if len(again.Messages()) != 1 {
		t.Fatalf("assistant-only write not persisted: %+v",
			again.Messages())
	}
}

func TestTrim_ResyncsStoreMirror(t *testing.T) {
	sess, st := boundSession(t)

	// Fill exactly to the cap, then append one more: the oldest
	// message drops from the live window and the store must
	// mirror the trimmed window.
	msgs := make([]Message, 0, MaxSessionMessages+1)
	msgs = append(msgs, Message{Role: "user", Content: "first-message"})
	for i := 1; i < MaxSessionMessages; i++ {
		msgs = append(msgs, Message{
			Role: "assistant", Content: strings.Repeat("m", i)})
	}
	sess.AppendMessages(msgs)

	loaded, _, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load before overflow: %v", err)
	}
	if len(loaded.Messages()) != MaxSessionMessages {
		t.Fatalf("pre-overflow store len = %d, want %d",
			len(loaded.Messages()), MaxSessionMessages)
	}

	sess.Append("user", "overflow")

	loaded, m, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load after overflow: %v", err)
	}
	got := loaded.Messages()
	if len(got) != MaxSessionMessages {
		t.Fatalf("store len = %d, want %d", len(got), MaxSessionMessages)
	}
	if got[0].Content == "first-message" {
		t.Error("trimmed message still mirrored in store")
	}
	if got[len(got)-1].Content != "overflow" {
		t.Error("newest message missing from store")
	}
	if m.LastSeq != MaxSessionMessages {
		t.Errorf("LastSeq = %d, want %d", m.LastSeq, MaxSessionMessages)
	}
}

func TestUnboundSession_WritesNothing(t *testing.T) {
	// No store bound (session.enabled: false / subagents): the
	// session must behave exactly as before.
	sess := New()
	sess.Append("user", "AKIAABCDEFGHIJKLMNOP")
	sess.AppendAssistant("x", nil)
	if len(sess.Messages()) != 2 {
		t.Fatalf("len = %d, want 2", len(sess.Messages()))
	}
	if sess.Title() != "" {
		t.Errorf("unbound Title = %q, want empty", sess.Title())
	}
	if err := sess.SetTitle("ignored"); err != nil {
		t.Errorf("SetTitle on unbound session: %v", err)
	}
}

func TestSetTitle_PersistsRename(t *testing.T) {
	sess, st := boundSession(t)
	sess.Append("user", "hello")

	if err := sess.SetTitle("incident triage"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	if _, m, err := st.Load(sess.ID()); err != nil ||
		m.Title != "incident triage" {
		t.Fatalf("title not persisted: %+v (%v)", m, err)
	}
	if sess.Title() != "incident triage" {
		t.Errorf("Title() = %q", sess.Title())
	}
}

func TestResumeFromStore_ContinuesAppend(t *testing.T) {
	sess, st := boundSession(t)
	sess.Append("user", "hello")
	sess.AppendAssistant("hi there", nil)

	// Simulate restart: Load, replay into a session with the
	// stored id, rebind, and keep talking.
	restored, m, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	restored.BindStore(st, m)

	restored.Append("user", "and one more thing")

	final, fm, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load after resume: %v", err)
	}
	msgs := final.Messages()
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[2].Content != "and one more thing" {
		t.Errorf("tail mismatch: %+v", msgs[2])
	}
	if fm.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", fm.MessageCount)
	}
}

func TestWriteThrough_RedactsPersistedContent(t *testing.T) {
	sess, st := boundSession(t)

	// The live window keeps the original text (the model must see
	// what the operator typed); persistence redacts.
	sess.Append("user", "rotate token sk-abcdefghijklmnopqrstuvwxyz012345")
	sess.AppendTool("tool", "AWS_SECRET: AKIAABCDEFGHIJKLMNOP", "shell", "c1")

	raw := "sk-abcdefghijklmnopqrstuvwxyz012345"
	if !strings.Contains(sess.Messages()[0].Content, raw) {
		t.Error("live window content was mutated; want raw text in memory")
	}

	loaded, _, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, m := range loaded.Messages() {
		if strings.Contains(m.Content, "AKIAABCDEFGHIJKLMNOP") ||
			strings.Contains(m.Content, "sk-abcdefghijklmnopqrstuvwxyz012345") {
			t.Errorf("secret persisted: %q", m.Content)
		}
	}
}
