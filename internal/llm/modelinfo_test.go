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

// Tests OpenAI fetchModelInfo uses builtin registry when API
// returns no context_length (real OpenAI API shape).
func TestFetchModelInfo_OpenAI_BuiltinFallback(t *testing.T) {
	// Real OpenAI /v1/models/{model} returns a flat object
	// without context_length — mock that shape.
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "gpt-5.4-mini",
				"object":   "model",
				"owned_by": "openai",
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "gpt-5.4-mini", 120,
		config.Config{},
	)

	info, err := p.fetchModelInfo(
		context.Background(), "gpt-5.4-mini",
	)
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	// gpt-5.4-mini has a 400K context window per OpenAI docs
	// (272K max input, 400K total). Previously this was 272000
	// (the pricing tier threshold, not the actual window).
	if info.ContextWindow != 400000 {
		t.Errorf("want 400000, got %d", info.ContextWindow)
	}
	// gpt-5.4-mini supports 128K max output tokens.
	if info.MaxOutputTokens != 128000 {
		t.Errorf("want 128000 MaxOutputTokens, got %d",
			info.MaxOutputTokens)
	}
}

// Tests OpenAI fetchModelInfo prefers API-provided context fields
// over the builtin registry (forward-compat with a future API).
func TestFetchModelInfo_OpenAI_APITakesPrecedence(t *testing.T) {
	cases := []struct {
		name string
		resp map[string]any
		want int
	}{
		{
			name: "context_length",
			resp: map[string]any{
				"data": map[string]any{"context_length": 16384},
			},
			want: 16384,
		},
		{
			name: "max_context_length",
			resp: map[string]any{
				"data": map[string]any{"max_context_length": 32768},
			},
			want: 32768,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					json.NewEncoder(w).Encode(tc.resp)
				},
			))
			defer srv.Close()

			p := newOpenAIProvider(
				srv.URL, "key", "", "gpt-5.4-mini", 120,
				config.Config{},
			)

			info, err := p.fetchModelInfo(
				context.Background(), "gpt-5.4-mini",
			)
			if err != nil {
				t.Fatalf("fetchModelInfo: %v", err)
			}
			if info.ContextWindow != tc.want {
				t.Errorf("want %d, got %d", tc.want, info.ContextWindow)
			}
		})
	}
}

// Tests OpenAI fetchModelInfo resolves date-stamped snapshot IDs via
// the longest registry prefix (gpt-4o-mini-2024-07-18 → gpt-4o-mini).
func TestFetchModelInfo_OpenAI_SnapshotPrefixFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "gpt-4o-mini-2024-07-18",
				"object":   "model",
				"owned_by": "openai",
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "gpt-4o-mini-2024-07-18", 120,
		config.Config{},
	)

	info, err := p.fetchModelInfo(
		context.Background(), "gpt-4o-mini-2024-07-18",
	)
	if err != nil {
		t.Fatalf("fetchModelInfo: %v", err)
	}
	if info.ContextWindow != 128000 {
		t.Errorf("want 128000, got %d", info.ContextWindow)
	}
}

// Tests prefix fallback does not match across an alphanumeric
// boundary (gpt-4x must not resolve via the gpt-4 registry key).
func TestFetchModelInfo_OpenAI_PrefixBoundary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "gpt-4x",
				"object":   "model",
				"owned_by": "openai",
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "gpt-4x", 120,
		config.Config{},
	)

	_, err := p.fetchModelInfo(
		context.Background(), "gpt-4x",
	)
	if err == nil {
		t.Fatal("expected error for boundary-crossing model, got nil")
	}
}

// Tests OpenAI fetchModelInfo returns error for unknown model
// so FetchModelInfo falls through to config override.
func TestFetchModelInfo_OpenAI_UnknownModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "unknown-model",
				"object":   "model",
				"owned_by": "openai",
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "unknown-model", 120,
		config.Config{},
	)

	_, err := p.fetchModelInfo(
		context.Background(), "unknown-model",
	)
	if err == nil {
		t.Fatal(
			"expected error for unknown model, got nil",
		)
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

// Tests Ollama fetchModelInfo returns error when context_length is
// missing from the /api/show response. This forces
// FetchModelInfo to fall through to config override → fallback.
func TestFetchModelInfo_Ollama_MissingContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			// Omit context_length — common for most Ollama models.
			json.NewEncoder(w).Encode(map[string]any{
				"model_info": map[string]any{
					"family": "llama",
				},
			})
		},
	))
	defer srv.Close()

	p := newOllamaProvider(srv.URL, "test-model", config.Config{})

	_, err := p.fetchModelInfo(context.Background(), "test-model")
	if err == nil {
		t.Fatal(
			"expected error for missing context_length, got nil",
		)
	}
}

// Tests FetchModelInfo uses config override when Ollama API
// returns no context_length.
func TestFetchModelInfo_Ollama_MissingContextLength_Override(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"model_info": map[string]any{
					"family": "llama",
				},
			})
		},
	))
	defer srv.Close()

	p := newOllamaProvider(srv.URL, "test-model", config.Config{})
	cfg := config.Config{
		Models: map[string]config.ModelOverride{
			"test-model": {
				ContextWindow:   128000,
				MaxOutputTokens: 128000,
			},
		},
	}

	info, err := FetchModelInfo(
		context.Background(), p, "test-model", cfg,
	)
	if err != nil {
		t.Fatalf("FetchModelInfo: %v", err)
	}
	if info.ContextWindow != 128000 {
		t.Errorf("want 128000, got %d", info.ContextWindow)
	}
	if info.MaxOutputTokens != 128000 {
		t.Errorf("want 128000, got %d", info.MaxOutputTokens)
	}
}

