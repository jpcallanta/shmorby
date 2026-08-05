package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"shmorby/internal/audit"
)

//go:embed websearch.txt
var websearchDescription string

var websearchParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"query": {
			"type": "string",
			"description": "search query"
		},
		"max_results": {
			"type": "integer",
			"description": "maximum number of results to return (default: 5, max: 20)"
		}
	},
	"required": ["query"]
}`)

type websearchArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// searxngResult represents a single SearXNG search result.
type searxngResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// searxngResponse represents the SearXNG JSON API response.
type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

// exaResult represents a single Exa API search result.
type exaResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// exaResponse represents the Exa API JSON response.
type exaResponse struct {
	Results []exaResult `json:"results"`
}

// WebSearchTool implements Tool for web search via SearXNG or Exa backend.
type WebSearchTool struct {
	perm           string
	client         HTTPClient
	baseURL        string // SearXNG instance URL
	engine         string // "searxng" or "exa"
	exaAPIKey      string // Exa API key
	defaultTimeout int
	auditLogger    *audit.Logger
}

// NewWebSearchTool creates a WebSearchTool with the given permission
// level and HTTP client. Pass nil client to use a default http.Client
// (no timeout — the context deadline in Run controls the timeout).
func NewWebSearchTool(permLevel string, client HTTPClient) *WebSearchTool {
	if client == nil {
		client = &http.Client{}
	}
	return &WebSearchTool{
		perm:           permLevel,
		client:         client,
		engine:         "searxng",
		defaultTimeout: 30,
	}
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (w *WebSearchTool) SetDefaultTimeout(t int) {
	if t > 0 {
		w.defaultTimeout = t
	}
}

// Name returns the tool name.
func (w *WebSearchTool) Name() string { return "websearch" }

// Description returns the embedded LLM description.
func (w *WebSearchTool) Description() string { return websearchDescription }

// Parameters returns the JSON schema for websearch parameters.
func (w *WebSearchTool) Parameters() json.RawMessage { return websearchParams }

// PermLevel returns the configured permission level.
func (w *WebSearchTool) PermLevel() string { return w.perm }

// SetPerm updates the permission level at runtime.
func (w *WebSearchTool) SetPerm(level string) { w.perm = level }

// SetAuditLogger sets the audit logger for this tool.
func (w *WebSearchTool) SetAuditLogger(l *audit.Logger) {
	w.auditLogger = l
}

// SetBaseURL sets the SearXNG instance URL.
func (w *WebSearchTool) SetBaseURL(u string) { w.baseURL = u }

// SetEngine sets the search backend: "searxng" or "exa".
func (w *WebSearchTool) SetEngine(e string) { w.engine = e }

// SetExaAPIKey sets the Exa API key for the Exa backend.
func (w *WebSearchTool) SetExaAPIKey(k string) { w.exaAPIKey = k }

// Run executes a web search using the configured backend.
func (w *WebSearchTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a websearchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf(
			"invalid websearch args: %w - "+
				`expected {"query":"<search terms>","max_results":<int>}`,
			err,
		)
	}
	if a.Query == "" {
		return "", fmt.Errorf(
			`websearch: missing required field "query"`,
		)
	}

	maxResults := a.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}

	start := time.Now()

	timeout := w.defaultTimeout
	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	var result string
	var err error

	switch w.engine {
	case "searxng":
		result, err = w.searchSearXNG(cmdCtx, a.Query, maxResults)
	case "exa":
		result, err = w.searchExa(cmdCtx, a.Query, maxResults)
	default:
		return "", fmt.Errorf(
			"websearch: unknown engine %q (want searxng or exa)",
			w.engine,
		)
	}

	elapsed := time.Since(start)
	slog.Info("tool run",
		"tool", "websearch",
		"engine", w.engine,
		"duration_ms", elapsed.Milliseconds(),
	)

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	w.logAudit(string(args), elapsed, errMsg)

	if err != nil {
		return result, err
	}

	return string(TruncateOutput([]byte(result))), nil
}

// searchSearXNG queries a SearXNG instance for search results.
func (w *WebSearchTool) searchSearXNG(
	ctx context.Context, query string, maxResults int,
) (string, error) {
	if w.baseURL == "" {
		return "", fmt.Errorf("websearch: no base_url configured for SearXNG")
	}

	searchURL := fmt.Sprintf(
		"%s/search?q=%s&format=json&number_of_results=%d",
		strings.TrimRight(w.baseURL, "/"),
		url.QueryEscape(query),
		maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("websearch: build request: %w", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("websearch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"websearch: GET %s returned %d", searchURL, resp.StatusCode,
		)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
	if err != nil {
		return "", fmt.Errorf("websearch: read response: %w", err)
	}

	var searxResp searxngResponse
	if err := json.Unmarshal(body, &searxResp); err != nil {
		return "", fmt.Errorf(
			"websearch: parse SearXNG response: %w", err,
		)
	}

	// Cap results at maxResults (SearXNG may ignore number_of_results).
	if len(searxResp.Results) > maxResults {
		searxResp.Results = searxResp.Results[:maxResults]
	}

	return formatSearchResults(searxResp.Results), nil
}

// searchExa queries the Exa API for search results.
func (w *WebSearchTool) searchExa(
	ctx context.Context, query string, maxResults int,
) (string, error) {
	if w.exaAPIKey == "" {
		return "", fmt.Errorf(
			"websearch: Exa engine selected but no exa_api_key configured",
		)
	}

	payload := map[string]interface{}{
		"query":      query,
		"numResults": maxResults,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("websearch: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		"https://api.exa.ai/search",
		strings.NewReader(string(payloadBytes)),
	)
	if err != nil {
		return "", fmt.Errorf("websearch: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", w.exaAPIKey)

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("websearch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"websearch: POST https://api.exa.ai/search returned %d",
			resp.StatusCode,
		)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
	if err != nil {
		return "", fmt.Errorf("websearch: read response: %w", err)
	}

	var exaResp exaResponse
	if err := json.Unmarshal(body, &exaResp); err != nil {
		return "", fmt.Errorf(
			"websearch: parse Exa response: %w", err,
		)
	}

	return formatExaResults(exaResp.Results), nil
}

// formatSearchResults formats SearXNG results into a numbered list.
func formatSearchResults(results []searxngResult) string {
	if len(results) == 0 {
		return "no results found"
	}

	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s",
			i+1, r.Title, r.URL, r.Content,
		))
	}

	return sb.String()
}

// formatExaResults formats Exa results into a numbered list.
func formatExaResults(results []exaResult) string {
	if len(results) == 0 {
		return "no results found"
	}

	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s",
			i+1, r.Title, r.URL, r.Snippet,
		))
	}

	return sb.String()
}

// logAudit sends a tool-run entry to the audit logger if configured.
func (w *WebSearchTool) logAudit(
	args string, duration time.Duration, errMsg string,
) {
	if w.auditLogger == nil {
		return
	}
	w.auditLogger.LogToolRun(
		audit.AuditEntry{
			Tool:       "websearch",
			Args:       args,
			DurationMs: duration.Milliseconds(),
			Error:      errMsg,
		},
		nil,
	)
}
