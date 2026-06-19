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
		Model:    model,
		Messages: buildOpenAIMessages(req),
		Tools:    buildOpenAITools(req.Tools),
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
		cw = 8192
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
