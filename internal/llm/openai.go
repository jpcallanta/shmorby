package llm

import (
	"bufio"
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

// openaiProvider sends requests to the OpenAI API.
type openaiProvider struct {
	baseURL string
	apiKey  string
	orgID   string
	model   string
	client  *http.Client
	cfg     config.Config
}

// Returns a new OpenAI provider with the given timeout.
//
// The baseURL is normalized — any trailing /v1 is stripped so that the
// request path does not double /v1.
func newOpenAIProvider(
	baseURL, apiKey, orgID, model string,
	timeoutSec int,
	cfg config.Config,
) *openaiProvider {
	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &openaiProvider{
		baseURL: normalizeBaseURL(baseURL),
		apiKey:  apiKey,
		orgID:   orgID,
		model:   model,
		client: &http.Client{
			Timeout: timeout,
		},
		cfg: cfg,
	}
}

// Returns the provider name "openai".
func (p *openaiProvider) Name() string {
	return "openai"
}

// Sends a chat request to OpenAI and returns the parsed response.
func (p *openaiProvider) Chat(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	body := openaiRequest{
		Model:     model,
		Messages:  buildOpenAIMessages(req),
		Tools:     buildOpenAITools(req.Tools),
		MaxTokens: req.MaxTokens,
	}

	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai chat: %w", err)
	}

	cr, err := parseOpenAIResponse(resp)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai chat: %w", err)
	}
	return cr, nil
}

// Sends a streaming chat request and returns a channel of StreamEvents.
func (p *openaiProvider) ChatStream(
	ctx context.Context, req ChatRequest,
) (<-chan StreamEvent, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	body := openaiRequest{
		Model:     model,
		Messages:  buildOpenAIMessages(req),
		Tools:     buildOpenAITools(req.Tools),
		Stream:    true,
		MaxTokens: req.MaxTokens,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", &buf,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if httpResp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf(
			"openai returned status %d: %s",
			httpResp.StatusCode, string(bodyBytes),
		)
	}

	events := make(chan StreamEvent)
	go p.readSSEStream(httpResp.Body, events)
	return events, nil
}

// Reads SSE lines and emits StreamEvents until done or error.
func (p *openaiProvider) readSSEStream(
	body io.ReadCloser,
	events chan<- StreamEvent,
) {
	defer close(events)
	defer func() { _ = body.Close() }()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			events <- StreamEvent{Type: "done", Done: true}
			return
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			events <- StreamEvent{
				Type:  "error",
				Delta: fmt.Sprintf("parse error: %v", err),
			}
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		finish := chunk.Choices[0].FinishReason

		if delta.Content != nil && *delta.Content != "" {
			events <- StreamEvent{
				Type:  "text",
				Delta: *delta.Content,
			}
		}
		for _, tc := range delta.ToolCalls {
			args := ""
			if tc.Function.Arguments != "" {
				args = tc.Function.Arguments
			}
			events <- StreamEvent{
				Type:    "tool-call",
				ToolID:  tc.ID,
				Tool:    tc.Function.Name,
				Content: args,
			}
		}
		if finish != "" {
			events <- StreamEvent{Type: "done", Done: true}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		events <- StreamEvent{
			Type:  "error",
			Delta: fmt.Sprintf("stream read error: %v", err),
		}
	}
}

