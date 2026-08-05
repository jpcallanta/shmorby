package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestWebFetch_Name checks Name returns "webfetch".
func TestWebFetch_Name(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	if tool.Name() != "webfetch" {
		t.Errorf("want 'webfetch', got %q", tool.Name())
	}
}

// TestWebFetch_Description_NotEmpty checks Description is non-empty.
func TestWebFetch_Description_NotEmpty(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	if tool.Description() == "" {
		t.Error("want non-empty description")
	}
}

// TestWebFetch_Parameters_ValidJSON checks Parameters is valid JSON.
func TestWebFetch_Parameters_ValidJSON(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	var v interface{}
	if err := json.Unmarshal(tool.Parameters(), &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestWebFetch_Basic checks fetching a URL returns the body.
func TestWebFetch_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body>Hello, World!</body></html>")
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	out, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "Hello, World!") {
		t.Errorf("want body content, got %q", out)
	}
}

// TestWebFetch_StatusError checks 404 returns error with status.
func TestWebFetch_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	_, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page"}`))
	if err == nil {
		t.Fatal("want error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("want status 404 in error, got %v", err)
	}
}

// TestWebFetch_Truncation checks body is truncated at max_bytes.
func TestWebFetch_Truncation(t *testing.T) {
	body := strings.Repeat("A", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	out, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page", "max_bytes": 100}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "(truncated after 100 bytes)") {
		t.Errorf("want truncation notice, got %q", out)
	}
}

// TestWebFetch_NoTruncation_ExactSize checks body exactly maxBytes is not truncated.
func TestWebFetch_NoTruncation_ExactSize(t *testing.T) {
	body := strings.Repeat("A", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	out, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page", "max_bytes": 100}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("want no truncation for exact-size body, got %q", out)
	}
	if out != body {
		t.Errorf("want body equal to input, got %q", out)
	}
}

// TestWebFetch_InvalidURL_FTP checks ftp:// URLs are rejected.
func TestWebFetch_InvalidURL_FTP(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "ftp://example.com"}`))
	if err == nil {
		t.Fatal("want error for ftp URL, got nil")
	}
	if !strings.Contains(err.Error(), "only http/https") {
		t.Errorf("want http/https error, got %v", err)
	}
}

// TestWebFetch_InvalidURL_BadFormat checks malformed URL returns error.
func TestWebFetch_InvalidURL_BadFormat(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "://bad"}`))
	if err == nil {
		t.Fatal("want error for malformed URL, got nil")
	}
}

// TestWebFetch_MissingURL checks missing url field returns error.
func TestWebFetch_MissingURL(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("want error for missing url, got nil")
	}
	if !strings.Contains(err.Error(), "missing required field") {
		t.Errorf("want missing field error, got %v", err)
	}
}

// TestWebFetch_InvalidJSON checks bad JSON returns error.
func TestWebFetch_InvalidJSON(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

// TestWebFetch_EmptyBody checks empty 200 returns empty string.
func TestWebFetch_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	out, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "" {
		t.Errorf("want empty string, got %q", out)
	}
}

// TestWebFetch_MaxBytes_Cap checks max_bytes is capped at 1 MiB.
func TestWebFetch_MaxBytes_Cap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("B", 2*1024*1024))
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	out, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page", "max_bytes": 2000000}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Should truncate at 1 MiB, not 2 MiB.
	if !strings.Contains(out, "(truncated after 1048576 bytes)") {
		t.Errorf("want truncation at 1 MiB, got %q", out)
	}
}

// TestWebFetch_NullByteStripping checks null bytes are removed.
func TestWebFetch_NullByteStripping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello\x00world")
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	out, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out, "\x00") {
		t.Errorf("want null bytes stripped, got %q", out)
	}
	if !strings.Contains(out, "helloworld") {
		t.Errorf("want 'helloworld', got %q", out)
	}
}

