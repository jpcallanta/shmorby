package llm

import (
	"context"
	"fmt"
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

	// Reasoning models (o-series, gpt-5.x) require
	// max_completion_tokens; legacy models use max_tokens.
	if req.MaxTokens > 0 {
		if IsReasoningModel(model) {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
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
	var resp struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := fetchModelJSON(
		ctx, o.client, o.baseURL+"/v1/models", "openrouter",
		&resp, http.Header{"Authorization": {"Bearer " + o.apiKey}},
	); err != nil {
		return ModelInfo{}, err
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

// Fetches available models from the OpenRouter /v1/models endpoint,
// including context_length when the platform supplies it.
func (o *openRouterProvider) ListModels(
	ctx context.Context, _ string,
) ([]ModelSummary, error) {
	var resp struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := fetchModelJSON(
		ctx, o.client, o.baseURL+"/v1/models", "openrouter",
		&resp, http.Header{"Authorization": {"Bearer " + o.apiKey}},
	); err != nil {
		return nil, err
	}

	models := make([]ModelSummary, len(resp.Data))
	for i, m := range resp.Data {
		models[i] = ModelSummary{
			ID:            m.ID,
			ContextWindow: m.ContextLength,
		}
	}

	return models, nil
}
