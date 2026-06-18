package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"shmorby/internal/config"
)

// mockFetcher returns a fetcher that always fails.
type mockFetcher struct {
	err error
}

func (m *mockFetcher) fetchModelInfo(
	_ context.Context, _ string,
) (ModelInfo, error) {
	return ModelInfo{}, m.err
}

// Resets the model info cache between tests.
func resetModelInfoCache() {
	modelInfoCache = sync.Map{}
}

// Tests OpenAI fetchModelInfo parses context_length.
func TestFetchModelInfo_OpenAI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"context_length": 16384,
				},
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "test-model", 120, config.Config{},
	)

	info, err := p.fetchModelInfo(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	if info.ContextWindow != 16384 {
		t.Errorf("want 16384, got %d", info.ContextWindow)
	}
}

// Tests OpenAI fetchModelInfo uses max_context_length fallback.
func TestFetchModelInfo_OpenAI_MaxContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"max_context_length": 32768,
				},
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "test-model", 120, config.Config{},
	)

	info, err := p.fetchModelInfo(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	if info.ContextWindow != 32768 {
		t.Errorf("want 32768, got %d", info.ContextWindow)
	}
}

// Tests Ollama fetchModelInfo parses model_info.context_length.
func TestFetchModelInfo_Ollama_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"model_info": map[string]any{
					"context_length": float64(8192),
				},
			})
		},
	))
	defer srv.Close()

	p := newOllamaProvider(srv.URL, "test-model", config.Config{})

	info, err := p.fetchModelInfo(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	if info.ContextWindow != 8192 {
		t.Errorf("want 8192, got %d", info.ContextWindow)
	}
}

// Tests OpenRouter fetchModelInfo finds model by ID.
func TestFetchModelInfo_OpenRouter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "other-model", "context_length": 4096},
					{"id": "test-model", "context_length": 24576},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpenRouterProvider(
		srv.URL, "key", "test-model", config.Config{},
	)

	info, err := p.fetchModelInfo(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	if info.ContextWindow != 24576 {
		t.Errorf("want 24576, got %d", info.ContextWindow)
	}
}

// Tests OpenCode Zen fetchModelInfo finds model by ID.
func TestFetchModelInfo_OpenCodeZen_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "zen-model", "context_length": 2048},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpencodeZenProvider(
		srv.URL, "key", "zen-model", config.Config{},
	)

	info, err := p.fetchModelInfo(context.Background(), "zen-model")
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	if info.ContextWindow != 2048 {
		t.Errorf("want 2048, got %d", info.ContextWindow)
	}
}

// Tests cache hit skips API call.
func TestFetchModelInfo_CacheHit(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	callCount := 0
	fetcher := &countingFetcher{count: &callCount}

	cfg := config.Config{}
	model := "cached-model"

	// First call — cache miss.
	_, err := FetchModelInfo(
		context.Background(), fetcher, model, cfg,
	)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if callCount != 1 {
		t.Errorf("want 1 API call, got %d", callCount)
	}

	// Second call — cache hit.
	_, err = FetchModelInfo(
		context.Background(), fetcher, model, cfg,
	)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if callCount != 1 {
		t.Errorf("want 1 API call (cached), got %d", callCount)
	}
}

// Tests config override used when API fails.
func TestFetchModelInfo_ConfigOverride(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	fetcher := &mockFetcher{err: fmt.Errorf("api down")}
	cfg := config.Config{
		Models: map[string]config.ModelOverride{
			"my-model": {
				ContextWindow:   32768,
				MaxOutputTokens: 4096,
			},
		},
	}

	info, err := FetchModelInfo(
		context.Background(), fetcher, "my-model", cfg,
	)
	if err != nil {
		t.Fatalf("FetchModelInfo: %v", err)
	}
	if info.ContextWindow != 32768 {
		t.Errorf("want 32768, got %d", info.ContextWindow)
	}
	if info.MaxOutputTokens != 4096 {
		t.Errorf("want 4096, got %d", info.MaxOutputTokens)
	}
}

// Tests fallback returns 8192 when API and config both fail.
func TestFetchModelInfo_Fallback(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	fetcher := &mockFetcher{err: fmt.Errorf("api down")}
	cfg := config.Config{}

	info, err := FetchModelInfo(
		context.Background(), fetcher, "unknown-model", cfg,
	)
	if err != ErrModelInfoFallback {
		t.Errorf("want ErrModelInfoFallback, got %v", err)
	}
	if info.ContextWindow != 8192 {
		t.Errorf("want 8192, got %d", info.ContextWindow)
	}
}

// Tests InvalidateModelInfo removes from cache.
func TestInvalidateModelInfo(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	callCount := 0
	fetcher := &countingFetcher{count: &callCount}
	cfg := config.Config{}
	model := "refresh-model"

	// Populate cache.
	FetchModelInfo(context.Background(), fetcher, model, cfg)
	if callCount != 1 {
		t.Fatalf("want 1 call, got %d", callCount)
	}

	// Invalidate and re-fetch.
	InvalidateModelInfo(model)
	FetchModelInfo(context.Background(), fetcher, model, cfg)
	if callCount != 2 {
		t.Errorf("want 2 calls after invalidation, got %d", callCount)
	}
}

// countingFetcher tracks how many times fetchModelInfo is called.
type countingFetcher struct {
	count *int
}

func (f *countingFetcher) fetchModelInfo(
	_ context.Context, _ string,
) (ModelInfo, error) {
	*f.count++
	return ModelInfo{ContextWindow: 16384}, nil
}
