package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"shmorby/internal/redact"
)

// OpenAI-compatible request/response types shared by OpenRouter and
// OpencodeZen providers.

// IsReasoningModel reports whether the model uses the reasoning API
// (o-series, gpt-5.x). Reasoning models require max_completion_tokens
// instead of max_tokens, support reasoning_effort, and use the
// "developer" role instead of "system".
//
// Handles vendor-prefixed IDs (e.g. "openai/o1", "openai/gpt-5.4-pro")
// used by OpenRouter and Zen by stripping the prefix before matching.
func IsReasoningModel(model string) bool {
	// Strip vendor prefix (e.g. "openai/" from "openai/o1").
	if i := strings.IndexByte(model, '/'); i >= 0 {
		model = model[i+1:]
	}

	// Exact or prefix match against known reasoning model families.
	// Prefixes end at a name boundary (dash, dot, underscore, or end
	// of string) so "gpt-5.4-mini" matches but "gpt-5.4xyz" does not.
	prefixes := []string{
		"o1", "o3", "o4-mini",
		"gpt-5",
	}
	for _, p := range prefixes {
		if model == p {
			return true
		}
		if len(model) > len(p) && model[:len(p)] == p {
			c := model[len(p)]
			if c == '-' || c == '.' || c == '_' {
				return true
			}
		}
	}

	return false
}

// openaiMessage mirrors the OpenAI chat message shape. Content holds
// either a JSON string (text-only messages) or, for image-bearing
// user messages, a content-parts array (openaiContentPart values).
type openaiMessage struct {
	Role         string              `json:"role"`
	Content      any                 `json:"content"`
	ToolCalls    []openaiToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
	FunctionCall *openaiFunctionCall `json:"function_call,omitempty"`
}

// Is one element of a multimodal content array. Exactly one of
// Text/ImageURL is set per element, keyed by Type.
type openaiContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openaiImageURL `json:"image_url,omitempty"`
}

// Carries a data-URI for inline images (data:<media>;base64,<payload>).
type openaiImageURL struct {
	URL string `json:"url"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Legacy function_call shape (pre tool_calls).
type openaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiRequest struct {
	Model         string          `json:"model"`
	Messages      []openaiMessage `json:"messages"`
	Tools         []openaiTool    `json:"tools,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
	// MaxTokens is deprecated — use MaxCompletionTokens for
	// reasoning models (o-series, gpt-5.x). Kept for legacy
	// non-reasoning models only.
	MaxTokens           int `json:"max_tokens,omitempty"`
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
}

// streamOptions enables usage reporting in streaming responses.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openaiChoice struct {
	Index        int           `json:"index"`
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// openaiUsage captures token usage from the response.
type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openaiResponse struct {
	ID      string         `json:"id"`
	Choices []openaiChoice `json:"choices"`
	Usage   *openaiUsage   `json:"usage,omitempty"`
}

// Sends an OpenAI-compatible chat request with retry on 5xx.
//
// The baseURL must NOT include /v1 — it is appended internally.
// Optional extraHeaders are applied after standard headers, allowing
// providers to inject custom headers (e.g. X-Opencode-Session).
func doOpenAIRequest(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	providerName string,
	body openaiRequest,
	extraHeaders ...http.Header,
) (openaiResponse, error) {
	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return openaiResponse{}, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}

		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return openaiResponse{}, fmt.Errorf("encode request: %w", err)
		}

		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost,
			baseURL+"/v1/chat/completions", &buf,
		)
		if err != nil {
			return openaiResponse{}, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		// Apply any provider-specific extra headers (e.g.
		// X-Opencode-Session for the Zen provider).
		for _, h := range extraHeaders {
			for k, v := range h {
				for _, val := range v {
					req.Header.Set(k, val)
				}
			}
		}

		httpResp, err := client.Do(req)
		if err != nil {
			return openaiResponse{}, fmt.Errorf("http request: %w", err)
		}

		if httpResp.StatusCode >= 500 {
			lastErr = fmt.Errorf(
				"%s returned status %d", providerName, httpResp.StatusCode,
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
				"%s returned status %d: %s",
				providerName, httpResp.StatusCode,
				redact.SecretString(string(bodyBytes)),
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
		fmt.Errorf("%s request failed after retry: %w", providerName, lastErr)
}

// GETs a model endpoint and decodes its JSON body into out, which
// must point at the shape that endpoint returns. Zero or more header
// maps are applied to the request.
//
// Errors carry the provider name; the error body is limited to 1024
// bytes and redacted so an API key echoed back by the server cannot
// reach logs.
func fetchModelJSON(
	ctx context.Context,
	client *http.Client,
	url, provider string,
	out any,
	headers ...http.Header,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	for _, h := range headers {
		for k, v := range h {
			for _, val := range v {
				req.Header.Set(k, val)
			}
		}
	}

	httpResp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode >= 400 {
		// Limit and redact error body to prevent API key
		// echo-back from leaking into logs.
		bodyBytes, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))

		return fmt.Errorf(
			"%s returned status %d: %s",
			provider, httpResp.StatusCode,
			redact.SecretString(string(bodyBytes)),
		)
	}

	if err := json.NewDecoder(httpResp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// Builds the OpenAI message array from internal ChatRequest, prepending
// system prompt as a system-role (or developer-role for reasoning
// models) message when non-empty.
//
// Tool-role messages always include content (empty string if none) to
// avoid JSON null rejection by some providers.
func buildOpenAIMessages(req ChatRequest) []openaiMessage {
	messages := make([]openaiMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		s := req.System
		// Reasoning models (o-series, gpt-5.x) use the
		// "developer" role per OpenAI docs. OpenAI auto-converts
		// today, but using the correct role avoids future breakage.
		role := "system"
		if req.Model != "" && IsReasoningModel(req.Model) {
			role = "developer"
		}
		messages = append(messages, openaiMessage{
			Role:    role,
			Content: &s,
		})
	}
	for _, m := range req.Messages {
		om := openaiMessage{Role: m.Role}
		if m.Content != "" || m.Role == "tool" {
			om.Content = &m.Content
		}
		if len(m.Images) > 0 && m.Role == "user" {
			om.Content = buildOpenAIContentParts(m)
		}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, openaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openaiToolCallFunction{
					Name:      tc.Name,
					Arguments: tc.Args,
				},
			})
		}
		if m.ToolCallID != "" {
			om.ToolCallID = m.ToolCallID
		}
		messages = append(messages, om)
	}
	return messages
}

