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

// --- OpenAI API compliance tests ---

// Checks context window registry has correct values per OpenAI docs.
func TestMatchOpenAIContextWindow_CorrectValues(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"gpt-5.6-sol", 1050000},
		{"gpt-5.6-terra", 1050000},
		{"gpt-5.6-luna", 1050000},
		{"gpt-5.5", 1050000},
		{"gpt-5.5-pro", 1050000},
		{"gpt-5.4", 1050000},
		{"gpt-5.4-pro", 1050000},
		{"gpt-5.4-mini", 400000},
		{"gpt-5.4-nano", 400000},
		{"gpt-5.3-codex", 400000},
		{"gpt-5.3-codex-spark", 400000},
		{"gpt-5.2", 400000},
		{"gpt-5.1", 400000},
		{"gpt-5.1-mini", 400000},
		{"gpt-5", 272000},
		{"gpt-5-nano", 272000},
		{"gpt-4.1", 1048576},
		{"gpt-4.1-mini", 1048576},
		{"gpt-4.1-nano", 1048576},
		{"o1", 200000},
		{"o3", 200000},
		{"o4-mini", 200000},
		{"gpt-4o", 128000},
		{"gpt-4o-mini", 128000},
		{"gpt-4", 8192},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			cw, ok := matchOpenAIContextWindow(tc.model)
			if !ok {
				t.Fatalf(
					"model %q not found in registry",
					tc.model,
				)
			}
			if cw != tc.want {
				t.Errorf(
					"model %q: want %d, got %d",
					tc.model, tc.want, cw,
				)
			}
		})
	}
}

// Checks MaxOutputTokens registry has correct values.
func TestMatchOpenAIMaxOutputTokens_CorrectValues(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"gpt-5.6-sol", 128000},
		{"gpt-5.4-mini", 128000},
		{"gpt-5", 128000},
		{"gpt-4.1", 32768},
		{"gpt-4.1-mini", 32768},
		{"o1", 32768},
		{"o1-mini", 65536},
		{"o3", 32768},
		{"o3-mini", 65536},
		{"o4-mini", 65536},
		{"gpt-4o", 16384},
		{"gpt-4o-mini", 16384},
		{"gpt-4-turbo", 4096},
		{"gpt-4", 4096},
		{"gpt-3.5-turbo", 4096},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			mot, ok := matchOpenAIMaxOutputTokens(tc.model)
			if !ok {
				t.Fatalf(
					"model %q not found in MaxOutputTokens "+
						"registry", tc.model,
				)
			}
			if mot != tc.want {
				t.Errorf(
					"model %q: want %d, got %d",
					tc.model, tc.want, mot,
				)
			}
		})
	}
}

// Checks dead models have been removed from the registry. Exact
// matches must fail; prefix matches via gpt-5/5.1 are expected
// (snapshot resolution).
func TestMatchOpenAIContextWindow_DeadModelsRemoved(t *testing.T) {
	// Exact dead keys — these models are fully shut down.
	dead := []string{
		"chatgpt-4o-latest",
		"gpt-4o-search",
	}

	for _, model := range dead {
		t.Run(model, func(t *testing.T) {
			_, ok := openAIModelContextWindows[model]
			if ok {
				t.Errorf(
					"dead model %q should be removed "+
						"from registry", model,
				)
			}
		})
	}

	// These codex models were shut down 2026-07-23 but may still
	// resolve via prefix matching (gpt-5-codex → gpt-5). Verify
	// they are NOT direct registry keys.
	deadPrefix := []string{
		"gpt-5-codex",
		"gpt-5.1-codex",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
		"gpt-5.2-codex",
	}

	for _, model := range deadPrefix {
		t.Run(model+"/exact", func(t *testing.T) {
			_, ok := openAIModelContextWindows[model]
			if ok {
				t.Errorf(
					"dead model %q should not be an "+
						"exact registry key", model,
				)
			}
		})
	}
}

