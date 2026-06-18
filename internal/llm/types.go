package llm

// Message is a single message in a conversation with the LLM.
//
// For tool-role messages, ToolName holds the function name (provider-specific).
// ToolCallID follows the OpenAI convention; not all providers use it.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolDef defines a tool the model may call.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ChatRequest is sent to the LLM provider.
type ChatRequest struct {
	Model    string    `json:"model"`
	System   string    `json:"system"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools"`
}

// ChatResponse is returned from the LLM provider.
//
// Use Text() to access the assistant's text content; Message.Content
// is the canonical field (no separate top-level Text).
type ChatResponse struct {
	Message      Message    `json:"message"`
	ToolCalls    []ToolCall `json:"tool_calls"`
	FinishReason string     `json:"finish_reason"`
}

// Returns the assistant's text content (Message.Content).
func (c ChatResponse) Text() string {
	return c.Message.Content
}

// StreamEvent represents an incremental event from a streaming response.
type StreamEvent struct {
	Type    string // "text", "reasoning", "tool-call", "tool-result", "done"
	Delta   string // incremental content (text/reasoning deltas)
	Content string // full content (tool-call args, tool-result output)
	ToolID  string // tool call identifier
	Tool    string // tool name
	Done    bool   // stream finished
}

// ModelInfo holds metadata about a model's capabilities.
type ModelInfo struct {
	ContextWindow   int
	MaxOutputTokens int
	SupportsTools   bool
}
