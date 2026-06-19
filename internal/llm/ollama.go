package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"shmorby/internal/config"
)

// ollamaRequest is the JSON body sent to /api/chat.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
}

type ollamaTool struct {
	Type     string         `json:"type"`
	Function ollamaFunction `json:"function"`
}

type ollamaFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	Type     string             `json:"type,omitempty"`
	Function ollamaCallFunction `json:"function"`
}

type ollamaCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ollamaResponse is the JSON body returned from /api/chat.
type ollamaResponse struct {
	Message    ollamaMessage `json:"message"`
	DoneReason string        `json:"done_reason"`
}

type ollamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
	cfg     config.Config
}

// Returns a new Ollama provider with a 120s HTTP client timeout.
func newOllamaProvider(baseURL, model string, cfg config.Config) *ollamaProvider {
	return &ollamaProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		cfg: cfg,
	}
}

// Returns the provider name "ollama".
func (o *ollamaProvider) Name() string {
	return "ollama"
}

// Sends a chat request to Ollama and returns the parsed response.
func (o *ollamaProvider) Chat(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}

	messages := buildMessages(req)
	tools := buildTools(req.Tools)

	body := ollamaRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
		Tools:    tools,
	}

	resp, err := o.doRequest(ctx, body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: %w", err)
	}

	return parseResponse(resp), nil
}

// Builds the Ollama message array from internal ChatRequest, prepending
// system prompt as a system-role message when non-empty.
func buildMessages(req ChatRequest) []ollamaMessage {
	messages := make([]ollamaMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		s := req.System
		messages = append(messages, ollamaMessage{
			Role:    "system",
			Content: &s,
		})
	}
	for _, m := range req.Messages {
		om := ollamaMessage{Role: m.Role}
		if m.Content != "" {
			om.Content = &m.Content
		}
		for _, tc := range m.ToolCalls {
			args := json.RawMessage(tc.Args)
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			om.ToolCalls = append(om.ToolCalls, ollamaToolCall{
				Type: "function",
				Function: ollamaCallFunction{
					Name:      tc.Name,
					Arguments: args,
				},
			})
		}
		if m.ToolName != "" {
			om.ToolName = m.ToolName
		}
		messages = append(messages, om)
	}
	return messages
}

// Converts internal tool definitions to Ollama's tools format.
func buildTools(tools []ToolDef) []ollamaTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ollamaTool, len(tools))
	for i, t := range tools {
		out[i] = ollamaTool{
			Type: "function",
			Function: ollamaFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}
	return out
}

// Posts to /api/chat with one retry on 5xx. Drains body before retry.
func (o *ollamaProvider) doRequest(
	ctx context.Context, body ollamaRequest,
) (ollamaResponse, error) {
	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ollamaResponse{}, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}

		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return ollamaResponse{}, fmt.Errorf("encode request: %w", err)
		}

		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost, o.baseURL+"/api/chat", &buf,
		)
		if err != nil {
			return ollamaResponse{}, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		httpResp, err := o.client.Do(req)
		if err != nil {
			return ollamaResponse{}, fmt.Errorf("http request: %w", err)
		}

		if httpResp.StatusCode >= 500 {
			lastErr = fmt.Errorf(
				"ollama returned status %d", httpResp.StatusCode,
			)
			// Drain before close for connection reuse.
			_, _ = io.Copy(io.Discard, httpResp.Body)
			httpResp.Body.Close()
			continue
		}
		if httpResp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			return ollamaResponse{}, fmt.Errorf(
				"ollama returned status %d: %s",
				httpResp.StatusCode, string(bodyBytes),
			)
		}

		var ollamaResp ollamaResponse
		if err := json.NewDecoder(httpResp.Body).Decode(&ollamaResp); err != nil {
			httpResp.Body.Close()
			return ollamaResponse{}, fmt.Errorf("decode response: %w", err)
		}
		httpResp.Body.Close()

		return ollamaResp, nil
	}

	return ollamaResponse{},
		fmt.Errorf("ollama request failed after retry: %w", lastErr)
}

// Converts an Ollama response into the internal ChatResponse format.
func parseResponse(resp ollamaResponse) ChatResponse {
	cr := ChatResponse{
		FinishReason: resp.DoneReason,
		Message: Message{
			Role: resp.Message.Role,
		},
	}

	if resp.Message.Content != nil {
		cr.Message.Content = *resp.Message.Content
	}

	// Ollama does not provide tool-call IDs; Name is the correlation key.
	for i, tc := range resp.Message.ToolCalls {
		call := ToolCall{
			ID:   fmt.Sprintf("call_%d", i),
			Name: tc.Function.Name,
			Args: string(tc.Function.Arguments),
		}
		cr.ToolCalls = append(cr.ToolCalls, call)
	}
	cr.Message.ToolCalls = cr.ToolCalls

	return cr
}

// Fetches model info from the Ollama /api/show endpoint.
func (o *ollamaProvider) fetchModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	body := map[string]string{"name": model}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return ModelInfo{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		o.baseURL+"/api/show", &buf,
	)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := o.client.Do(req)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(httpResp.Body)
		return ModelInfo{}, fmt.Errorf(
			"ollama returned status %d: %s",
			httpResp.StatusCode, string(bodyBytes),
		)
	}

	var resp struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return ModelInfo{}, fmt.Errorf("decode response: %w", err)
	}

	cw := 8192
	if ctxLen, ok := resp.ModelInfo["context_length"]; ok {
		if v, ok := ctxLen.(float64); ok {
			cw = int(v)
		}
	}

	return ModelInfo{
		ContextWindow: cw,
		SupportsTools: true,
	}, nil
}

// Returns model info via FetchModelInfo with provider config.
func (o *ollamaProvider) ModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	if model == "" {
		model = o.model
	}
	return FetchModelInfo(ctx, o, model, o.cfg)
}

// Streams a chat response (not yet implemented for Ollama).
func (o *ollamaProvider) ChatStream(
	_ context.Context, _ ChatRequest,
) (<-chan StreamEvent, error) {
	return nil, fmt.Errorf("ollama: streaming not yet supported")
}
