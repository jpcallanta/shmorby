package llm

import "context"

// Provider is the interface for LLM backends.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	ModelInfo(ctx context.Context, model string) (ModelInfo, error)
}

// SessionProvider is an optional interface that providers may implement
// to receive a stable per-conversation session ID. The OpenCode Zen
// API requires the X-Opencode-Session header on every request.
type SessionProvider interface {
	SetSessionID(id string)
}
