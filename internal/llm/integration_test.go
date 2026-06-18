//go:build integration

package llm

import (
	"context"
	"os"
	"strings"
	"testing"

	"shmorby/internal/config"
)

// Tests that the Ollama provider can connect to a running local instance.
// Requires OLLAMA_BASE_URL to be set (per SPEC §11).
// Skipped when unset or when Ollama is not available.
func TestIntegration_OllamaChat(t *testing.T) {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		t.Skip("OLLAMA_BASE_URL not set")
	}

	p := newOllamaProvider(baseURL, "llama3.2", config.Config{})

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Say hello"},
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "no such host") ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "not found") {
			t.Skipf("Ollama not available: %v", err)
		}
		t.Fatalf("Chat: %v", err)
	}
}

// Tests that the OpenAI provider can connect with a valid API key.
// Requires OPENAI_API_KEY to be set.
func TestIntegration_OpenAIChat(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	p, err := newOpenAIProvider(config.Config{
		Model: "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("newOpenAIProvider: %v", err)
	}

	_, err = p.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "user", Content: "Say hello in one word"},
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "401") ||
			strings.Contains(err.Error(), "403") ||
			strings.Contains(err.Error(), "429") {
			t.Skipf("OpenAI auth/rate limited: %v", err)
		}
		t.Fatalf("Chat: %v", err)
	}
}
