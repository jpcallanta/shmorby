package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shmorby/internal/config"
)

// Builds an OpenAI-format JSON response body for the mock server.
func openaiResponseJSON(
	t *testing.T, msg openaiMessage, finishReason string,
) string {
	t.Helper()

	resp := openaiResponse{
		Choices: []openaiChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return string(b)
}

// Checks a simple text assistant response via OpenRouter.
func TestOpenRouterChat_TextResponse(t *testing.T) {
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

	p := newOpenRouterProvider(srv.URL, "test-key-123", "test-model", config.Config{})

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

// Checks tool_calls are parsed correctly from OpenRouter response.
func TestOpenRouterChat_ToolCallsResponse(t *testing.T) {
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

	p := newOpenRouterProvider(srv.URL, "test-key", "test-model", config.Config{})

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

// Checks system prompt is included as a system-role message in the
// outbound request.
func TestOpenRouterChat_SystemPrompt(t *testing.T) {
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

	p := newOpenRouterProvider(srv.URL, "test-key", "test-model", config.Config{})

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
func TestOpenRouterChat_ModelOverride(t *testing.T) {
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

	p := newOpenRouterProvider(srv.URL, "test-key", "default-model", config.Config{})

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
func TestOpenRouterChat_ToolsInRequest(t *testing.T) {
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

	p := newOpenRouterProvider(srv.URL, "test-key", "test-model", config.Config{})

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

// Checks round-trip: assistant tool_calls with tool_call_id on
// tool-role messages → follow-up chat.
func TestOpenRouterChat_RoundTripToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req openaiRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			// First call: user message → respond with tool_calls.
			if len(req.Messages) == 1 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(openaiResponseJSON(t,
					openaiMessage{
						Role:    "assistant",
						Content: nil,
						ToolCalls: []openaiToolCall{
							{
								ID:   "call_round_1",
								Type: "function",
								Function: openaiToolCallFunction{
									Name:      "get_weather",
									Arguments: `{"location":"NYC"}`,
								},
							},
						},
					},
					"tool_calls",
				)))
				return
			}

			// Second call: verify tool_call_id on tool-role messages.
			if len(req.Messages) == 3 {
				if req.Messages[1].Role != "assistant" {
					t.Errorf("want assistant role, got %q",
						req.Messages[1].Role)
				}
				if len(req.Messages[1].ToolCalls) != 1 {
					t.Errorf("want 1 tool_call in history, got %d",
						len(req.Messages[1].ToolCalls))
				}
				if req.Messages[1].ToolCalls[0].ID != "call_round_1" {
					t.Errorf("want tool_call ID call_round_1, got %q",
						req.Messages[1].ToolCalls[0].ID)
				}
				if req.Messages[2].Role != "tool" {
					t.Errorf("want tool role, got %q",
						req.Messages[2].Role)
				}
				if req.Messages[2].ToolCallID != "call_round_1" {
					t.Errorf("want tool_call_id call_round_1, got %q",
						req.Messages[2].ToolCallID)
				}
				if req.Messages[2].Content == nil {
					t.Errorf("want non-nil content on tool message, got nil")
				} else if *req.Messages[2].Content != "72F and sunny" {
					t.Errorf("want content '72F and sunny', got %q",
						*req.Messages[2].Content)
				}
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiResponseJSON(t,
				openaiMessage{
					Role:    "assistant",
					Content: strPtr("NYC weather is 72F."),
				},
				"stop",
			)))
		},
	))
	defer srv.Close()

	p := newOpenRouterProvider(srv.URL, "test-key", "test-model", config.Config{})

	// Turn 1: LLM returns tool_calls.
	resp1, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Weather in NYC?"},
		},
	})
	if err != nil {
		t.Fatalf("Chat turn 1: %v", err)
	}
	if len(resp1.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(resp1.ToolCalls))
	}

	// Turn 2: include assistant tool_calls + tool result in history.
	resp2, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Weather in NYC?"},
			{
				Role:      "assistant",
				ToolCalls: resp1.ToolCalls,
			},
			{
				Role:       "tool",
				Content:    "72F and sunny",
				ToolCallID: "call_round_1",
				ToolName:   "get_weather",
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat turn 2: %v", err)
	}
	if resp2.Message.Content != "NYC weather is 72F." {
		t.Errorf("want 'NYC weather is 72F.', got %q",
			resp2.Message.Content)
	}
}

// Checks factory returns openrouter provider when key is set.
func TestNewProvider_OpenRouter(t *testing.T) {
	cfg := config.Config{}
	cfg.Provider = "openrouter"
	cfg.OpenRouter.APIKey = "test-key"

	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Name() != "openrouter" {
		t.Errorf("want name openrouter, got %q", p.Name())
	}
}

// Checks factory returns error when OpenRouter API key is missing.
func TestNewProvider_OpenRouterKeyMissing(t *testing.T) {
	cfg := config.Config{}
	cfg.Provider = "openrouter"

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing openrouter key, got nil")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Errorf("want error containing 'OPENROUTER_API_KEY', got: %v", err)
	}
}

// Returns a wrapped error for bad URL.
func TestOpenRouterChat_UnreachableHost(t *testing.T) {
	p := newOpenRouterProvider("http://127.0.0.1:1", "key", "test-model", config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
	if !strings.Contains(err.Error(), "http request") {
		t.Errorf("want error containing 'http request', got: %v", err)
	}
}

// Returns a wrapped error for malformed URL.
func TestOpenRouterChat_BadURL(t *testing.T) {
	p := newOpenRouterProvider("://not-a-url", "key", "test-model", config.Config{})

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

// Checks 4xx is not retried.
func TestOpenRouterChat_ClientErrorNoRetry(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
		},
	))
	defer srv.Close()

	p := newOpenRouterProvider(srv.URL, "key", "test-model", config.Config{})

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

// Checks retry on 5xx then succeeds.
func TestOpenRouterChat_ServerErrorRetry(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 2 {
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

	p := newOpenRouterProvider(srv.URL, "key", "test-model", config.Config{})

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
	if attempts != 2 {
		t.Errorf("want 2 attempts (1 fail + 1 retry), got %d", attempts)
	}
}

// Checks retry exhaustion on persistent 5xx returns wrapped error.
func TestOpenRouterChat_ServerErrorRetryExhaustion(t *testing.T) {
	var attempts int

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer srv.Close()

	p := newOpenRouterProvider(srv.URL, "key", "test-model", config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion, got nil")
	}
	if !strings.Contains(err.Error(), "retry") {
		t.Errorf("want error containing 'retry', got: %v", err)
	}
	if attempts != 2 {
		t.Errorf("want 2 attempts, got %d", attempts)
	}
}

// Checks base URL normalization strips trailing /v1.
func TestOpenRouterChat_NormalizeBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
				t.Errorf("want /v1/chat/completions, got %s", r.URL.Path)
			}
			// Should not double /v1.
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

	// Simulate a base URL that includes /v1 (should be normalized).
	p := newOpenRouterProvider(srv.URL+"/v1", "key", "test-model", config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}
