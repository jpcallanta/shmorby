package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOllamaEmbedder_Embed_Success checks a single text embedding.
func TestOllamaEmbedder_Embed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/embeddings" {
				t.Errorf("path: want /api/embeddings, got %s", r.URL.Path)
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
			if req.Prompt != "hello world" {
				t.Errorf("prompt: want 'hello world', got %s", req.Prompt)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ollamaEmbedResponse{
				Embedding: []float32{0.1, 0.2, 0.3},
			})
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text")

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

// TestOllamaEmbedder_Embed_ServerError checks error on non-200.
func TestOllamaEmbedder_Embed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("model not loaded"))
		},
	))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text")

	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
}

// TestOllamaEmbedder_Dimension returns 768.
func TestOllamaEmbedder_Dimension(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost", "")
	if e.Dimension() != 768 {
		t.Errorf("want 768, got %d", e.Dimension())
	}
}

// TestOllamaEmbedder_DefaultModel uses nomic-embed-text when empty.
func TestOllamaEmbedder_DefaultModel(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost", "")
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
