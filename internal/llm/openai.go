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
	"shmorby/internal/redact"
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
		Model:    model,
		Messages: buildOpenAIMessages(req),
		Tools:    buildOpenAITools(req.Tools),
	}

	// Reasoning models (o-series, gpt-5.x) require
	// max_completion_tokens; legacy models use max_tokens.
	if req.MaxTokens > 0 {
		if IsReasoningModel(model) {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
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
		Model:    model,
		Messages: buildOpenAIMessages(req),
		Tools:    buildOpenAITools(req.Tools),
		Stream:   true,
		// Request usage in the final streaming chunk for cost/audit.
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	// Reasoning models (o-series, gpt-5.x) require
	// max_completion_tokens; legacy models use max_tokens.
	if req.MaxTokens > 0 {
		if IsReasoningModel(model) {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
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
		// Limit and redact error body to prevent API key
		// echo-back from leaking into logs.
		bodyBytes, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
		httpResp.Body.Close()
		return nil, fmt.Errorf(
			"openai returned status %d: %s",
			httpResp.StatusCode, redact.SecretString(string(bodyBytes)),
		)
	}

	events := make(chan StreamEvent)
	go p.readSSEStream(ctx, httpResp.Body, events)
	return events, nil
}

// Reads SSE lines and emits StreamEvents until done or error.
// The ctx parameter allows the caller to cancel the stream; when
// cancelled, the goroutine closes the body to unblock the scanner
// and exits promptly to prevent goroutine leaks on cancel.
func (p *openaiProvider) readSSEStream(
	ctx context.Context,
	body io.ReadCloser,
	events chan<- StreamEvent,
) {
	defer close(events)
	defer func() { _ = body.Close() }()

	// Close the body when context is cancelled to unblock
	// scanner.Scan() which blocks on the HTTP response body.
	go func() {
		<-ctx.Done()
		body.Close()
	}()

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
		// Usage-only chunks (final streaming chunk with
		// stream_options.include_usage) have empty choices — surface
		// usage via a dedicated StreamEvent for audit/cost logging.
		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				events <- StreamEvent{
					Type: "usage",
					Content: fmt.Sprintf(
						`{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}`,
						chunk.Usage.PromptTokens,
						chunk.Usage.CompletionTokens,
						chunk.Usage.TotalTokens,
					),
				}
			}
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
		// Reasoning deltas from o-series and gpt-5.x models.
		// StreamEvent already has a "reasoning" type used by other
		// providers, so this wires it up for OpenAI.
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			events <- StreamEvent{
				Type:  "reasoning",
				Delta: *delta.Reasoning,
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
		// Limit and redact error body to prevent API key
		// echo-back from leaking into logs.
		bodyBytes, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
		return ModelInfo{}, fmt.Errorf(
			"openai returned status %d: %s",
			httpResp.StatusCode, redact.SecretString(string(bodyBytes)),
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

	// Populate MaxOutputTokens from the registry so the output cap
	// is applied automatically without requiring a config override.
	_mot, _ := matchOpenAIMaxOutputTokens(model)

	return ModelInfo{
		ContextWindow:   cw,
		MaxOutputTokens: _mot,
		SupportsTools:   true,
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
//
// Values verified against OpenAI platform docs (2026-08-07):
// https://platform.openai.com/docs/models
var openAIModelContextWindows = map[string]int{
	// GPT-5.6 series — current flagship (Aug 2026)
	"gpt-5.6-sol":   1050000,
	"gpt-5.6-terra": 1050000,
	"gpt-5.6-luna":  1050000,

	// GPT-5.5 / 5.4 series — 1M context window
	// (272K was the long-context pricing tier, NOT the window)
	"gpt-5.5":     1050000,
	"gpt-5.5-pro": 1050000,
	"gpt-5.4":     1050000,
	"gpt-5.4-pro": 1050000,

	// GPT-5.4 mini/nano — 400K context (272K max input)
	"gpt-5.4-mini": 400000,
	"gpt-5.4-nano": 400000,

	// GPT-5.3 codex — 400K context (estimated)
	"gpt-5.3-codex":       400000,
	"gpt-5.3-codex-spark": 400000,

	// GPT-5.2 / 5.1 — 400K context
	"gpt-5.2":      400000,
	"gpt-5.1":      400000,
	"gpt-5.1-mini": 400000,

	// GPT-5 — 272K context (correct)
	"gpt-5":      272000,
	"gpt-5-nano": 272000,

	// GPT-4.1 (1M context)
	"gpt-4.1":      1048576,
	"gpt-4.1-mini": 1048576,
	"gpt-4.1-nano": 1048576,

	// GPT-4o / o-series
	"o1":           200000,
	"o1-mini":      128000,
	"o1-preview":   128000,
	"o3":           200000,
	"o3-mini":      200000,
	"o4-mini":      200000,
	"o4-mini-high": 200000,
	"gpt-4o":       128000,
	"gpt-4o-mini":  128000,
	"gpt-4o-audio": 128000,

	// GPT-4 Turbo / Legacy
	"gpt-4-turbo":         128000,
	"gpt-4-turbo-preview": 128000,
	"gpt-4":               8192,
	"gpt-4-32k":           32768,

	// GPT-3.5 Turbo
	"gpt-3.5-turbo":     16385,
	"gpt-3.5-turbo-16k": 16385,
}

// Known max output tokens for OpenAI models.
// Used to populate ModelInfo.MaxOutputTokens so the output cap
// is applied automatically (previously only set via config override).
var openAIModelMaxOutputTokens = map[string]int{
	// GPT-5.6 / 5.5 / 5.4 — 128K max output
	"gpt-5.6-sol":   128000,
	"gpt-5.6-terra": 128000,
	"gpt-5.6-luna":  128000,
	"gpt-5.5":       128000,
	"gpt-5.5-pro":   128000,
	"gpt-5.4":       128000,
	"gpt-5.4-pro":   128000,
	"gpt-5.4-mini":  128000,
	"gpt-5.4-nano":  128000,
	"gpt-5.3-codex": 128000,
	"gpt-5.2":       128000,
	"gpt-5.1":       128000,
	"gpt-5.1-mini":  128000,
	"gpt-5":         128000,
	"gpt-5-nano":    128000,

	// GPT-4.1 — 32K max output
	"gpt-4.1":      32768,
	"gpt-4.1-mini": 32768,
	"gpt-4.1-nano": 32768,

	// o-series — 32K max output (o1/o3); 65K (o1-mini, o3-mini, o4-mini)
	"o1":      32768,
	"o1-mini": 65536,
	"o3":      32768,
	"o3-mini": 65536,
	"o4-mini": 65536,

	// GPT-4o — 16K max output
	"gpt-4o":      16384,
	"gpt-4o-mini": 16384,

	// GPT-4 Turbo — 4K max output
	"gpt-4-turbo": 4096,
	"gpt-4":       4096,

	// GPT-3.5 Turbo — 4K max output
	"gpt-3.5-turbo": 4096,
}

// matchOpenAIMaxOutputTokens returns the known max output tokens
// for a model using the same prefix-matching logic as
// matchOpenAIContextWindow.
func matchOpenAIMaxOutputTokens(model string) (int, bool) {
	if mot, ok := openAIModelMaxOutputTokens[model]; ok {
		return mot, true
	}

	best := 0
	bestMOT := 0
	found := false
	for key, mot := range openAIModelMaxOutputTokens {
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
		bestMOT = mot
		found = true
	}
	return bestMOT, found
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
			// Limit and redact error body to prevent API key
			// echo-back from leaking into logs.
			bodyBytes, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
			httpResp.Body.Close()
			return openaiResponse{}, fmt.Errorf(
				"openai returned status %d: %s",
				httpResp.StatusCode, redact.SecretString(string(bodyBytes)),
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
	Usage   *openaiUsage         `json:"usage,omitempty"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Content   *string            `json:"content"`
	Reasoning *string            `json:"reasoning"`
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
