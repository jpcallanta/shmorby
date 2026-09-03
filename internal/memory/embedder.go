package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"shmorby/internal/redact"
)

// Embedder generates vector embeddings for text.
type Embedder interface {
	// Embed returns embeddings for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the vector dimension for this embedder.
	Dimension() int
}

// OllamaEmbedder calls Ollama's /api/embed endpoint (batch-capable,
// supports Matryoshka dimension truncation for nomic-embed-text).
type OllamaEmbedder struct {
	baseURL   string
	model     string
	dimension int
}

// NewOllamaEmbedder creates an Ollama-based embedder. When dimension
// is 0 the server default is used (no dimensions field sent).
func NewOllamaEmbedder(baseURL, model string, dimension int) *OllamaEmbedder {
	if model == "" {
		model = "nomic-embed-text"
	}

	return &OllamaEmbedder{
		baseURL:   strings.TrimRight(baseURL, "/"),
		model:     model,
		dimension: dimension,
	}
}

type ollamaEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed sends all texts in a single batch request to /api/embed.
// The endpoint accepts an input array and returns embeddings in
// index-aligned order. When dimensions is configured (non-zero) the
// response is Matryoshka-truncated at the server.
func (e *OllamaEmbedder) Embed(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	req := ollamaEmbedRequest{
		Model: e.model,
		Input: texts,
	}
	if e.dimension > 0 {
		req.Dimensions = e.dimension
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := e.baseURL + "/api/embed"
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("exec request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Limit error response read to prevent OOM from a rogue
		// server, and redact the body to prevent API key echo-back
		// from leaking into logs.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama %d: %s",
			resp.StatusCode, redact.SecretString(string(b)))
	}

	// Limit response body to 10 MiB to prevent OOM from a rogue
	// embedding server returning an extremely large JSON response.
	var oResp ollamaEmbedResponse
	limitedBody := io.LimitReader(resp.Body, 10<<20)
	if err := json.NewDecoder(limitedBody).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(oResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf(
			"embeddings count mismatch: want %d, got %d",
			len(texts), len(oResp.Embeddings))
	}

	return oResp.Embeddings, nil
}

// Dimension returns the configured vector dimension.
func (e *OllamaEmbedder) Dimension() int {
	return e.dimension
}

// OpenAIEmbedder calls OpenAI's /v1/embeddings endpoint.
type OpenAIEmbedder struct {
	apiKey  string
	baseURL string
	model   string
}

// NewOpenAIEmbedder creates an OpenAI-based embedder.
func NewOpenAIEmbedder(apiKey, baseURL, model string) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	if model == "" {
		model = "text-embedding-3-small"
	}

	return &OpenAIEmbedder{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
	}
}

type openaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openaiEmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openaiEmbedResponse struct {
	Data []openaiEmbedData `json:"data"`
}

// Embed sends all texts in a single batch request.
func (e *OpenAIEmbedder) Embed(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	body, err := json.Marshal(openaiEmbedRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := e.baseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exec request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Limit error response read to prevent OOM from a rogue
		// server, and redact the body to prevent API key echo-back
		// from leaking into logs.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("openai %d: %s",
			resp.StatusCode, redact.SecretString(string(b)))
	}

	// Limit response body to 10 MiB to prevent OOM from a rogue
	// embedding server returning an extremely large JSON response.
	var oResp openaiEmbedResponse
	limitedBody := io.LimitReader(resp.Body, 10<<20)
	if err := json.NewDecoder(limitedBody).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([][]float32, len(texts))
	for _, d := range oResp.Data {
		if d.Index < len(results) {
			results[d.Index] = d.Embedding
		}
	}

	return results, nil
}

// Dimension returns the default dimension for text-embedding-3-small.
func (e *OpenAIEmbedder) Dimension() int {
	return 1536
}
