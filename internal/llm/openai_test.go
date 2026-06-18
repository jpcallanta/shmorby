package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shmorby/internal/config"
)

// Checks factory returns openai provider when key is set.
func TestNewProvider_OpenAI(t *testing.T) {
	cfg := config.Config{}
	cfg.Provider = "openai"
	cfg.OpenAI.APIKey = "test-key"

	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("want name openai, got %q", p.Name())
	}
}

// Checks factory returns error when OpenAI API key is missing.
func TestNewProvider_OpenAIKeyMissing(t *testing.T) {
	cfg := config.Config{}
	cfg.Provider = "openai"

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing openai key, got nil")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("want error containing 'API key', got: %v", err)
	}
}

// Checks factory reads API key from env var named by api_key_env.
func TestNewProvider_OpenAIKeyEnv(t *testing.T) {
	cfg := config.Config{}
	cfg.Provider = "openai"
	cfg.OpenAI.APIKeyEnv = "TEST_OPENAI_KEY_ENV"

	t.Setenv("TEST_OPENAI_KEY_ENV", "env-key-123")

	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("want name openai, got %q", p.Name())
	}
}

// Checks simple text response via OpenAI.
func TestOpenAIChat_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("want POST, got %s", r.Method)
			}
			if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
				t.Errorf("want /v1/chat/completions, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-key-123" {
				t.Errorf(
					"want Bearer test-key-123, got %q",
					r.Header.Get("Authorization"),
				)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("Hello! How can I help?"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "test-key-123", "", "test-model", 120,
		config.Config{},
	)

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Message.Content != "Hello! How can I help?" {
		t.Errorf(
			"want content %q, got %q",
			"Hello! How can I help?", resp.Message.Content,
		)
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("want role assistant, got %q", resp.Message.Role)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("want finish_reason stop, got %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("want 0 tool calls, got %d", len(resp.ToolCalls))
	}
}

// Checks tool_calls are parsed correctly from OpenAI response.
func TestOpenAIChat_ToolCallsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: nil,
					ToolCalls: []openaiToolCall{
						{
							ID:   "call_abc123",
							Type: "function",
							Function: openaiToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"location":"NYC","unit":"celsius"}`,
							},
						},
						{
							ID:   "call_def456",
							Type: "function",
							Function: openaiToolCallFunction{
								Name:      "get_time",
								Arguments: `{"timezone":"UTC"}`,
							},
						},
					},
				},
				"tool_calls",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "test-key", "", "test-model", 120, config.Config{})

	resp, err := p.Chat(context.Background(), ChatRequest{
		System: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: "What's the weather in NYC?"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("want 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_abc123" {
		t.Errorf("want ID call_abc123, got %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("want name get_weather, got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Args != `{"location":"NYC","unit":"celsius"}` {
		t.Errorf(
			"want args %q, got %q",
			`{"location":"NYC","unit":"celsius"}`,
			resp.ToolCalls[0].Args,
		)
	}
	if resp.ToolCalls[1].Name != "get_time" {
		t.Errorf("want name get_time, got %q", resp.ToolCalls[1].Name)
	}
	if resp.ToolCalls[1].Args != `{"timezone":"UTC"}` {
		t.Errorf(
			"want args %q, got %q",
			`{"timezone":"UTC"}`,
			resp.ToolCalls[1].Args,
		)
	}

	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("want 2 Message.ToolCalls, got %d",
			len(resp.Message.ToolCalls))
	}

	if resp.FinishReason != "tool_calls" {
		t.Errorf("want finish_reason tool_calls, got %q",
			resp.FinishReason)
	}
	if resp.Message.Content != "" {
		t.Errorf("want empty content, got %q", resp.Message.Content)
	}
}

// Checks legacy function_call shape is parsed as a tool call.
func TestOpenAIChat_LegacyFunctionCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := openaiResponse{
				Choices: []openaiChoice{
					{
						Index: 0,
						Message: openaiMessage{
							Role:    "assistant",
							Content: nil,
							FunctionCall: &openaiFunctionCall{
								Name:      "get_weather",
								Arguments: `{"location":"NYC"}`,
							},
						},
						FinishReason: "function_call",
					},
				},
			}
			b, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			_, _ = w.Write(b)
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "test-key", "", "test-model", 120, config.Config{})

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Weather?"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("want name get_weather, got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Args != `{"location":"NYC"}` {
		t.Errorf(
			"want args %q, got %q",
			`{"location":"NYC"}`, resp.ToolCalls[0].Args,
		)
	}
	if resp.ToolCalls[0].ID != "" {
		t.Errorf("want empty ID for legacy call, got %q", resp.ToolCalls[0].ID)
	}
}

// Checks system prompt is included as a system-role message.
func TestOpenAIChat_SystemPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			var found bool
			for _, m := range req.Messages {
				if m.Role == "system" && m.Content != nil &&
					*m.Content == "You are a helpful assistant." {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("system message not found in request messages")
			}
			if len(req.Messages) != 2 {
				t.Errorf("want 2 messages (system + user), got %d",
					len(req.Messages))
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("OK"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "test-key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		System: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks ChatRequest.Model overrides the provider default.
func TestOpenAIChat_ModelOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			if req.Model != "override-model" {
				t.Errorf("want model override-model, got %q", req.Model)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("ok"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "test-key", "", "default-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Model: "override-model",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks tools are included in the outbound request body.
func TestOpenAIChat_ToolsInRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			if len(req.Tools) != 1 {
				t.Fatalf("want 1 tool, got %d", len(req.Tools))
			}
			if req.Tools[0].Type != "function" {
				t.Errorf("want type function, got %q", req.Tools[0].Type)
			}
			if req.Tools[0].Function.Name != "get_weather" {
				t.Errorf("want name get_weather, got %q",
					req.Tools[0].Function.Name)
			}
			if req.Tools[0].Function.Description != "Get weather" {
				t.Errorf("want description 'Get weather', got %q",
					req.Tools[0].Function.Description)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("I can help with that."),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "test-key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "What's the weather?"},
		},
		Tools: []ToolDef{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks organization header is sent when configured.
func TestOpenAIChat_OrganizationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("OpenAI-Organization") != "org-test" {
				t.Errorf(
					"want org header 'org-test', got %q",
					r.Header.Get("OpenAI-Organization"),
				)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("ok"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "test-key", "org-test", "test-model", 120,
		config.Config{},
	)

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks 401 returns "invalid API key" error.
func TestOpenAIChat_InvalidAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "bad-key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("want error containing 'invalid API key', got: %v", err)
	}
}

// Checks retry on 429 then succeeds.
func TestOpenAIChat_RateLimitRetry(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("recovered"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "recovered" {
		t.Errorf("want content recovered, got %q", resp.Message.Content)
	}
	if attempts != 3 {
		t.Errorf("want 3 attempts (2 fails + 1 retry), got %d", attempts)
	}
}

// Checks retry on 5xx then succeeds.
func TestOpenAIChat_ServerErrorRetry(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("recovered"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "recovered" {
		t.Errorf("want content recovered, got %q", resp.Message.Content)
	}
	if attempts != 3 {
		t.Errorf("want 3 attempts (2 fails + 1 retry), got %d", attempts)
	}
}

// Checks retry exhaustion on persistent errors.
func TestOpenAIChat_RetryExhaustion(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion, got nil")
	}
	if !strings.Contains(err.Error(), "retries") {
		t.Errorf("want error containing 'retries', got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("want 3 attempts, got %d", attempts)
	}
}

// Returns a wrapped error for bad URL.
func TestOpenAIChat_BadURL(t *testing.T) {
	p := newOpenAIProvider("://not-a-url", "key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for bad URL, got nil")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("want error containing 'create request', got: %v", err)
	}
}

// Checks base URL normalization strips trailing /v1.
func TestOpenAIChat_NormalizeBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
				t.Errorf("want /v1/chat/completions, got %s", r.URL.Path)
			}
			if strings.Contains(r.URL.Path, "/v1/v1") {
				t.Errorf("double /v1 detected in URL: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("ok"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL+"/v1", "key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks streaming sends SSE request and emits events.
func TestOpenAIChatStream_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Verify stream flag is set.
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if !req.Stream {
				t.Errorf("want stream true, got false")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	events, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var got []string
	for ev := range events {
		if ev.Type == "done" {
			break
		}
		if ev.Type == "text" {
			got = append(got, ev.Delta)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want 2 text events, got %d", len(got))
	}
	if got[0] != "Hello" || got[1] != " world" {
		t.Errorf("want ['Hello',' world'], got %v", got)
	}
}

// Checks streaming tool calls are emitted.
func TestOpenAIChatStream_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"loc\\\"\"}}]},\"finish_reason\":null}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\":\\\"NYC\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	events, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Weather?"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var toolEvents []StreamEvent
	for ev := range events {
		if ev.Type == "done" {
			break
		}
		if ev.Type == "tool-call" {
			toolEvents = append(toolEvents, ev)
		}
	}
	if len(toolEvents) != 2 {
		t.Fatalf("want 2 tool-call events, got %d", len(toolEvents))
	}
	if toolEvents[0].ToolID != "call_1" {
		t.Errorf("want tool ID call_1, got %q", toolEvents[0].ToolID)
	}
	if toolEvents[0].Tool != "get_weather" {
		t.Errorf("want tool get_weather, got %q", toolEvents[0].Tool)
	}
}

// Checks streaming error events on bad JSON.
func TestOpenAIChatStream_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {invalid json\n\n")
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	events, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var errorEvent *StreamEvent
	for ev := range events {
		if ev.Type == "error" {
			errorEvent = &ev
			break
		}
	}
	if errorEvent == nil {
		t.Fatal("expected error event, got none")
	}
	if !strings.Contains(errorEvent.Delta, "parse error") {
		t.Errorf("want parse error, got %q", errorEvent.Delta)
	}
}

// Checks streaming HTTP error returns error.
func TestOpenAIChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	_, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
}

// Checks ModelInfo returns fallback when API is unreachable.
func TestOpenAIModelInfo_Fallback(t *testing.T) {
	p := newOpenAIProvider("http://unused", "key", "", "model", 120, config.Config{})

	info, err := p.ModelInfo(context.Background(), "gpt-4")
	if err != ErrModelInfoFallback {
		t.Errorf("want ErrModelInfoFallback, got %v", err)
	}
	if info.ContextWindow != 8192 {
		t.Errorf("want 8192 ContextWindow, got %d", info.ContextWindow)
	}
}

// Checks 4xx is not retried.
func TestOpenAIChat_ClientErrorNoRetry(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("want error containing status code 400, got: %v", err)
	}
	if attempts != 1 {
		t.Errorf("want 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

// Helper to check context cancellation.
func TestOpenAIChat_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("ok"),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	ctx, cancel := context.WithTimeout(
		context.Background(), 50*time.Millisecond,
	)
	defer cancel()

	_, err := p.Chat(ctx, ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for context cancel, got nil")
	}
}
