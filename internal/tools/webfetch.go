package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"shmorby/internal/audit"
)

//go:embed webfetch.txt
var webfetchDescription string

var webfetchParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"url": {
			"type": "string",
			"description": "URL to fetch (http/https only)"
		},
		"max_bytes": {
			"type": "integer",
			"description": "maximum bytes to read from the response body (default: 65536, max: 1048576)"
		}
	},
	"required": ["url"]
}`)

type webfetchArgs struct {
	URL      string `json:"url"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

const (
	defaultMaxBytes  = 65536
	absoluteMaxBytes = 1048576
)

// WebFetchTool implements Tool for fetching web page content.
type WebFetchTool struct {
	perm           string
	client         HTTPClient
	defaultTimeout int
	auditLogger    *audit.Logger
}

// NewWebFetchTool creates a WebFetchTool with the given permission
// level and HTTP client. Pass nil client to use a default http.Client
// with SSRF-safe redirect validation (no timeout — the context
// deadline in Run controls the timeout).
func NewWebFetchTool(permLevel string, client HTTPClient) *WebFetchTool {
	if client == nil {
		client = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return validateURLNotPrivate(req.URL)
			},
		}
	}
	return &WebFetchTool{
		perm:           permLevel,
		client:         client,
		defaultTimeout: 30,
	}
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (w *WebFetchTool) SetDefaultTimeout(t int) {
	if t > 0 {
		w.defaultTimeout = t
	}
}

// Name returns the tool name.
func (w *WebFetchTool) Name() string { return "webfetch" }

// Description returns the embedded LLM description.
func (w *WebFetchTool) Description() string { return webfetchDescription }

// Parameters returns the JSON schema for webfetch parameters.
func (w *WebFetchTool) Parameters() json.RawMessage { return webfetchParams }

// PermLevel returns the configured permission level.
func (w *WebFetchTool) PermLevel() string { return w.perm }

// SetPerm updates the permission level at runtime.
func (w *WebFetchTool) SetPerm(level string) { w.perm = level }

// SetAuditLogger sets the audit logger for this tool.
func (w *WebFetchTool) SetAuditLogger(l *audit.Logger) {
	w.auditLogger = l
}

// isPrivateIP reports whether ip is a loopback, link-local, or RFC1918 address.
// Package-level var so tests can override for SSRF-check bypass.
var isPrivateIP = func(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1]&0xf0 == 16) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}
	// IPv6 unique-local (fc00::/7) and link-local (fe80::/10).
	return ip.IsPrivate()
}

// resolveHost resolves a hostname to IPs. Package-level var so tests
// can override to avoid real DNS lookups.
var resolveHost = net.LookupIP

// validateURLNotPrivate checks that a URL does not target a private,
// loopback, or link-local address. Called for the initial URL and by
// CheckRedirect on every redirect hop.
func validateURLNotPrivate(u *url.URL) error {
	hostname := u.Hostname()
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf(
				"webfetch: private/loopback " +
					"address is not allowed",
			)
		}
		return nil
	}
	ips, err := resolveHost(hostname)
	if err != nil {
		return fmt.Errorf(
			"webfetch: resolve hostname: %w", err,
		)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf(
				"webfetch: private/loopback " +
					"address is not allowed",
			)
		}
	}
	return nil
}

// Run fetches a URL and returns the response body as plain text.
func (w *WebFetchTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a webfetchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf(
			"invalid webfetch args: %w - "+
				`expected {"url":"<url>","max_bytes":<int>}`,
			err,
		)
	}
	if a.URL == "" {
		return "", fmt.Errorf(
			`webfetch: missing required field "url"`,
		)
	}

	// Validate URL scheme.
	parsed, err := url.Parse(a.URL)
	if err != nil {
		return "", fmt.Errorf("webfetch: invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf(
			"webfetch: only http/https URLs are supported",
		)
	}

	// Block private/loopback IPs to prevent SSRF.
	if err := validateURLNotPrivate(parsed); err != nil {
		return "", err
	}

	maxBytes := a.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxBytes > absoluteMaxBytes {
		maxBytes = absoluteMaxBytes
	}

	timeout := w.defaultTimeout
	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	start := time.Now()

	req, err := http.NewRequestWithContext(cmdCtx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fmt.Errorf("webfetch: build request: %w", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		elapsed := time.Since(start)
		errMsg := err.Error()
		if cmdCtx.Err() != nil {
			errMsg = "request cancelled or timed out"
		}
		w.logAudit(a.URL, string(args), elapsed, 0, errMsg)
		if cmdCtx.Err() != nil {
			return "", fmt.Errorf("webfetch: request cancelled or timed out")
		}
		return "", fmt.Errorf("webfetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		elapsed := time.Since(start)
		errMsg := fmt.Sprintf("GET %s returned %d", a.URL, resp.StatusCode)
		w.logAudit(a.URL, string(args), elapsed, resp.StatusCode, errMsg)
		return "", fmt.Errorf("webfetch: %s", errMsg)
	}

	// Read maxBytes+1 so we can detect truncation accurately.
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		elapsed := time.Since(start)
		w.logAudit(a.URL, string(args), elapsed, resp.StatusCode, err.Error())
		return "", fmt.Errorf("webfetch: read body: %w", err)
	}

	elapsed := time.Since(start)
	slog.Info("tool run",
		"tool", "webfetch",
		"duration_ms", elapsed.Milliseconds(),
		"bytes", len(body),
	)
	w.logAudit(a.URL, string(args), elapsed, resp.StatusCode, "")

	// Strip null bytes (some pages return binary garbage).
	cleaned := strings.ReplaceAll(string(body), "\x00", "")

	result := cleaned
	truncated := len(body) > maxBytes
	if truncated {
		// Trim to maxBytes after null-byte removal.
		if len(cleaned) > maxBytes {
			result = cleaned[:maxBytes]
		}
		result += fmt.Sprintf("\n(truncated after %d bytes)", maxBytes)
	}

	return string(TruncateOutput([]byte(result))), nil
}

// logAudit sends a tool-run entry to the audit logger if configured.
func (w *WebFetchTool) logAudit(
	url, args string, duration time.Duration,
	statusCode int, errMsg string,
) {
	if w.auditLogger == nil {
		return
	}
	code := statusCode
	w.auditLogger.LogToolRun(
		audit.AuditEntry{
			Tool:       "webfetch",
			Args:       args,
			DurationMs: duration.Milliseconds(),
			ExitCode:   &code,
			Error:      errMsg,
		},
		nil,
	)
}
