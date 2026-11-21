package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"shmorby/internal/config"
)

const (
	zenModelPrefix = "opencode/"
	goModelPrefix  = "opencode-go/"
)

// opencodeZenProvider sends requests to the OpencodeZen API.
type opencodeZenProvider struct {
	baseURL   string
	apiKey    string
	model     string
	client    *http.Client
	cfg       config.Config
	sessionID string // stable per-conversation ID for X-Opencode-Session header
}

// Returns a new OpencodeZen provider with a 120s HTTP client timeout.
//
// The baseURL is normalized — any trailing /v1 is stripped so that the
// shared doOpenAIRequest appends /v1 without doubling.
func newOpencodeZenProvider(
	baseURL, apiKey, model string,
	cfg config.Config,
) *opencodeZenProvider {
	return &opencodeZenProvider{
		baseURL: normalizeBaseURL(baseURL),
		apiKey:  apiKey,
		model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		cfg: cfg,
	}
}

// Returns the provider name "opencode_zen".
func (o *opencodeZenProvider) Name() string {
	return "opencode_zen"
}

// SetSessionID stores the conversation session ID for the
// X-Opencode-Session header. Must be called before Chat or ModelInfo
// requests. The empty string disables the header.
func (o *opencodeZenProvider) SetSessionID(id string) {
	o.sessionID = id
}

// zenSessionHeader returns an http.Header containing the
// X-Opencode-Session value when set, or nil otherwise.
func (o *opencodeZenProvider) zenSessionHeader() http.Header {
	if o.sessionID == "" {
		return nil
	}
	return http.Header{"X-Opencode-Session": {o.sessionID}}
}

// resolveModel returns the effective base URL and stripped model name
// based on the model prefix:
//
//	opencode-go/<name> → o.baseURL/go + <name>  (Go subscription endpoint)
//	opencode/<name>    → o.baseURL     + <name>  (Zen pay-as-you-go endpoint)
//	<name>             → o.baseURL     + <name>  (unprefixed, backwards compatible)
func (o *opencodeZenProvider) resolveModel(model string) (baseURL, effectiveModel string) {
	if strings.HasPrefix(model, goModelPrefix) {
		return strings.TrimRight(o.baseURL, "/") + "/go", strings.TrimPrefix(model, goModelPrefix)
	}
	if strings.HasPrefix(model, zenModelPrefix) {
		return o.baseURL, strings.TrimPrefix(model, zenModelPrefix)
	}
	return o.baseURL, model
}

// Sends a chat request to OpencodeZen and returns the parsed response.
func (o *opencodeZenProvider) Chat(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}

	baseURL, effectiveModel := o.resolveModel(model)

	body := openaiRequest{
		Model:    effectiveModel,
		Messages: buildOpenAIMessages(req),
		Tools:    buildOpenAITools(req.Tools),
	}

	// Reasoning models (o-series, gpt-5.x) require
	// max_completion_tokens; legacy models use max_tokens.
	if req.MaxTokens > 0 {
		if IsReasoningModel(effectiveModel) {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
	}

	resp, err := doOpenAIRequest(
		ctx, o.client, baseURL, o.apiKey, "opencode_zen", body,
		o.zenSessionHeader(),
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("opencode_zen chat: %w", err)
	}

	cr, err := parseOpenAIResponse(resp)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("opencode_zen chat: %w", err)
	}
	return cr, nil
}

// Known context windows for Zen models.
// The /v1/models API does not return context_length, so we maintain
// this map as our source of truth.
var zenModelContextWindows = map[string]int{
	// DeepSeek V4
	"deepseek-v4-pro":        1000000,
	"deepseek-v4-flash":      1000000,
	"deepseek-v4-flash-free": 1000000,

	// Anthropic Claude
	"claude-fable-5":    200000,
	"claude-opus-4-8":   200000,
	"claude-opus-4-7":   200000,
	"claude-opus-4-6":   200000,
	"claude-opus-4-5":   200000,
	"claude-opus-4-1":   200000,
	"claude-sonnet-4-6": 200000,
	"claude-sonnet-4-5": 200000,
	"claude-sonnet-4":   200000,
	"claude-haiku-4-5":  200000,

	// Google Gemini
	"gemini-3.5-flash": 1048576,
	"gemini-3.1-pro":   2097152,
	"gemini-3-flash":   1048576,

	// OpenAI GPT-5 series — keep in sync with
	// openAIModelContextWindows (openai.go) to avoid drift.
	// Values verified against OpenAI platform docs (2026-08-07).

	// GPT-5.6 — current flagship (1M context, 128K output)
	"gpt-5.6-sol":   1050000,
	"gpt-5.6-terra": 1050000,
	"gpt-5.6-luna":  1050000,

	// GPT-5.5 / 5.4 — 1M context (272K was pricing tier, not window)
	"gpt-5.5":     1050000,
	"gpt-5.5-pro": 1050000,
	"gpt-5.4":     1050000,
	"gpt-5.4-pro": 1050000,

	// GPT-5.4 mini/nano — 400K context
	"gpt-5.4-mini": 400000,
	"gpt-5.4-nano": 400000,

	// GPT-5.3 codex — 400K context (estimated)
	"gpt-5.3-codex":       400000,
	"gpt-5.3-codex-spark": 400000,

	// GPT-5.2 / 5.1 — 400K context
	"gpt-5.2":      400000,
	"gpt-5.1":      400000,
	"gpt-5.1-mini": 400000,

	// GPT-5 — 272K context
	"gpt-5":      272000,
	"gpt-5-nano": 272000,

	// Qwen
	"qwen3.7-max":  1000000,
	"qwen3.7-plus": 1000000,
	"qwen3.6-plus": 1000000,
	"qwen3.5-plus": 1000000,

	// GLM
	"glm-5.1": 128000,
	"glm-5":   128000,

	// MiniMax
	"minimax-m3":   1000000,
	"minimax-m2.7": 1000000,
	"minimax-m2.5": 1000000,

	// Kimi
	"kimi-k2.7-code": 128000,
	"kimi-k2.6":      128000,
	"kimi-k2.5":      128000,

	// Grok
	"grok-build-0.1": 1000000,

	// Big Pickle
	"big-pickle": 1000000,

	// MiMo
	"mimo-v2.5":     128000,
	"mimo-v2.5-pro": 1000000,

	// Free tier models
	"mimo-v2.5-free":        128000,
	"north-mini-code-free":  128000,
	"nemotron-3-ultra-free": 128000,
}