// TestWebFetch_PrivateIP_Loopback checks loopback IP is blocked.
func TestWebFetch_PrivateIP_Loopback(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://127.0.0.1/secret"}`))
	if err == nil {
		t.Fatal("want error for loopback IP, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_PrivateIP_Localhost checks localhost is blocked.
func TestWebFetch_PrivateIP_Localhost(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://localhost/secret"}`))
	if err == nil {
		t.Fatal("want error for localhost, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_PrivateIP_RFC1918 checks RFC1918 IPs are blocked.
func TestWebFetch_PrivateIP_RFC1918(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	for _, ip := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1"} {
		_, err := tool.Run(context.Background(), []byte(`{"url": "http://`+ip+`/secret"}`))
		if err == nil {
			t.Errorf("want error for %s, got nil", ip)
		}
	}
}

// TestWebFetch_PrivateIP_MetadataEndpoint checks cloud metadata is blocked.
func TestWebFetch_PrivateIP_MetadataEndpoint(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://169.254.169.254/latest/meta-data/"}`))
	if err == nil {
		t.Fatal("want error for metadata endpoint, got nil")
	}
}

// TestWebFetch_PrivateIP_IPv6_UniqueLocal checks IPv6 unique-local is blocked.
func TestWebFetch_PrivateIP_IPv6_UniqueLocal(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://[fc00::1]/secret"}`))
	if err == nil {
		t.Fatal("want error for IPv6 unique-local, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_PrivateIP_IPv6_LinkLocal checks IPv6 link-local is blocked.
func TestWebFetch_PrivateIP_IPv6_LinkLocal(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://[fe80::1]/secret"}`))
	if err == nil {
		t.Fatal("want error for IPv6 link-local, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_PrivateIP_DNSResolved checks hostname resolving to private IP is blocked.
func TestWebFetch_PrivateIP_DNSResolved(t *testing.T) {
	origResolve := resolveHost
	resolveHost = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.5")}, nil
	}
	defer func() { resolveHost = origResolve }()

	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://internal.corp/secret"}`))
	if err == nil {
		t.Fatal("want error for DNS-resolved private IP, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_PrivateIP_DNSMixedResolution checks that a hostname
// resolving to a mix of public and private IPs is blocked (anyPrivate).
func TestWebFetch_PrivateIP_DNSMixedResolution(t *testing.T) {
	origResolve := resolveHost
	resolveHost = func(host string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("8.8.8.8"),
			net.ParseIP("10.0.0.5"),
		}, nil
	}
	defer func() { resolveHost = origResolve }()

	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://mixed.example.com/secret"}`))
	if err == nil {
		t.Fatal("want error for mixed-resolution hostname with private IP, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_RedirectSSRF_Blocked checks that HTTP redirects to
// private/loopback addresses are blocked by CheckRedirect.
func TestWebFetch_RedirectSSRF_Blocked(t *testing.T) {
	// Start a server that redirects to a loopback address.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://127.0.0.1/secret", http.StatusFound)
			return
		}
		fmt.Fprint(w, "reached private target")
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "`+srv.URL+`/redirect"}`))
	if err == nil {
		t.Fatal("want error for redirect to loopback, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_RedirectSSRF_DNSResolved checks that redirects to
// hostnames that resolve to private IPs are blocked.
func TestWebFetch_RedirectSSRF_DNSResolved(t *testing.T) {
	origResolve := resolveHost
	resolveHost = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.99")}, nil
	}
	defer func() { resolveHost = origResolve }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://internal.corp/secret", http.StatusFound)
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "`+srv.URL+`/redirect"}`))
	if err == nil {
		t.Fatal("want error for redirect to private-resolved hostname, got nil")
	}
	if !strings.Contains(err.Error(), "private/loopback") {
		t.Errorf("want private/loopback error, got %v", err)
	}
}

// TestWebFetch_DNSResolutionError checks DNS failure returns error.
func TestWebFetch_DNSResolutionError(t *testing.T) {
	origResolve := resolveHost
	resolveHost = func(host string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host}
	}
	defer func() { resolveHost = origResolve }()

	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "http://nonexistent.invalid/page"}`))
	if err == nil {
		t.Fatal("want error for DNS failure, got nil")
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("want resolve error, got %v", err)
	}
}

// TestWebFetch_PermLevel checks PermLevel and SetPerm.
func TestWebFetch_PermLevel(t *testing.T) {
	tool := NewWebFetchTool("deny", nil)
	if tool.PermLevel() != "deny" {
		t.Errorf("want 'deny', got %q", tool.PermLevel())
	}
	tool.SetPerm("allow")
	if tool.PermLevel() != "allow" {
		t.Errorf("want 'allow' after SetPerm, got %q", tool.PermLevel())
	}
}

// TestWebFetch_Timeout checks context cancellation propagates.
func TestWebFetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tool.Run(ctx, []byte(`{"url": "http://example.com/page"}`))
	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
}

// TestWebFetch_NetworkError checks network error returns error.
func TestWebFetch_NetworkError(t *testing.T) {
	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	})

	_, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page"}`))
	if err == nil {
		t.Fatal("want error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("want 'request failed' error, got %v", err)
	}
}

// TestWebFetch_BodyReadError checks body read error returns error.
func TestWebFetch_BodyReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	_, err := tool.Run(context.Background(), []byte(`{"url": "http://example.com/page"}`))
	if err == nil {
		t.Fatal("want error for body read failure, got nil")
	}
}

// TestWebFetch_SetDefaultTimeout checks SetDefaultTimeout works.
func TestWebFetch_SetDefaultTimeout(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
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

// TestWebFetch_ContextTimeout checks context timeout produces clear error.
func TestWebFetch_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, "delayed")
	}))
	defer srv.Close()

	tool := NewWebFetchTool("allow", &mockHTTPClient{
		handler: func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return srv.Client().Do(req)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tool.Run(ctx, []byte(`{"url": "http://example.com/page"}`))
	if err == nil {
		t.Fatal("want error for context timeout, got nil")
	}
}

// TestWebFetch_SchemeValidation_WebSocket checks ws:// URLs are rejected.
func TestWebFetch_SchemeValidation_WebSocket(t *testing.T) {
	tool := NewWebFetchTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{"url": "ws://example.com"}`))
	if err == nil {
		t.Fatal("want error for ws:// URL, got nil")
	}
	if !strings.Contains(err.Error(), "only http/https") {
		t.Errorf("want http/https error, got %v", err)
	}
}
