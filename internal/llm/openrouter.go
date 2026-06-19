package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"shmorby/internal/config"
)

// openRouterProvider sends requests to the OpenRouter API.
type openRouterProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
	cfg     config.Config
}

// Returns a new OpenRouter provider with a 120s HTTP client timeout.
//
// The baseURL is normalized — any trailing /v1 is stripped so that the
// shared doOpenAIRequest appends /v1 without doubling.
func newOpenRouterProvider(
	baseURL, apiKey, model string,
	cfg config.Config,
) *openRouterProvider {
	return &openRouterProvider{
		baseURL: normalizeBaseURL(baseURL),
		apiKey:  apiKey,
		model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		cfg: cfg,
	}
}

// Returns the provider name "openrouter".
func (o *openRouterProvider) Name() string {
	return "openrouter"
}

// Sends a chat request to OpenRouter and returns the parsed response.
func (o *openRouterProvider) Chat(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}

	body := openaiRequest{
		Model:    model,
		Messages: buildOpenAIMessages(req),
		Tools:    buildOpenAITools(req.Tools),
	}

	resp, err := doOpenAIRequest(
		ctx, o.client, o.baseURL, o.apiKey, "openrouter", body,
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openrouter chat: %w", err)
	}

	cr, err := parseOpenAIResponse(resp)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openrouter chat: %w", err)
	}
	return cr, nil
}

// Fetches model info from the OpenRouter /api/v1/models endpoint.
func (o *openRouterProvider) fetchModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		o.baseURL+"/v1/models", nil,
	)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	httpResp, err := o.client.Do(req)
	if err != nil {
		return ModelInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(httpResp.Body)
		return ModelInfo{}, fmt.Errorf(
			"openrouter returned status %d: %s",
			httpResp.StatusCode, string(bodyBytes),
		)
	}

	var resp struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return ModelInfo{}, fmt.Errorf("decode response: %w", err)
	}

	for _, m := range resp.Data {
		if m.ID == model {
			cw := m.ContextLength
			if cw == 0 {
				cw = 8192
			}
			return ModelInfo{
				ContextWindow: cw,
				SupportsTools: true,
			}, nil
		}
	}

	return ModelInfo{}, fmt.Errorf("model %q not found", model)
}

// Returns model info via FetchModelInfo with provider config.
func (o *openRouterProvider) ModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	if model == "" {
		model = o.model
	}
	return FetchModelInfo(ctx, o, model, o.cfg)
}

// Streams a chat response (not yet implemented for OpenRouter).
func (o *openRouterProvider) ChatStream(
	_ context.Context, _ ChatRequest,
) (<-chan StreamEvent, error) {
	return nil, fmt.Errorf("openrouter: streaming not yet supported")
}