// Checks IsReasoningModel correctly identifies reasoning models,
// including vendor-prefixed IDs used by OpenRouter/Zen.
func TestIsReasoningModel_IdentifiesModels(t *testing.T) {
	reasoning := []string{
		"o1", "o1-mini", "o1-preview",
		"o3", "o3-mini",
		"o4-mini", "o4-mini-high",
		"gpt-5", "gpt-5-nano",
		"gpt-5.1", "gpt-5.1-mini",
		"gpt-5.2",
		"gpt-5.3-codex", "gpt-5.3-codex-spark",
		"gpt-5.4", "gpt-5.4-pro",
		"gpt-5.4-mini", "gpt-5.4-nano",
		"gpt-5.5", "gpt-5.5-pro",
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
		// Vendor-prefixed IDs (OpenRouter / Zen).
		"openai/o1", "openai/o3-mini",
		"openai/gpt-5.4-pro", "openai/gpt-5.5",
	}
	nonReasoning := []string{
		"gpt-4", "gpt-4-turbo", "gpt-4o", "gpt-4o-mini",
		"gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano",
		"gpt-3.5-turbo",
		"llama3.2", "claude-sonnet-4",
		// Vendor-prefixed non-reasoning.
		"openai/gpt-4o", "openai/gpt-4.1",
	}

	for _, model := range reasoning {
		t.Run("reasoning/"+model, func(t *testing.T) {
			if !IsReasoningModel(model) {
				t.Errorf(
					"want %q to be a reasoning model",
					model,
				)
			}
		})
	}
	for _, model := range nonReasoning {
		t.Run("non-reasoning/"+model, func(t *testing.T) {
			if IsReasoningModel(model) {
				t.Errorf(
					"want %q to NOT be a reasoning model",
					model,
				)
			}
		})
	}
}

