package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"shmorby/internal/config"
)

// Tests OpenAI ListModels returns model IDs from /v1/models.
func TestListModels_OpenAI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4o", "object": "model"},
					{"id": "gpt-4o-mini", "object": "model"},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "gpt-4o", 120,
		config.Config{},
	)

	models, err := p.ListModels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" {
		t.Errorf("want gpt-4o, got %s", models[0].ID)
	}
	if models[1].ID != "gpt-4o-mini" {
		t.Errorf("want gpt-4o-mini, got %s", models[1].ID)
	}
	// OpenAI list endpoint does not provide context window.
	if models[0].ContextWindow != 0 {
		t.Errorf("want 0 context window, got %d",
			models[0].ContextWindow)
	}
}

// Tests OpenAI ListModels returns error on HTTP failure.
func TestListModels_OpenAI_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer srv.Close()

	p := newOpenAIProvider(
		srv.URL, "key", "", "gpt-4o", 120,
		config.Config{},
	)

	_, err := p.ListModels(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Tests OpenRouter ListModels returns models with context_length.
func TestListModels_OpenRouter_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "model-a", "context_length": 8192},
					{"id": "model-b", "context_length": 32768},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpenRouterProvider(
		srv.URL, "key", "model-a", config.Config{},
	)

	models, err := p.ListModels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].ContextWindow != 8192 {
		t.Errorf("want 8192, got %d", models[0].ContextWindow)
	}
	if models[1].ContextWindow != 32768 {
		t.Errorf("want 32768, got %d", models[1].ContextWindow)
	}
}

// Tests OpenRouter ListModels returns error on HTTP failure.
func TestListModels_OpenRouter_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		},
	))
	defer srv.Close()

	p := newOpenRouterProvider(
		srv.URL, "bad-key", "model-a", config.Config{},
	)

	_, err := p.ListModels(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Tests Zen ListModels returns models with context_length.
func TestListModels_Zen_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "claude-sonnet-4", "context_length": 200000},
					{"id": "gpt-5.4", "context_length": 1050000},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpencodeZenProvider(
		srv.URL, "key", "claude-sonnet-4", config.Config{},
	)

	models, err := p.ListModels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].ContextWindow != 200000 {
		t.Errorf("want 200000, got %d", models[0].ContextWindow)
	}
	if models[1].ContextWindow != 1050000 {
		t.Errorf("want 1050000, got %d", models[1].ContextWindow)
	}
}

// Tests Zen ListModels returns error on HTTP failure.
func TestListModels_Zen_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
	))
	defer srv.Close()

	p := newOpencodeZenProvider(
		srv.URL, "bad-key", "model", config.Config{},
	)

	_, err := p.ListModels(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Tests Zen ListModels queries the Go subscription endpoint when the
// current model carries the opencode-go/ prefix.
func TestListModels_Zen_GoModelQueriesGoEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "mimo-v2.5"},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpencodeZenProvider(
		srv.URL, "key", "opencode-go/mimo-v2.5", config.Config{},
	)

	models, err := p.ListModels(context.Background(), "opencode-go/mimo-v2.5")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if gotPath != "/go/v1/models" {
		t.Errorf("want /go/v1/models, got %s", gotPath)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 model, got %d", len(models))
	}
	if models[0].ID != "mimo-v2.5" {
		t.Errorf("want mimo-v2.5, got %s", models[0].ID)
	}
}

// Tests Zen ListModels queries the Go subscription endpoint when a
// model switch passes an opencode-go/ model to a provider configured
// with a pay-as-you-go model.
func TestListModels_Zen_ModelSwitchQueriesGoEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "mimo-v2.5"},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpencodeZenProvider(
		srv.URL, "key", "claude-sonnet-4", config.Config{},
	)

	models, err := p.ListModels(context.Background(), "opencode-go/mimo-v2.5")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if gotPath != "/go/v1/models" {
		t.Errorf("want /go/v1/models, got %s", gotPath)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 model, got %d", len(models))
	}
	if models[0].ID != "mimo-v2.5" {
		t.Errorf("want mimo-v2.5, got %s", models[0].ID)
	}
}

// Tests Zen ListModels falls back to the context-window registry when
// the API omits context_length, matching fetchModelInfo.
func TestListModels_Zen_ContextWindowFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "mimo-v2.5"},
				},
			})
		},
	))
	defer srv.Close()

	p := newOpencodeZenProvider(
		srv.URL, "key", "mimo-v2.5", config.Config{},
	)

	models, err := p.ListModels(context.Background(), "mimo-v2.5")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 model, got %d", len(models))
	}
	if models[0].ContextWindow != 128000 {
		t.Errorf("want 128000, got %d", models[0].ContextWindow)
	}
}

// Tests Ollama ListModels returns model names from /api/tags.
func TestListModels_Ollama_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "llama3:8b", "size": 4700000000},
					{"name": "codellama:13b", "size": 7400000000},
				},
			})
		},
	))
	defer srv.Close()

	p := newOllamaProvider(srv.URL, "llama3:8b", config.Config{})

	models, err := p.ListModels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].ID != "llama3:8b" {
		t.Errorf("want llama3:8b, got %s", models[0].ID)
	}
	if models[1].ID != "codellama:13b" {
		t.Errorf("want codellama:13b, got %s", models[1].ID)
	}
	// Ollama /api/tags does not provide context window.
	if models[0].ContextWindow != 0 {
		t.Errorf("want 0 context window, got %d",
			models[0].ContextWindow)
	}
}

// Tests Ollama ListModels returns error on HTTP failure.
func TestListModels_Ollama_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer srv.Close()

	p := newOllamaProvider(srv.URL, "model", config.Config{})

	_, err := p.ListModels(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Tests FormatSummary renders ID only when no capabilities.
func TestFormatSummary_IDOnly(t *testing.T) {
	m := ModelSummary{ID: "gpt-4o"}
	got := m.FormatSummary()
	if got != "gpt-4o" {
		t.Errorf("want %q, got %q", "gpt-4o", got)
	}
}

// Tests FormatSummary renders context window when present.
func TestFormatSummary_WithContext(t *testing.T) {
	m := ModelSummary{ID: "model-a", ContextWindow: 128000}
	got := m.FormatSummary()
	want := "model-a  ctx 128k"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// Tests FormatSummary rounds a context window that is not a multiple
// of 1000, matching the TUI formatter.
func TestFormatSummary_NonKContext(t *testing.T) {
	m := ModelSummary{ID: "model-d", ContextWindow: 16385}
	got := m.FormatSummary()
	want := "model-d  ctx 16k"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// Tests FormatSummary leaves a sub-1000 context window unrounded.
func TestFormatSummary_Sub1kContext(t *testing.T) {
	m := ModelSummary{ID: "model-e", ContextWindow: 512}
	got := m.FormatSummary()
	want := "model-e  ctx 512"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// Tests provider implements ModelLister interface.
func TestModelListerInterface(t *testing.T) {
	// Verify each provider type satisfies ModelLister at compile time.
	var _ ModelLister = (*openaiProvider)(nil)
	var _ ModelLister = (*openRouterProvider)(nil)
	var _ ModelLister = (*opencodeZenProvider)(nil)
	var _ ModelLister = (*ollamaProvider)(nil)
}
