package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockTransport implements http.RoundTripper for testing.
type mockTransport struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

// mockClient wraps a handler in an http.Client for tool constructors.
func mockClient(handler func(req *http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: &mockTransport{handler: handler}}
}

// TestWebSearch_Name checks Name returns "websearch".
func TestWebSearch_Name(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	if tool.Name() != "websearch" {
		t.Errorf("want 'websearch', got %q", tool.Name())
	}
}

// TestWebSearch_Description_NotEmpty checks Description is non-empty.
func TestWebSearch_Description_NotEmpty(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	if tool.Description() == "" {
		t.Error("want non-empty description")
	}
}

// TestWebSearch_Parameters_ValidJSON checks Parameters is valid JSON.
func TestWebSearch_Parameters_ValidJSON(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	var v interface{}
	if err := json.Unmarshal(tool.Parameters(), &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestWebSearch_SearXNG_Basic checks SearXNG backend returns formatted results.
func TestWebSearch_SearXNG_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "Result One", "url": "https://example.com/1", "content": "Snippet one"},
				{"title": "Result Two", "url": "https://example.com/2", "content": "Snippet two"}
			]
		}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	out, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "Result One") {
		t.Errorf("want output containing 'Result One', got %q", out)
	}
	if !strings.Contains(out, "https://example.com/1") {
		t.Errorf("want URL in output, got %q", out)
	}
	if !strings.Contains(out, "Snippet one") {
		t.Errorf("want snippet in output, got %q", out)
	}
	if !strings.Contains(out, "Result Two") {
		t.Errorf("want 'Result Two' in output, got %q", out)
	}
}

// TestWebSearch_SearXNG_Empty checks empty results returns "no results found".
func TestWebSearch_SearXNG_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	out, err := tool.Run(context.Background(), []byte(`{"query": "nothing"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "no results found" {
		t.Errorf("want 'no results found', got %q", out)
	}
}

// TestWebSearch_SearXNG_Error checks server error returns error.
func TestWebSearch_SearXNG_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for 500 status, got nil")
	}
}

// TestWebSearch_SearXNG_ReadError checks body read error returns error.
func TestWebSearch_SearXNG_ReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1000")
		// Close connection before sending body to force read error.
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for read failure, got nil")
	}
}

// TestWebSearch_SearXNG_ParseError checks invalid JSON response returns error.
func TestWebSearch_SearXNG_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `not json at all`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for parse failure, got nil")
	}
	if !strings.Contains(err.Error(), "parse SearXNG response") {
		t.Errorf("want parse error, got %v", err)
	}
}

// TestWebSearch_SearXNG_MaxResultsPostFilter checks results are capped.
func TestWebSearch_SearXNG_MaxResultsPostFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "R1", "url": "https://a.com", "content": "s1"},
				{"title": "R2", "url": "https://b.com", "content": "s2"},
				{"title": "R3", "url": "https://c.com", "content": "s3"}
			]
		}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	out, err := tool.Run(context.Background(), []byte(`{"query": "test", "max_results": 2}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out, "R3") {
		t.Errorf("want results capped at 2, but R3 present in output: %q", out)
	}
	if !strings.Contains(out, "R1") || !strings.Contains(out, "R2") {
		t.Errorf("want R1 and R2 in output, got %q", out)
	}
}

// TestWebSearch_SearXNG_DefaultMaxResults checks default max_results is 5.
func TestWebSearch_SearXNG_DefaultMaxResults(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(gotQ, "number_of_results=5") {
		t.Errorf("want default max_results=5, got query %q", gotQ)
	}
}

// TestWebSearch_Exa_Basic checks Exa backend returns formatted results.
func TestWebSearch_Exa_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error": "unauthorized"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "Exa Result", "url": "https://example.com/exa", "snippet": "Exa snippet"}
			]
		}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("test-key")
	tool.client = mockClient(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return srv.Client().Do(req)
	},
	)

	out, err := tool.Run(context.Background(), []byte(`{"query": "exa test"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "Exa Result") {
		t.Errorf("want 'Exa Result' in output, got %q", out)
	}
	if !strings.Contains(out, "Exa snippet") {
		t.Errorf("want 'Exa snippet' in output, got %q", out)
	}
}

// TestWebSearch_Exa_Empty checks Exa empty results returns "no results found".
func TestWebSearch_Exa_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("key")
	tool.client = mockClient(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return srv.Client().Do(req)
	},
	)

	out, err := tool.Run(context.Background(), []byte(`{"query": "nothing"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "no results found" {
		t.Errorf("want 'no results found', got %q", out)
	}
}

// TestWebSearch_Exa_ReadError checks Exa body read error returns error.
func TestWebSearch_Exa_ReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("key")
	tool.client = mockClient(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return srv.Client().Do(req)
	},
	)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for read failure, got nil")
	}
}

// TestWebSearch_Exa_ParseError checks Exa invalid JSON response returns error.
func TestWebSearch_Exa_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `not json`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("key")
	tool.client = mockClient(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return srv.Client().Do(req)
	},
	)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for parse failure, got nil")
	}
	if !strings.Contains(err.Error(), "parse Exa response") {
		t.Errorf("want parse error, got %v", err)
	}
}