// Checks reasoning model uses max_completion_tokens instead of
// max_tokens in the outbound request.
func TestOpenAIChat_ReasoningModel_MaxCompletionTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			if req.MaxCompletionTokens != 4096 {
				t.Errorf(
					"want max_completion_tokens 4096, got %d",
					req.MaxCompletionTokens,
				)
			}
			if req.MaxTokens != 0 {
				t.Errorf(
					"want max_tokens 0 (not sent), got %d",
					req.MaxTokens,
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

	p := newOpenAIProvider(srv.URL, "key", "", "gpt-5.4", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Model: "gpt-5.4",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks non-reasoning model uses max_tokens (legacy) in the
// outbound request.
func TestOpenAIChat_NonReasoningModel_MaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			if req.MaxTokens != 2048 {
				t.Errorf(
					"want max_tokens 2048, got %d",
					req.MaxTokens,
				)
			}
			if req.MaxCompletionTokens != 0 {
				t.Errorf(
					"want max_completion_tokens 0 (not sent), got %d",
					req.MaxCompletionTokens,
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

	p := newOpenAIProvider(srv.URL, "key", "", "gpt-4o", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks reasoning model uses developer role for system prompt.
func TestOpenAIChat_ReasoningModel_DeveloperRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			var found bool
			for _, m := range req.Messages {
				if m.Role == "developer" && m.Content != nil &&
					*m.Content == "You are helpful." {
					found = true
					break
				}
			}
			if !found {
				t.Errorf(
					"developer message not found in request; "+
						"roles: %v", messageRoles(req.Messages),
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

	p := newOpenAIProvider(srv.URL, "key", "", "o3", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Model:  "o3",
		System: "You are helpful.",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks non-reasoning model still uses system role.
func TestOpenAIChat_NonReasoningModel_SystemRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			var found bool
			for _, m := range req.Messages {
				if m.Role == "system" && m.Content != nil &&
					*m.Content == "You are helpful." {
					found = true
					break
				}
			}
			if !found {
				t.Errorf(
					"system message not found; roles: %v",
					messageRoles(req.Messages),
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

	p := newOpenAIProvider(srv.URL, "key", "", "gpt-4o", 120, config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Model:  "gpt-4o",
		System: "You are helpful.",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

// Checks streaming reasoning deltas are emitted as "reasoning" events.
func TestOpenAIChatStream_ReasoningDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w,
				`data: {"choices":[{"delta":{"reasoning":"Let me think"},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprint(w,
				`data: {"choices":[{"delta":{"content":"The answer is 42"},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "o3", 120, config.Config{})

	events, err := p.ChatStream(context.Background(), ChatRequest{
		Model: "o3",
		Messages: []Message{
			{Role: "user", Content: "What is 6*7?"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var reasoningEvents []StreamEvent
	var textEvents []StreamEvent
	for ev := range events {
		switch ev.Type {
		case "reasoning":
			reasoningEvents = append(reasoningEvents, ev)
		case "text":
			textEvents = append(textEvents, ev)
		case "done":
			goto done
		}
	}
done:
	if len(reasoningEvents) != 1 {
		t.Fatalf("want 1 reasoning event, got %d",
			len(reasoningEvents))
	}
	if reasoningEvents[0].Delta != "Let me think" {
		t.Errorf(
			"want reasoning delta 'Let me think', got %q",
			reasoningEvents[0].Delta,
		)
	}
	if len(textEvents) != 1 {
		t.Fatalf("want 1 text event, got %d", len(textEvents))
	}
	if textEvents[0].Delta != "The answer is 42" {
		t.Errorf(
			"want text delta 'The answer is 42', got %q",
			textEvents[0].Delta,
		)
	}
}

// Checks streaming usage chunk is emitted when stream_options
// include_usage is set.
func TestOpenAIChatStream_UsageChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Verify stream_options.include_usage is sent.
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
				t.Errorf("want stream_options.include_usage true")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w,
				`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`+"\n\n")
			// Final usage chunk (empty choices).
			_, _ = fmt.Fprint(w,
				`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`+"\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "gpt-5.4", 120, config.Config{})

	events, err := p.ChatStream(context.Background(), ChatRequest{
		Model: "gpt-5.4",
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var usageEvent *StreamEvent
	for ev := range events {
		if ev.Type == "usage" {
			usageEvent = &ev
		}
	}
	if usageEvent == nil {
		t.Fatal("expected usage event, got none")
	}
	if !strings.Contains(usageEvent.Content, `"total_tokens":15`) {
		t.Errorf(
			"want usage with total_tokens 15, got %q",
			usageEvent.Content,
		)
	}
}

// Checks non-streaming response captures usage and ID.
func TestOpenAIChat_ResponseUsageAndID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"id": "chatcmpl-test123",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello!",
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{
					"prompt_tokens":     10,
					"completion_tokens": 5,
					"total_tokens":      15,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "test-model", 120, config.Config{})

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.ID != "chatcmpl-test123" {
		t.Errorf("want ID 'chatcmpl-test123', got %q", resp.ID)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("want TotalTokens 15, got %d", resp.Usage.TotalTokens)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("want PromptTokens 10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf(
			"want CompletionTokens 5, got %d",
			resp.Usage.CompletionTokens,
		)
	}
}

// Checks streaming tool calls work alongside reasoning deltas.
func TestOpenAIChatStream_ReasoningModel_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w,
				`data: {"choices":[{"delta":{"reasoning":"I should check the weather"},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprint(w,
				`data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":"}}]},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprint(w,
				`data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"arguments":"\"NYC\"}"}}]},"finish_reason":null}]}`+"\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(srv.URL, "key", "", "gpt-5.4", 120, config.Config{})

	events, err := p.ChatStream(context.Background(), ChatRequest{
		Model: "gpt-5.4",
		Messages: []Message{
			{Role: "user", Content: "Weather?"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var reasoningCount, toolCount int
	for ev := range events {
		switch ev.Type {
		case "reasoning":
			reasoningCount++
		case "tool-call":
			toolCount++
		case "done":
			goto done
		}
	}
done:
	if reasoningCount != 1 {
		t.Errorf("want 1 reasoning event, got %d", reasoningCount)
	}
	if toolCount != 2 {
		t.Errorf("want 2 tool-call events, got %d", toolCount)
	}
}

// helper: extracts roles from a message slice for test diagnostics.
func messageRoles(msgs []openaiMessage) []string {
	roles := make([]string, len(msgs))
	for i, m := range msgs {
		roles[i] = m.Role
	}
	return roles
}
