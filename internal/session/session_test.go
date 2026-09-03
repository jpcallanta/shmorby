package session

import (
	"fmt"
	"testing"

	"shmorby/internal/llm"
)

// TestNew_CreatesEmptySession verifies New() returns an empty session.
func TestNew_CreatesEmptySession(t *testing.T) {
	s := New()

	msgs := s.Messages()
	if len(msgs) != 0 {
		t.Fatalf("want empty session, got %d messages", len(msgs))
	}
}

// TestAppend_AddsMessagesInOrder checks messages are stored in append order.
func TestAppend_AddsMessagesInOrder(t *testing.T) {
	s := New()

	s.Append("system", "You are a sysadmin")
	s.Append("user", "Hello")
	s.Append("assistant", "Hi there")
	s.Append("tool", "result data")

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}

	// Verify order is preserved.
	if msgs[0].Role != "system" || msgs[0].Content != "You are a sysadmin" {
		t.Errorf("message[0]: want system/You are a sysadmin, got %s/%s",
			msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "user" || msgs[1].Content != "Hello" {
		t.Errorf("message[1]: want user/Hello, got %s/%s",
			msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "Hi there" {
		t.Errorf("message[2]: want assistant/Hi there, got %s/%s",
			msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != "tool" || msgs[3].Content != "result data" {
		t.Errorf("message[3]: want tool/result data, got %s/%s",
			msgs[3].Role, msgs[3].Content)
	}
}

// TestReset_ClearsAllMessages verifies Reset() removes all messages.
func TestReset_ClearsAllMessages(t *testing.T) {
	s := New()

	s.Append("user", "Hello")
	s.Append("assistant", "Hi")

	if len(s.Messages()) != 2 {
		t.Fatalf("setup: want 2 messages, got %d", len(s.Messages()))
	}

	s.Reset()

	msgs := s.Messages()
	if len(msgs) != 0 {
		t.Fatalf("want 0 messages after reset, got %d", len(msgs))
	}
}

// TestReset_CanAppendAfterReset checks messages can be added after reset.
func TestReset_CanAppendAfterReset(t *testing.T) {
	s := New()

	s.Append("user", "first")
	s.Reset()
	s.Append("user", "second")

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("want 1 message after reset+append, got %d", len(msgs))
	}
	if msgs[0].Content != "second" {
		t.Fatalf("want content 'second', got %q", msgs[0].Content)
	}
}

// TestMessages_ReturnsCopy verifies the returned slice is a copy.
func TestMessages_ReturnsCopy(t *testing.T) {
	s := New()

	s.Append("user", "original")

	msgs1 := s.Messages()
	if len(msgs1) != 1 {
		t.Fatalf("setup: want 1 message, got %d", len(msgs1))
	}

	// Modify the returned slice.
	msgs1[0].Content = "modified"

	// Original should be unchanged.
	msgs2 := s.Messages()
	if msgs2[0].Content != "original" {
		t.Fatalf("want original unchanged, got %q", msgs2[0].Content)
	}
}

// TestMessages_ToolCallsDeepCopy verifies ToolCalls are deep-copied.
func TestMessages_ToolCallsDeepCopy(t *testing.T) {
	s := New()

	s.AppendAssistant("run", []llm.ToolCall{
		{ID: "c1", Name: "shell", Args: `{"cmd":"ls"}`},
	})

	msgs1 := s.Messages()
	// Mutate the returned ToolCalls.
	msgs1[0].ToolCalls[0].ID = "mutated"

	// Original should be unchanged.
	msgs2 := s.Messages()
	if msgs2[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("want original ToolCall.ID 'c1', got %q",
			msgs2[0].ToolCalls[0].ID)
	}
}

// TestAppendMessages_AppendsAll verifies multiple messages can be appended.
func TestAppendMessages_AppendsAll(t *testing.T) {
	s := New()

	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	s.AppendMessages(msgs)

	if len(s.Messages()) != 2 {
		t.Fatalf("want 2 messages, got %d", len(s.Messages()))
	}
	if s.Messages()[0].Content != "hello" {
		t.Errorf("want msg[0] content 'hello', got %q",
			s.Messages()[0].Content)
	}
	if s.Messages()[1].Content != "world" {
		t.Errorf("want msg[1] content 'world', got %q",
			s.Messages()[1].Content)
	}
}

// TestMessages_SafeConcurrentAccess verifies basic thread safety.
func TestMessages_SafeConcurrentAccess(t *testing.T) {
	s := New()

	// Append from multiple goroutines.
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(n int) {
			if n%2 == 0 {
				s.Append("user", "msg")
			} else {
				_ = s.Messages()
			}
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// Should not panic; count may vary due to concurrency.
	msgs := s.Messages()
	if len(msgs) == 0 {
		t.Error("expected at least one message after concurrent writes")
	}
}

// TestAppendTool_SetsCorrelationFields verifies AppendTool stores tool
// result metadata.
func TestAppendTool_SetsCorrelationFields(t *testing.T) {
	s := New()

	s.AppendTool("tool", "result output", "shell", "call_123")

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "tool" {
		t.Errorf("want role 'tool', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "result output" {
		t.Errorf("want content 'result output', got %q", msgs[0].Content)
	}
	if msgs[0].ToolName != "shell" {
		t.Errorf("want ToolName 'shell', got %q", msgs[0].ToolName)
	}
	if msgs[0].ToolCallID != "call_123" {
		t.Errorf("want ToolCallID 'call_123', got %q", msgs[0].ToolCallID)
	}
}

// TestAppendAssistant_WithToolCalls verifies assistant messages with tool
// calls are stored and round-tripped.
func TestAppendAssistant_WithToolCalls(t *testing.T) {
	s := New()

	tcs := []llm.ToolCall{
		{ID: "call_1", Name: "shell", Args: `{"command":"ls"}`},
	}
	s.AppendAssistant("Running...", tcs)

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("want role 'assistant', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "Running..." {
		t.Errorf("want content 'Running...', got %q", msgs[0].Content)
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("want 1 ToolCall, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("want ToolCall.ID 'call_1', got %q",
			msgs[0].ToolCalls[0].ID)
	}
	if msgs[0].ToolCalls[0].Name != "shell" {
		t.Errorf("want ToolCall.Name 'shell', got %q",
			msgs[0].ToolCalls[0].Name)
	}
}

// TestAppendAssistant_NilToolCalls verifies nil tool calls are allowed.
func TestAppendAssistant_NilToolCalls(t *testing.T) {
	s := New()

	s.AppendAssistant("Just text", nil)

	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("want role 'assistant', got %q", msgs[0].Role)
	}
	if msgs[0].ToolCalls != nil {
		t.Errorf("want nil ToolCalls, got %v", msgs[0].ToolCalls)
	}
}

// TestSession_Append_ExceedingMax_DropsOldest checks the message
// count is capped at MaxSessionMessages by dropping the oldest
// entries.
func TestSession_Append_ExceedingMax_DropsOldest(t *testing.T) {
	s := New()
	for i := 0; i < MaxSessionMessages+50; i++ {
		s.Append("user", fmt.Sprintf("m%d", i))
	}

	msgs := s.Messages()
	if len(msgs) != MaxSessionMessages {
		t.Fatalf("want %d messages, got %d", MaxSessionMessages,
			len(msgs))
	}
	if msgs[0].Content != "m50" {
		t.Errorf("want oldest dropped (first=m50), got %q",
			msgs[0].Content)
	}
	last := fmt.Sprintf("m%d", MaxSessionMessages+49)
	if msgs[len(msgs)-1].Content != last {
		t.Errorf("want newest kept (%s), got %q", last,
			msgs[len(msgs)-1].Content)
	}
}

// TestSession_AppendMessages_ExceedingMax_Trims checks bulk appends
// are also bounded.
func TestSession_AppendMessages_ExceedingMax_Trims(t *testing.T) {
	s := New()
	bulk := make([]Message, MaxSessionMessages+1)
	for i := range bulk {
		bulk[i] = Message{Role: "user", Content: "x"}
	}
	s.AppendMessages(bulk)
	if len(s.Messages()) != MaxSessionMessages {
		t.Errorf("want %d, got %d", MaxSessionMessages,
			len(s.Messages()))
	}
}

// TestSession_SetMessages_ExceedingMax_Trims checks the compressor
// entry point cannot smuggle an oversized history in.
func TestSession_SetMessages_ExceedingMax_Trims(t *testing.T) {
	s := New()
	msgs := make([]Message, MaxSessionMessages+7)
	for i := range msgs {
		msgs[i] = Message{Role: "user", Content: "y"}
	}
	s.SetMessages(msgs)
	if len(s.Messages()) != MaxSessionMessages {
		t.Errorf("want %d, got %d", MaxSessionMessages,
			len(s.Messages()))
	}
}

// TestSession_AppendAssistantAndTool_Bounded checks the remaining
// append paths trim as well.
func TestSession_AppendAssistantAndTool_Bounded(t *testing.T) {
	s := New()
	for i := 0; i < MaxSessionMessages+5; i++ {
		s.AppendAssistant("a", nil)
		s.AppendTool("tool", "t", "shell", "id")
	}
	if len(s.Messages()) != MaxSessionMessages {
		t.Errorf("want %d, got %d", MaxSessionMessages,
			len(s.Messages()))
	}
}

// TestSession_Sync_NoStore verifies Sync is a safe no-op when no
// store is bound (in-memory session).
func TestSession_Sync_NoStore(t *testing.T) {
	s := New()

	if err := s.Sync(); err != nil {
		t.Fatalf("Sync on in-memory session: want nil, got %v", err)
	}
}

// TestSession_Sync_NoRow verifies Sync is a safe no-op when a store
// is bound but no row has been created yet (no user messages).
func TestSession_Sync_NoRow(t *testing.T) {
	s, _ := boundSession(t)

	// Append only assistant messages — no row is created.
	s.AppendAssistant("thinking", nil)

	if err := s.Sync(); err != nil {
		t.Fatalf("Sync with no row: want nil, got %v", err)
	}
}

// TestSession_Sync_PersistsMeta verifies Sync flushes metadata to
// the store after a row exists.
func TestSession_Sync_PersistsMeta(t *testing.T) {
	s, st := boundSession(t)

	s.Append("user", "hello")
	s.AppendAssistant("hi", nil)

	// Mutate metadata without triggering a turn.
	s.UpdateMeta("openai", "gpt-4o", "code")

	if err := s.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// The stored row should reflect the updated metadata.
	m, ok := st.Latest("/work")
	if !ok {
		t.Fatal("row not found after Sync")
	}
	if m.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", m.Provider, "openai")
	}
	if m.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", m.Model, "gpt-4o")
	}
	if m.AgentMode != "code" {
		t.Errorf("AgentMode = %q, want %q", m.AgentMode, "code")
	}
}

// TestSession_UpdateMeta_PreservesTitle verifies UpdateMeta does
// not clobber the title set during row creation.
func TestSession_UpdateMeta_PreservesTitle(t *testing.T) {
	s, st := boundSession(t)

	s.Append("user", "check disk usage")

	s.UpdateMeta("openrouter", "claude-sonnet-4-20250514", "operate")

	if err := s.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	m, ok := st.Latest("/work")
	if !ok {
		t.Fatal("row not found after Sync")
	}
	if m.Title != "check disk usage" {
		t.Errorf("Title = %q, want %q", m.Title, "check disk usage")
	}
	if m.Provider != "openrouter" {
		t.Errorf("Provider = %q, want %q", m.Provider, "openrouter")
	}
}

// TestSession_Sync_FullExitSequence simulates the real exit path:
// Sync() must run while the store is still open, then Close() releases
// it. Confirms the stored row reflects metadata mutations made after
// the last turn.
func TestSession_Sync_FullExitSequence(t *testing.T) {
	s, st := boundSession(t)

	s.Append("user", "hello")
	s.AppendAssistant("hi", nil)

	// Simulate a /model switch without a subsequent turn.
	s.UpdateMeta("openai", "gpt-4o", "code")

	// Sync must flush while the store is open.
	if err := s.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify the store has the updated metadata.
	_, m, err := st.Load(s.ID())
	if err != nil {
		t.Fatalf("Load after Sync: %v", err)
	}
	if m.Provider != "openai" {
		t.Errorf("after Sync: Provider = %q, want %q", m.Provider, "openai")
	}
	if m.Model != "gpt-4o" {
		t.Errorf("after Sync: Model = %q, want %q", m.Model, "gpt-4o")
	}
	if m.AgentMode != "code" {
		t.Errorf("after Sync: AgentMode = %q, want %q", m.AgentMode, "code")
	}

	// Close must succeed without panicking on a post-Sync WAL state.
	if err := st.Close(); err != nil {
		t.Fatalf("Close after Sync: %v", err)
	}
}
