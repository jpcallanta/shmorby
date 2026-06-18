package session

import (
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
	_ = s.Messages()
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