// Fetches model info from the OpenAI /v1/models endpoint.
func (p *openaiProvider) fetchModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		p.baseURL+"/v1/models/"+model, nil,
	)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("create request: %w", err)
	}
	p.setHeaders(req)

	httpResp, err := p.client.Do(req)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(httpResp.Body)
		return ModelInfo{}, fmt.Errorf(
			"openai returned status %d: %s",
			httpResp.StatusCode, string(bodyBytes),
		)
	}

	// The real OpenAI /v1/models/{model} endpoint does NOT return
	// context_length or max_context_length. The struct fields will
	// always decode to 0. We check them for forward-compat with
	// any future API change, then fall back to the builtin registry.
	var resp struct {
		Data struct {
			ContextLength    int `json:"context_length"`
			MaxContextLength int `json:"max_context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return ModelInfo{}, fmt.Errorf("decode response: %w", err)
	}

	cw := resp.Data.ContextLength
	if cw == 0 {
		cw = resp.Data.MaxContextLength
	}
	if cw == 0 {
		cw, _ = matchOpenAIContextWindow(model)
	}
	if cw == 0 {
		// Return error so FetchModelInfo falls through to config
		// override → ErrModelInfoFallback instead of silently
		// caching 8192 as authoritative.
		return ModelInfo{}, fmt.Errorf(
			"model %q context length unknown", model,
		)
	}

	return ModelInfo{
		ContextWindow: cw,
		SupportsTools: true,
	}, nil
}

// Returns model info via FetchModelInfo with provider config.
func (p *openaiProvider) ModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	if model == "" {
		model = p.model
	}
	return FetchModelInfo(ctx, p, model, p.cfg)
}

// Known context windows for OpenAI models.
// The /v1/models API does not return context_length, so we maintain
// this map as our source of truth.
//
// Snapshot/date-stamped model IDs (e.g. gpt-4o-mini-2024-07-18)
// resolve via the longest registry key that is a prefix of the
// requested name — see matchOpenAIContextWindow.
var openAIModelContextWindows = map[string]int{
	// GPT-5 series — keep in sync with zenModelContextWindows
	// (opencode_zen.go) to avoid drift.
	"gpt-5.5":             272000,
	"gpt-5.5-pro":         272000,
	"gpt-5.4":             272000,
	"gpt-5.4-pro":         272000,
	"gpt-5.4-mini":        272000,
	"gpt-5.4-nano":        272000,
	"gpt-5.3-codex":       272000,
	"gpt-5.3-codex-spark": 272000,
	"gpt-5.2":             272000,
	"gpt-5.2-codex":       272000,
	"gpt-5.1":             272000,
	"gpt-5.1-codex":       272000,
	"gpt-5.1-codex-max":   272000,
	"gpt-5.1-codex-mini":  272000,
	"gpt-5":               272000,
	"gpt-5-codex":         272000,
	"gpt-5-nano":          272000,

	// GPT-4.1 (1M context)
	"gpt-4.1":      1048576,
	"gpt-4.1-mini": 1048576,
	"gpt-4.1-nano": 1048576,

	// GPT-4o / o-series
	"o1":                200000,
	"o1-mini":           128000,
	"o1-preview":        128000,
	"o3":                200000,
	"o3-mini":           200000,
	"o4-mini":           200000,
	"o4-mini-high":      200000,
	"gpt-4o":            128000,
	"gpt-4o-mini":       128000,
	"gpt-4o-audio":      128000,
	"gpt-4o-search":     128000,
	"chatgpt-4o-latest": 128000,

	// GPT-4 Turbo / Legacy
	"gpt-4-turbo":         128000,
	"gpt-4-turbo-preview": 128000,
	"gpt-4":               8192,
	"gpt-4-32k":           32768,

	// GPT-3.5 Turbo
	"gpt-3.5-turbo":     16385,
	"gpt-3.5-turbo-16k": 16385,
}

// matchOpenAIContextWindow returns the known context window for a
// model. An exact registry key wins; otherwise the longest key that
// is a prefix of the model ending at a name boundary (dash, dot, or
// underscore) is used, so snapshot IDs like gpt-4o-mini-2024-07-18
// resolve to gpt-4o-mini (128000). Returns false when no key matches.
func matchOpenAIContextWindow(model string) (int, bool) {
	if cw, ok := openAIModelContextWindows[model]; ok {
		return cw, true
	}

	best := 0
	bestCW := 0
	found := false
	for key, cw := range openAIModelContextWindows {
		if len(key) <= best || len(key) >= len(model) {
			continue
		}
		if !strings.HasPrefix(model, key) {
			continue
		}
		if c := model[len(key)]; c != '-' && c != '.' && c != '_' {
			continue
		}
		best = len(key)
		bestCW = cw
		found = true
	}
	return bestCW, found
}

// Sends an OpenAI request with retries for 429 and 5xx errors.
func (p *openaiProvider) doRequest(
	ctx context.Context, body openaiRequest,
) (openaiResponse, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return openaiResponse{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return openaiResponse{}, fmt.Errorf("encode request: %w", err)
		}

		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost,
			p.baseURL+"/v1/chat/completions", &buf,
		)
		if err != nil {
			return openaiResponse{}, fmt.Errorf("create request: %w", err)
		}
		p.setHeaders(req)

		httpResp, err := p.client.Do(req)
		if err != nil {
			return openaiResponse{}, fmt.Errorf("http request: %w", err)
		}

		if httpResp.StatusCode == 401 {
			_, _ = io.Copy(io.Discard, httpResp.Body)
			httpResp.Body.Close()
			return openaiResponse{}, fmt.Errorf("invalid API key")
		}
		if httpResp.StatusCode == 429 ||
			httpResp.StatusCode == 500 ||
			httpResp.StatusCode == 502 ||
			httpResp.StatusCode == 503 {
			lastErr = fmt.Errorf(
				"openai returned status %d", httpResp.StatusCode,
			)
			_, _ = io.Copy(io.Discard, httpResp.Body)
			httpResp.Body.Close()
			continue
		}
		if httpResp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			return openaiResponse{}, fmt.Errorf(
				"openai returned status %d: %s",
				httpResp.StatusCode, string(bodyBytes),
			)
		}

		var openaiResp openaiResponse
		if err := json.NewDecoder(httpResp.Body).Decode(&openaiResp); err != nil {
			httpResp.Body.Close()
			return openaiResponse{}, fmt.Errorf("decode response: %w", err)
		}
		httpResp.Body.Close()

		return openaiResp, nil
	}

	return openaiResponse{},
		fmt.Errorf("openai request failed after retries: %w", lastErr)
}

// Sets Authorization, Content-Type, and optional OpenAI-Organization headers.
func (p *openaiProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if p.orgID != "" {
		req.Header.Set("OpenAI-Organization", p.orgID)
	}
}

// Streaming chunk from OpenAI SSE response.
type openaiStreamChunk struct {
	Choices []openaiStreamChoice `json:"choices"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Content   *string            `json:"content"`
	ToolCalls []openaiStreamTool `json:"tool_calls"`
}

type openaiStreamTool struct {
	ID       string                   `json:"id"`
	Type     string                   `json:"type"`
	Function openaiStreamToolFunction `json:"function"`
}

type openaiStreamToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
