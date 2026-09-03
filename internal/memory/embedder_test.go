package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOllamaEmbedder_Embed_Success checks batch embedding via /api/embed.
func TestOllamaEmbedder_Embed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/embed" {
				t.Errorf("path: want /api/embed, got %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("method: want POST, got %s", r.Method)
			}

			var req ollamaEmbedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if req.Model != "nomic-embed-text" {
				t.Errorf("model: want nomic-embed-text, got %s", req.Model)
			}
			if len(req.Input) != 1 {
				t.Fatalf("want 1 input, got %d", len(req.Input))
			}
			if req.Input[0] != "hello world" {
				t.Errorf("input[0]: want 'hello world', got %s", req.Input[0])
			}
			// dimension=0 means "unset": no dimensions field should be sent.
			if req.Dimensions != 0 {
				t.Errorf("dimensions: want 0 (unset), got %d", req.Dimensions)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ollamaEmbedResponse{
				Embeddings: [][]float32{{0.1, 0.2, 0.3}},
			})
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 0)

	results, err := e.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if len(results[0]) != 3 {
		t.Fatalf("want 3 dims, got %d", len(results[0]))
	}
	if results[0][0] != 0.1 {
		t.Errorf("dim 0: want 0.1, got %f", results[0][0])
	}
}

// TestOllamaEmbedder_Embed_Batch checks multi-text batch embedding.
func TestOllamaEmbedder_Embed_Batch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req ollamaEmbedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if len(req.Input) != 2 {
				t.Fatalf("want 2 inputs, got %d", len(req.Input))
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ollamaEmbedResponse{
				Embeddings: [][]float32{
					{0.1, 0.2},
					{0.3, 0.4},
				},
			})
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 0)

	results, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0][0] != 0.1 || results[1][0] != 0.3 {
		t.Errorf("unexpected embeddings: %v", results)
	}
}

// TestOllamaEmbedder_Embed_Dimensions checks configurable dimensions.
func TestOllamaEmbedder_Embed_Dimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req ollamaEmbedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if req.Dimensions != 256 {
				t.Errorf("dimensions: want 256, got %d", req.Dimensions)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ollamaEmbedResponse{
				Embeddings: [][]float32{{0.1}},
			})
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 256)

	results, err := e.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
}

// TestOllamaEmbedder_Embed_ServerError checks error on non-200.
func TestOllamaEmbedder_Embed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("model not loaded"))
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 0)

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
}

// TestOllamaEmbedder_Embed_CountMismatch checks response count validation.
func TestOllamaEmbedder_Embed_CountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Return fewer embeddings than inputs.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ollamaEmbedResponse{
				Embeddings: [][]float32{{0.1}},
			})
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 0)

	_, err := e.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("want error on count mismatch, got nil")
	}
}

// TestOllamaEmbedder_Dimension returns 0 when unset (model default).
func TestOllamaEmbedder_Dimension(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost", "", 0)
	if e.Dimension() != 0 {
		t.Errorf("want 0 (unset), got %d", e.Dimension())
	}
}

// TestOllamaEmbedder_Dimension_Custom checks custom dimension.
func TestOllamaEmbedder_Dimension_Custom(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost", "", 256)
	if e.Dimension() != 256 {
		t.Errorf("want 256, got %d", e.Dimension())
	}
}

// TestOllamaEmbedder_DefaultModel uses nomic-embed-text when empty.
func TestOllamaEmbedder_DefaultModel(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost", "", 0)
	if e.model != "nomic-embed-text" {
		t.Errorf("want nomic-embed-text, got %s", e.model)
	}
}

// TestOpenAIEmbedder_Embed_Success checks batch embedding.
func TestOpenAIEmbedder_Embed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/embeddings" {
				t.Errorf("path: want /v1/embeddings, got %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("method: want POST, got %s", r.Method)
			}

			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-key" {
				t.Errorf("auth: want Bearer test-key, got %s", auth)
			}

			var req openaiEmbedRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if req.Model != "text-embedding-3-small" {
				t.Errorf("model: want text-embedding-3-small, got %s", req.Model)
			}
			if len(req.Input) != 2 {
				t.Fatalf("want 2 inputs, got %d", len(req.Input))
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(openaiEmbedResponse{
				Data: []openaiEmbedData{
					{Embedding: []float32{0.1, 0.2}, Index: 0},
					{Embedding: []float32{0.3, 0.4}, Index: 1},
				},
			})
		},
	))
	defer srv.Close()

	e := NewOpenAIEmbedder("test-key", srv.URL, "text-embedding-3-small")

	results, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0][0] != 0.1 || results[1][0] != 0.3 {
		t.Errorf("unexpected embeddings: %v", results)
	}
}

