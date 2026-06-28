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
)

// OpenAI-compatible request/response types shared by OpenRouter and
// OpencodeZen providers.

type openaiMessage struct {
	Role         string              `json:"role"`
	Content      *string             `json:"content"`
	ToolCalls    []openaiToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
	FunctionCall *openaiFunctionCall `json:"function_call,omitempty"`
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
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Tools    []openaiTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream,omitempty"`
}

type openaiChoice struct {
	Index        int           `json:"index"`
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
}

// Sends an OpenAI-compatible chat request with retry on 5xx.
//
// The baseURL must NOT include /v1 — it is appended internally.
func doOpenAIRequest(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	apiKey string,
	providerName string,
	body openaiRequest,
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
			bodyBytes, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			return openaiResponse{}, fmt.Errorf(
				"%s returned status %d: %s",
				providerName, httpResp.StatusCode, string(bodyBytes),
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

// Builds the OpenAI message array from internal ChatRequest, prepending
// system prompt as a system-role message when non-empty.
//
// Tool-role messages always include content (empty string if none) to
// avoid JSON null rejection by some providers.
func buildOpenAIMessages(req ChatRequest) []openaiMessage {
	messages := make([]openaiMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		s := req.System
		messages = append(messages, openaiMessage{
			Role:    "system",
			Content: &s,
		})
	}
	for _, m := range req.Messages {
		om := openaiMessage{Role: m.Role}
		if m.Content != "" || m.Role == "tool" {
			om.Content = &m.Content
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
// valid JSON.
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
	}

	if ch.Message.Content != nil {
		cr.Message.Content = *ch.Message.Content
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

// Strips trailing /v1 from a base URL so doOpenAIRequest does not
// produce a double /v1 path.
func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSuffix(
		strings.TrimRight(baseURL, "/"), "/v1",
	)
	return baseURL
}
