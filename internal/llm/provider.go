package llm

import "context"

// Provider is the interface for LLM backends.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	ModelInfo(ctx context.Context, model string) (ModelInfo, error)
}