// Vision capability for Zen models. The /v1/models API returns no
// capability metadata, so this map is our source of truth, keyed by
// exact model ID — name-prefix rules must not be used here: mimo-v2.5
// accepts image input while mimo-v2.5-pro does not. Models absent
// from the map are not vision-capable.
var zenModelVision = map[string]bool{
	"mimo-v2.5":                    true,
	"mimo-v2.5-free":               true,
	"mimo-v2.5-pro":                false,
	"deepseek-v4-flash-vision-exp": true,
}

// Fetches model info from the appropriate /v1/models endpoint based on
// the model prefix (opencode-go/ → Go, opencode/ or bare → Zen).
func (o *opencodeZenProvider) fetchModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	baseURL, effectiveModel := o.resolveModel(model)

	var resp struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	// Include session header for Zen API compliance.
	if err := fetchModelJSON(
		ctx, o.client, baseURL+"/v1/models", "opencode_zen",
		&resp,
		http.Header{"Authorization": {"Bearer " + o.apiKey}},
		o.zenSessionHeader(),
	); err != nil {
		return ModelInfo{}, err
	}

	for _, m := range resp.Data {
		if m.ID == effectiveModel {
			cw := m.ContextLength
			if cw == 0 {
				cw = zenModelContextWindows[effectiveModel]
			}
			if cw == 0 {
				return ModelInfo{}, fmt.Errorf("model %q context length unknown", effectiveModel)
			}
			return ModelInfo{
				ContextWindow:  cw,
				SupportsTools:  true,
				SupportsVision: zenModelVision[effectiveModel],
			}, nil
		}
	}

	return ModelInfo{}, fmt.Errorf("model %q not found", effectiveModel)
}

// Returns model info via FetchModelInfo with provider config.
func (o *opencodeZenProvider) ModelInfo(
	ctx context.Context, model string,
) (ModelInfo, error) {
	if model == "" {
		model = o.model
	}
	return FetchModelInfo(ctx, o, model, o.cfg)
}

// Streams a chat response (not yet implemented for OpencodeZen).
func (o *opencodeZenProvider) ChatStream(
	_ context.Context, _ ChatRequest,
) (<-chan StreamEvent, error) {
	return nil, fmt.Errorf("opencode_zen: streaming not yet supported")
}

// Fetches available models from the catalog the given model
// resolves to — the Go subscription endpoint for opencode-go/ models,
// the pay-as-you-go endpoint otherwise — so the list always contains
// the model named on the line above it.
func (o *opencodeZenProvider) ListModels(
	ctx context.Context, model string,
) ([]ModelSummary, error) {
	if model == "" {
		model = o.model
	}

	baseURL, _ := o.resolveModel(model)

	var resp struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	// Include session header for Zen API compliance.
	if err := fetchModelJSON(
		ctx, o.client, baseURL+"/v1/models", "opencode_zen",
		&resp,
		http.Header{"Authorization": {"Bearer " + o.apiKey}},
		o.zenSessionHeader(),
	); err != nil {
		return nil, err
	}

	models := make([]ModelSummary, len(resp.Data))
	for i, m := range resp.Data {
		cw := m.ContextLength
		if cw == 0 {
			// The API omits context_length; the registry is the
			// source of truth (same fallback as fetchModelInfo).
			cw = zenModelContextWindows[m.ID]
		}
		models[i] = ModelSummary{
			ID:            m.ID,
			ContextWindow: cw,
		}
	}

	return models, nil
}