// TestOpenAIEmbedder_Embed_ServerError checks error on non-200.
func TestOpenAIEmbedder_Embed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("invalid key"))
		},
	))
	defer srv.Close()

	e := NewOpenAIEmbedder("bad-key", srv.URL, "text-embedding-3-small")

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error on 401, got nil")
	}
}

// TestOpenAIEmbedder_Dimension returns 1536.
func TestOpenAIEmbedder_Dimension(t *testing.T) {
	e := NewOpenAIEmbedder("key", "", "")
	if e.Dimension() != 1536 {
		t.Errorf("want 1536, got %d", e.Dimension())
	}
}

// TestOpenAIEmbedder_DefaultBaseURL uses api.openai.com when empty.
func TestOpenAIEmbedder_DefaultBaseURL(t *testing.T) {
	e := NewOpenAIEmbedder("key", "", "")
	if e.baseURL != "https://api.openai.com" {
		t.Errorf("want https://api.openai.com, got %s", e.baseURL)
	}
}

// TestOpenAIEmbedder_DefaultModel uses text-embedding-3-small when empty.
func TestOpenAIEmbedder_DefaultModel(t *testing.T) {
	e := NewOpenAIEmbedder("key", "", "")
	if e.model != "text-embedding-3-small" {
		t.Errorf("want text-embedding-3-small, got %s", e.model)
	}
}

// --- Embedder body limit tests ---

// TestOllamaEmbedder_Embed_ResponseTooLarge checks that a response
// body exceeding the 10 MiB limit is rejected (not OOM-killed).
func TestOllamaEmbedder_Embed_ResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Return a response body larger than 10 MiB.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"embeddings":[["`))
			// Write ~11 MiB of padding to exceed the limit.
			padding := make([]byte, 11<<20)
			for i := range padding {
				padding[i] = 'x'
			}
			w.Write(padding)
			w.Write([]byte(`"]]}`))
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 0)

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error for oversized response, got nil")
	}
	// The error should be a decode error (truncated JSON), not an OOM.
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("want decode error, got %v", err)
	}
}

// TestOpenAIEmbedder_Embed_ResponseTooLarge checks that a response
// body exceeding the 10 MiB limit is rejected.
func TestOpenAIEmbedder_Embed_ResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"embedding":[`))
			padding := make([]byte, 11<<20)
			for i := range padding {
				padding[i] = 'y'
			}
			w.Write(padding)
			w.Write([]byte(`],"index":0}]}`))
		},
	))
	defer srv.Close()

	e := NewOpenAIEmbedder("key", srv.URL, "")

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error for oversized response, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("want decode error, got %v", err)
	}
}

// --- API key redaction in error messages ---

// TestOllamaEmbedder_Embed_ErrorRedactsBody checks that error
// responses are truncated and redacted to prevent key leakage.
func TestOllamaEmbedder_Embed_ErrorRedactsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			// Echo back a fake API key in the error body.
			w.Write([]byte(`{"error":"invalid key sk-abc123456789012345678901234567890"}`))
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text", 0)

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error on 400, got nil")
	}
	// The error message should NOT contain the raw key.
	if strings.Contains(err.Error(), "sk-abc123456789012345678901234567890") {
		t.Errorf("want API key redacted from error, got: %v", err)
	}
}

// TestOpenAIEmbedder_Embed_ErrorRedactsBody checks that error
// responses are truncated and redacted.
func TestOpenAIEmbedder_Embed_ErrorRedactsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid api_key=sk-realkey123456789012345678901234567890"}`))
		},
	))
	defer srv.Close()

	e := NewOpenAIEmbedder("bad", srv.URL, "")

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error on 401, got nil")
	}
	if strings.Contains(err.Error(), "sk-realkey123456789012345678901234567890") {
		t.Errorf("want API key redacted from error, got: %v", err)
	}
}
