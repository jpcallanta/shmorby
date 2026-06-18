package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Embedder generates vector embeddings for text.
type Embedder interface {
	// Embed returns embeddings for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the vector dimension for this embedder.
	Dimension() int
}

// OllamaEmbedder calls Ollama's /api/embeddings endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
}

// NewOllamaEmbedder creates an Ollama-based embedder.
func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	if model == "" {
		model = "nomic-embed-text"
	}

	return &OllamaEmbedder{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed calls Ollama once per text (no batch support).
func (e *OllamaEmbedder) Embed(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	results := make([][]float32, len(texts))

	for i, text := range texts {
		emb, err := e.embedOne(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embed text %d: %w", i, err)
		}
		results[i] = emb
	}

	return results, nil
}

func (e *OllamaEmbedder) embedOne(
	ctx context.Context, text string,
) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbedRequest{
		Model:  e.model,
		Prompt: text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := e.baseURL + "/api/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exec request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama %d: %s", resp.StatusCode, b)
	}

	var oResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return oResp.Embedding, nil
}

// Dimension returns the default dimension for nomic-embed-text.
func (e *OllamaEmbedder) Dimension() int {
	return 768
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, b)
	}

	var oResp openaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
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