// Builds a multimodal content-parts array for an image-bearing user
// message: the text part first (when non-empty), then one image part
// per attached image as a base64 data URI.
func buildOpenAIContentParts(m Message) []openaiContentPart {
	parts := make([]openaiContentPart, 0, 1+len(m.Images))

	if m.Content != "" {
		parts = append(parts, openaiContentPart{
			Type: "text",
			Text: m.Content,
		})
	}
	for _, img := range m.Images {
		parts = append(parts, openaiContentPart{
			Type: "image_url",
			ImageURL: &openaiImageURL{
				URL: fmt.Sprintf(
					"data:%s;base64,%s",
					img.MediaType,
					base64.StdEncoding.EncodeToString(img.Data),
				),
			},
		})
	}

	return parts
}

// Converts internal tool definitions to OpenAI tools format.
func buildOpenAITools(tools []ToolDef) []openaiTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openaiTool, len(tools))
	for i, t := range tools {
		out[i] = openaiTool{
			Type:     "function",
			Function: openaiFunction(t),
		}
	}
	return out
}

// Converts an OpenAI response into the internal ChatResponse format.
//
// Returns an error when choices is empty or tool-call arguments are not
// valid JSON. Usage data (prompt/completion/total tokens) is captured
// in ChatResponse for audit and cost logging.
func parseOpenAIResponse(resp openaiResponse) (ChatResponse, error) {
	if len(resp.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("openai response: empty choices")
	}
	ch := resp.Choices[0]

	cr := ChatResponse{
		FinishReason: ch.FinishReason,
		Message: Message{
			Role: ch.Message.Role,
		},
		// Surface response ID for audit/logging.
		ID: resp.ID,
	}

	// Content decodes as a plain string for most providers; some
	// return a content-parts array (text + optional image parts),
	// from which the text parts are concatenated. Images are never
	// returned by assistants.
	if s, ok := ch.Message.Content.(string); ok {
		cr.Message.Content = s
	} else {
		cr.Message.Content = extractContentPartsText(ch.Message.Content)
	}

	// Capture token usage when present (non-streaming responses).
	if resp.Usage != nil {
		cr.Usage = Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}

	for _, tc := range ch.Message.ToolCalls {
		if !json.Valid([]byte(tc.Function.Arguments)) {
			return ChatResponse{}, fmt.Errorf(
				"openai response: invalid tool call arguments for %q",
				tc.Function.Name,
			)
		}
		call := ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: tc.Function.Arguments,
		}
		cr.ToolCalls = append(cr.ToolCalls, call)
	}

	// Legacy function_call shape (pre tool_calls).
	if len(cr.ToolCalls) == 0 && ch.Message.FunctionCall != nil {
		fc := ch.Message.FunctionCall
		if !json.Valid([]byte(fc.Arguments)) {
			return ChatResponse{}, fmt.Errorf(
				"openai response: invalid function_call arguments for %q",
				fc.Name,
			)
		}
		cr.ToolCalls = append(cr.ToolCalls, ToolCall{
			Name: fc.Name,
			Args: fc.Arguments,
		})
	}

	cr.Message.ToolCalls = cr.ToolCalls

	return cr, nil
}

// Joins the text parts of a content-parts array response. Non-array
// values decode as empty, matching providers that omit content.
func extractContentPartsText(content any) string {
	raw, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	var parts []openaiContentPart
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type == "text" && p.Text != "" {
			texts = append(texts, p.Text)
		}
	}

	return strings.Join(texts, "\n")
}

// Strips trailing /v1 from a base URL so doOpenAIRequest does not
// produce a double /v1 path.
func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSuffix(
		strings.TrimRight(baseURL, "/"), "/v1",
	)
	return baseURL
}
