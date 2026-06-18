package session

import (
	"crypto/rand"
	"fmt"
	"sync"

	"shmorby/internal/llm"
)

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Message represents a single message in the conversation.
type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
}

// Session holds conversation messages for a single session.
// Safe for concurrent use; uses mutex for synchronization.
type Session struct {
	id       string
	mu       sync.Mutex
	messages []Message
}

// Returns a new empty Session.
func New() *Session {
	return &Session{
		id:       newID(),
		messages: make([]Message, 0),
	}
}

// Returns the session identifier.
func (s *Session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.id
}

// Replaces all messages. Used by context compression to rewrite history.
func (s *Session) SetMessages(msgs []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = msgs
}

// Adds a message to the session history.
func (s *Session) Append(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, Message{
		Role:    role,
		Content: content,
	})
}

// Appends multiple messages in a single locked operation.
func (s *Session) AppendMessages(msgs []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, msgs...)
}

// Adds an assistant message with optional tool calls to the session history.
// toolCalls may be nil for text-only responses.
func (s *Session) AppendAssistant(content string, toolCalls []llm.ToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	})
}

// Adds a tool result message with correlation fields.
func (s *Session) AppendTool(role, content, toolName, callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, Message{
		Role:       role,
		Content:    content,
		ToolName:   toolName,
		ToolCallID: callID,
	})
}

// Clears all messages from the session.
func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = s.messages[:0]
}

// Returns a deep copy of all messages in order.
func (s *Session) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Message, len(s.messages))
	copy(result, s.messages)

	// Deep-copy ToolCalls so returned messages cannot alias session state.
	for i, m := range s.messages {
		if len(m.ToolCalls) > 0 {
			result[i].ToolCalls = make([]llm.ToolCall, len(m.ToolCalls))
			copy(result[i].ToolCalls, m.ToolCalls)
		}
	}

	return result
}
