package session

import (
	"testing"

	"shmorby/internal/llm"
)

// TestFindInput_Duplicate_ReturnsExisting verifies a repeated input
// ID resolves to the persisted user message.
func TestFindInput_Duplicate_ReturnsExisting(t *testing.T) {
	sess, st := boundSession(t)

	sess.AppendMessages([]Message{{
		Role:    "user",
		Content: "check disk usage",
		InputID: "input-1",
	}})

	first, ok := sess.FindInput("input-1")
	if !ok {
		t.Fatal("FindInput did not find input")
	}
	if first.Content != "check disk usage" {
		t.Errorf("content = %q, want %q",
			first.Content, "check disk usage")
	}

	second, ok := sess.FindInput("input-1")
	if !ok {
		t.Fatal("duplicate FindInput did not find input")
	}
	if second.Content != "check disk usage" {
		t.Errorf("duplicate content = %q, want %q",
			second.Content, "check disk usage")
	}

	msgs := sess.Messages()
	if len(msgs) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(msgs))
	}
	if msgs[0].InputID != "input-1" {
		t.Errorf("InputID = %q, want %q",
			msgs[0].InputID, "input-1")
	}

	loaded, _, err := st.Load(sess.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(loaded.Messages()); got != 1 {
		t.Errorf("stored messages = %d, want 1", got)
	}
}

// TestAppendMessages_DuplicateInput_SkipsTurn verifies a retried turn
// cannot append a second assistant reply after the user input exists.
func TestAppendMessages_DuplicateInput_SkipsTurn(t *testing.T) {
	sess, _ := boundSession(t)

	sess.AppendMessages([]Message{
		{Role: "user", Content: "first", InputID: "input-1"},
		{Role: "assistant", Content: "reply one"},
	})
	sess.AppendMessages([]Message{
		{Role: "user", Content: "retry", InputID: "input-1"},
		{Role: "assistant", Content: "reply two"},
	})

	msgs := sess.Messages()
	if len(msgs) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(msgs))
	}
	if msgs[1].Content != "reply one" {
		t.Errorf("assistant = %q, want %q",
			msgs[1].Content, "reply one")
	}
}

// TestStore_FindInput_RoundTrip verifies the idempotency key survives
// persistence.
func TestStore_FindInput_RoundTrip(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	if err := st.Append("s1", []Message{
		{Role: "user", Content: "hello", InputID: "input-9"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, ok, err := st.FindInput("s1", "input-9")
	if err != nil {
		t.Fatalf("FindInput: %v", err)
	}
	if !ok {
		t.Fatal("FindInput did not find input")
	}
	if got.Content != "hello" || got.InputID != "input-9" {
		t.Errorf("input mismatch: %+v", got)
	}

	if _, ok, err := st.FindInput("s1", "missing"); err != nil {
		t.Fatalf("FindInput missing: %v", err)
	} else if ok {
		t.Error("FindInput missing found a row")
	}
}

// TestSession_FindInputReply_ReturnsFinalAssistant verifies a retry
// gets the persisted assistant reply, not tool-call scaffolding or
// the user's own prompt.
func TestSession_FindInputReply_ReturnsFinalAssistant(t *testing.T) {
	sess, _ := boundSession(t)

	sess.AppendMessages([]Message{
		{Role: "user", Content: "check disk", InputID: "input-1"},
		{Role: "assistant", Content: "running df",
			ToolCalls: []llm.ToolCall{{
				ID: "call-1", Name: "shell",
			}}},
		{Role: "tool", Content: "disk is fine",
			ToolName: "shell", ToolCallID: "call-1"},
		{Role: "assistant", Content: "disk looks healthy"},
	})

	got, ok := sess.FindInputReply("input-1")
	if !ok {
		t.Fatal("FindInputReply did not find a reply")
	}
	if got.Content != "disk looks healthy" {
		t.Errorf("reply = %q, want %q",
			got.Content, "disk looks healthy")
	}

	if _, ok := sess.FindInputReply("missing"); ok {
		t.Error("FindInputReply found a reply for a missing input")
	}
}

// TestStore_FindInputReply_RoundTrip verifies the reply lookup
// survives persistence.
func TestStore_FindInputReply_RoundTrip(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "s1")

	if err := st.Append("s1", []Message{
		{Role: "user", Content: "hello", InputID: "input-9"},
		{Role: "assistant", Content: "hi there"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, ok, err := st.FindInputReply("s1", "input-9")
	if err != nil {
		t.Fatalf("FindInputReply: %v", err)
	}
	if !ok || got.Content != "hi there" {
		t.Errorf("reply = %+v, ok = %v", got, ok)
	}
}