// Tests FetchModelInfo falls back to fallback_context_window when
// Ollama API returns no context_length and no override is set.
func TestFetchModelInfo_Ollama_MissingContextLength_Fallback(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"model_info": map[string]any{
					"family": "llama",
				},
			})
		},
	))
	defer srv.Close()

	p := newOllamaProvider(srv.URL, "test-model", config.Config{})
	cfg := config.Config{
		Context: config.ContextConfig{
			FallbackContextWindow: 32000,
		},
	}

	info, err := FetchModelInfo(
		context.Background(), p, "test-model", cfg,
	)
	if err != ErrModelInfoFallback {
		t.Errorf("want ErrModelInfoFallback, got %v", err)
	}
	if info.ContextWindow != 32000 {
		t.Errorf("want 32000, got %d", info.ContextWindow)
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

// Tests the fallback guess is cached: a second lookup does not
// re-hit the network or repeat the API error path.
func TestFetchModelInfo_FallbackCached(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	callCount := 0
	fetcher := &failingFetcher{count: &callCount}
	cfg := config.Config{}

	// First call — cache miss, guess cached.
	_, err := FetchModelInfo(
		context.Background(), fetcher, "unknown-model", cfg,
	)
	if err != ErrModelInfoFallback {
		t.Errorf("want ErrModelInfoFallback, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("want 1 API call, got %d", callCount)
	}

	// Second call — cached guess, no network hit.
	_, err = FetchModelInfo(
		context.Background(), fetcher, "unknown-model", cfg,
	)
	if err != ErrModelInfoFallback {
		t.Errorf("want ErrModelInfoFallback, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("want 1 API call (cached), got %d", callCount)
	}
}

// Tests a config override added after the first miss still wins
// over the cached fallback guess.
func TestFetchModelInfo_GuessedOverrideRecheck(t *testing.T) {
	resetModelInfoCache()
	defer resetModelInfoCache()

	callCount := 0
	fetcher := &failingFetcher{count: &callCount}

	// First lookup — no override, fallback guess cached.
	cfg := config.Config{}
	_, err := FetchModelInfo(
		context.Background(), fetcher, "my-model", cfg,
	)
	if err != ErrModelInfoFallback {
		t.Errorf("want ErrModelInfoFallback, got %v", err)
	}

	// Override appears in config — must win over the cached guess.
	cfg.Models = map[string]config.ModelOverride{
		"my-model": {
			ContextWindow:   32768,
			MaxOutputTokens: 4096,
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
	if callCount != 1 {
		t.Errorf("want 1 API call, got %d", callCount)
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

// failingFetcher counts calls and always fails.
type failingFetcher struct {
	count *int
}

func (f *failingFetcher) fetchModelInfo(
	_ context.Context, _ string,
) (ModelInfo, error) {
	*f.count++
	return ModelInfo{}, fmt.Errorf("api down")
}

// --- Zen GPT-5.x context window consistency ---

// Checks Zen registry GPT-5.x context windows match the OpenAI
// registry (openai.go). Both must be kept in sync.
func TestZenModelContextWindows_GPT5Consistency(t *testing.T) {
	// Spot-check that Zen and OpenAI registries agree on key
	// GPT-5.x models. Full sync is enforced by code comments.
	cases := []struct {
		model string
		want  int
	}{
		{"gpt-5.6-sol", 1050000},
		{"gpt-5.5", 1050000},
		{"gpt-5.4", 1050000},
		{"gpt-5.4-mini", 400000},
		{"gpt-5.3-codex", 400000},
		{"gpt-5.2", 400000},
		{"gpt-5.1", 400000},
		{"gpt-5", 272000},
		{"gpt-5-nano", 272000},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			zenCW, ok := zenModelContextWindows[tc.model]
			if !ok {
				t.Fatalf(
					"model %q not in zenModelContextWindows",
					tc.model,
				)
			}
			if zenCW != tc.want {
				t.Errorf(
					"zen: model %q: want %d, got %d",
					tc.model, tc.want, zenCW,
				)
			}

			openaiCW, ok := openAIModelContextWindows[tc.model]
			if !ok {
				t.Fatalf(
					"model %q not in openAIModelContextWindows",
					tc.model,
				)
			}
			if openaiCW != zenCW {
				t.Errorf(
					"drift for %q: want %d, got %d",
					tc.model, zenCW, openaiCW,
				)
			}
		})
	}
}

// Checks dead GPT-5.x codex models removed from Zen registry.
func TestZenModelContextWindows_DeadModelsRemoved(t *testing.T) {
	dead := []string{
		"gpt-5-codex",
		"gpt-5.1-codex",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
		"gpt-5.2-codex",
	}

	for _, model := range dead {
		t.Run(model, func(t *testing.T) {
			_, ok := zenModelContextWindows[model]
			if ok {
				t.Errorf(
					"dead model %q should be removed "+
						"from Zen registry", model,
				)
			}
		})
	}
}
