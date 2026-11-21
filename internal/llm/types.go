package llm

import (
	"bytes"
	"fmt"
	"image"

	_ "image/jpeg"
	_ "image/png"
)

// ImageData is a single image attached to a message.
type ImageData struct {
	MediaType string // "image/png" (or "image/jpeg")
	Data      []byte // raw image bytes
}

// Message is a single message in a conversation with the LLM.
//
// For tool-role messages, ToolName holds the function name (provider-specific).
// ToolCallID follows the OpenAI convention; not all providers use it.
// Images ride as a sidecar field: only user-role messages carry them,
// and providers map them to their wire format at request time.
type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolName   string      `json:"tool_name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	Images     []ImageData `json:"images,omitempty"`
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
	Model     string    `json:"model"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	Tools     []ToolDef `json:"tools"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

// ChatResponse is returned from the LLM provider.
//
// Use Text() to access the assistant's text content; Message.Content
// is the canonical field (no separate top-level Text).
type ChatResponse struct {
	ID           string     `json:"id,omitempty"`
	Message      Message    `json:"message"`
	ToolCalls    []ToolCall `json:"tool_calls"`
	FinishReason string     `json:"finish_reason"`
	Usage        Usage      `json:"usage,omitempty"`
}

// Usage holds token counts from a chat response.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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
	SupportsVision  bool
}

// ModelSummary holds basic info about a model from the platform's
// list endpoint. Zero fields mean neither the platform nor a provider
// registry supplied that capability; callers must not render zero values.
type ModelSummary struct {
	ID            string
	ContextWindow int
}

// Renders a display line for a model summary; the context window is
// omitted when the platform did not supply one.
func (m ModelSummary) FormatSummary() string {
	s := m.ID
	if m.ContextWindow > 0 {
		s += fmt.Sprintf("  ctx %s", FormatTokenCount(m.ContextWindow))
	}

	return s
}

// Formats a token count for display, e.g. 42000 → "42k". Uses the
// same rounding as the TUI's formatter so /model and /context never
// show two different numbers for the same value.
func FormatTokenCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}

	return fmt.Sprintf("%d", n)
}

// Image token accounting constants (OpenAI-style "detail: auto"
// pricing): a flat base cost per image plus a per-tile cost for each
// 512x512 tile region the image covers. Ollama does not report image
// token usage, so the same conservative formula is used for every
// provider to avoid underestimating context usage.
const (
	imageBaseTokens    = 85
	imageTileTokens    = 170
	imageTileSize      = 512
	unknownImageTokens = 2200 // flat cost when dimensions are unavailable
)

// Returns a conservative token estimate for attached images.
// Dimensions are read from the image headers; when a header cannot
// be parsed a flat high-cost fallback is used.
func EstimateImageTokens(images []ImageData) int {
	total := 0
	for _, img := range images {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			total += unknownImageTokens

			continue
		}

		tilesX := (cfg.Width + imageTileSize - 1) / imageTileSize
		tilesY := (cfg.Height + imageTileSize - 1) / imageTileSize
		total += imageBaseTokens + tilesX*tilesY*imageTileTokens

	}

	return total
}