// TestWebSearch_Exa_NoKey checks Exa without API key returns error.
func TestWebSearch_Exa_NoKey(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	tool.SetEngine("exa")

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for missing Exa API key, got nil")
	}
	if !strings.Contains(err.Error(), "no exa_api_key configured") {
		t.Errorf("want exa_api_key error, got %v", err)
	}
}

// TestWebSearch_Exa_Error checks Exa 401 returns error.
func TestWebSearch_Exa_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": "unauthorized"}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("bad-key")
	tool.client = mockClient(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return srv.Client().Do(req)
	},
	)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for 401 status, got nil")
	}
}

// TestWebSearch_Exa_NetworkError checks Exa network error returns error.
func TestWebSearch_Exa_NetworkError(t *testing.T) {
	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection refused")
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("key")

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("want 'request failed' error, got %v", err)
	}
}

// TestWebSearch_MissingQuery checks missing query returns error.
func TestWebSearch_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("want error for missing query, got nil")
	}
	if !strings.Contains(err.Error(), "missing required field") {
		t.Errorf("want missing field error, got %v", err)
	}
}

// TestWebSearch_InvalidEngine checks unknown engine returns error.
func TestWebSearch_InvalidEngine(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	tool.SetEngine("google")
	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for unknown engine, got nil")
	}
	if !strings.Contains(err.Error(), "unknown engine") {
		t.Errorf("want unknown engine error, got %v", err)
	}
}

// TestWebSearch_InvalidJSON checks bad JSON returns error.
func TestWebSearch_InvalidJSON(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

// TestWebSearch_MaxResults_Cap checks max_results is capped at 20.
func TestWebSearch_MaxResults_Cap(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	_, err := tool.Run(context.Background(), []byte(`{"query": "test", "max_results": 100}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(gotQ, "number_of_results=20") {
		t.Errorf("want max_results capped at 20, got query %q", gotQ)
	}
}

// TestWebSearch_SearXNG_NoBaseURL checks missing base URL returns error.
func TestWebSearch_SearXNG_NoBaseURL(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	// baseURL is empty by default.
	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for missing base URL, got nil")
	}
	if !strings.Contains(err.Error(), "no base_url configured") {
		t.Errorf("want base_url error, got %v", err)
	}
}

// TestWebSearch_PermLevel checks PermLevel and SetPerm.
func TestWebSearch_PermLevel(t *testing.T) {
	tool := NewWebSearchTool("ask", nil)
	if tool.PermLevel() != "ask" {
		t.Errorf("want 'ask', got %q", tool.PermLevel())
	}
	tool.SetPerm("allow")
	if tool.PermLevel() != "allow" {
		t.Errorf("want 'allow' after SetPerm, got %q", tool.PermLevel())
	}
}

// TestWebSearch_Timeout checks context cancellation propagates.
func TestWebSearch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is done.
		<-r.Context().Done()
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetBaseURL(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tool.Run(ctx, []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
}

// TestWebSearch_Exa_MultiResult checks Exa formats multiple results correctly.
func TestWebSearch_Exa_MultiResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [
				{"title": "First", "url": "https://a.com/1", "snippet": "snippet one"},
				{"title": "Second", "url": "https://b.com/2", "snippet": "snippet two"}
			]
		}`)
	}))
	defer srv.Close()

	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return srv.Client().Do(req)
	},
	))
	tool.SetEngine("exa")
	tool.SetExaAPIKey("key")
	tool.client = mockClient(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return srv.Client().Do(req)
	},
	)

	out, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "1. First") {
		t.Errorf("want '1. First' in output, got %q", out)
	}
	if !strings.Contains(out, "2. Second") {
		t.Errorf("want '2. Second' in output, got %q", out)
	}
	if !strings.Contains(out, "snippet one") {
		t.Errorf("want 'snippet one' in output, got %q", out)
	}
	if !strings.Contains(out, "snippet two") {
		t.Errorf("want 'snippet two' in output, got %q", out)
	}
}

// TestWebSearch_SetDefaultTimeout checks SetDefaultTimeout works.
func TestWebSearch_SetDefaultTimeout(t *testing.T) {
	tool := NewWebSearchTool("allow", nil)
	if tool.defaultTimeout != 30 {
		t.Errorf("want default timeout 30, got %d", tool.defaultTimeout)
	}
	tool.SetDefaultTimeout(60)
	if tool.defaultTimeout != 60 {
		t.Errorf("want timeout 60 after Set, got %d", tool.defaultTimeout)
	}
	// Zero/negative should be no-op.
	tool.SetDefaultTimeout(0)
	if tool.defaultTimeout != 60 {
		t.Errorf("want timeout unchanged after 0, got %d", tool.defaultTimeout)
	}
}

// TestWebSearch_SearXNG_NetworkError checks network error returns error.
func TestWebSearch_SearXNG_NetworkError(t *testing.T) {
	tool := NewWebSearchTool("allow", mockClient(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection refused")
	},
	))
	tool.SetBaseURL("http://localhost:1")

	_, err := tool.Run(context.Background(), []byte(`{"query": "test"}`))
	if err == nil {
		t.Fatal("want error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("want 'request failed' error, got %v", err)
	}
}
