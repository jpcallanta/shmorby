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
	"shmorby/internal/health"
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
	client         *http.Client
	defaultTimeout int
	auditLogger    *audit.Logger
}

// Creates a WebFetchTool with the given permission
// level and HTTP client. Pass nil client to use a default http.Client
// with SSRF-safe redirect validation (no timeout — the context
// deadline in Run controls the timeout).
func NewWebFetchTool(permLevel string, client *http.Client) *WebFetchTool {
	if client == nil {
		// Use a custom DialContext that resolves DNS and validates
		// IPs at connection time, closing the TOCTOU gap between
		// DNS resolution (before request) and TCP connect (during
		// request). Without this, an attacker with TTL=0 DNS can
		// rebind to a private IP between validation and connection.
		transport := &http.Transport{
			DialContext: safeDialContext,
		}
		client = &http.Client{
			Transport: transport,
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

// Sets the default timeout in seconds for this tool.
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

// Returns the configured permission level.
func (w *WebFetchTool) PermLevel() string { return w.perm }

// Updates the permission level at runtime.
func (w *WebFetchTool) SetPerm(level string) { w.perm = level }

// Sets the audit logger for this tool.
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

// safeDialContext resolves DNS and validates IPs at connection time,
// closing the TOCTOU gap between DNS resolution and TCP connect.
// Without this, an attacker controlling a DNS name can respond with
// a public IP during validation, then rebind to 127.0.0.1,
// 169.254.169.254, or 10.x.x.x before the connection is established.
//
// NOTE: This deliberately performs a second DNS resolution at connect
// time (the first is in validateURLNotPrivate before the request).
// The redundant lookup is intentional — it closes the TOCTOU gap by
// ensuring the IP we connect to is the same one that was validated.
var safeDialContext = func(
	ctx context.Context, network, addr string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("webfetch: split addr: %w", err)
	}

	// Resolve DNS at connection time, not before the request.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("webfetch: resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, fmt.Errorf(
				"webfetch: resolved %s to private IP %s, blocked",
				host, ip.IP,
			)
		}
	}

	// Connect to a non-private IP. Iterate all resolved IPs so that
	// a transient connection failure on one address falls back to the
	// next, matching the standard resolver's multi-address behavior.
	var d net.Dialer
	var firstErr error
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			continue
		}
		// Rebuild addr with the resolved IP to avoid a second
		// DNS lookup by the default dialer. net.JoinHostPort
		// handles IPv6 bracket wrapping automatically.
		resolved := net.JoinHostPort(ip.IP.String(), port)
		conn, dialErr := d.DialContext(ctx, network, resolved)
		if dialErr == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = dialErr
		}
	}

	// All IPs failed or were private — return the first error.
	if firstErr != nil {
		return nil, fmt.Errorf(
			"webfetch: dial %s: %w", host, firstErr,
		)
	}
	return nil, fmt.Errorf(
		"webfetch: no public IPs for %s", host,
	)
}

// Checks that a URL does not target a private,
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

// Fetches a URL and returns the response body as plain text.
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
			return "", health.Wrap("webfetch", elapsed,
				fmt.Errorf(
					"webfetch: request cancelled or timed out: %w",
					cmdCtx.Err(),
				),
			)
		}
		return "", health.Wrap("webfetch", elapsed,
			fmt.Errorf("webfetch: request failed: %w", err),
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		elapsed := time.Since(start)
		errMsg := fmt.Sprintf("GET %s returned %d", a.URL, resp.StatusCode)
		w.logAudit(a.URL, string(args), elapsed, resp.StatusCode, errMsg)
		return "", health.Wrap("webfetch", elapsed,
			fmt.Errorf("webfetch: %s", errMsg),
		)
	}

	// Read maxBytes+1 so we can detect truncation accurately.
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		elapsed := time.Since(start)
		w.logAudit(a.URL, string(args), elapsed, resp.StatusCode, err.Error())
		return "", health.Wrap("webfetch", elapsed,
			fmt.Errorf("webfetch: read body: %w", err),
		)
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

// Sends a tool-run entry to the audit logger if configured.
func (w *WebFetchTool) logAudit(
	url, args string, duration time.Duration,
	statusCode int, errMsg string,
) {
	if w.auditLogger == nil {
		return
	}
	code := statusCode
	// Redact args before audit logging to prevent secrets embedded
	// in URLs (API keys, tokens) from persisting in plaintext in
	// the SQLite audit database.
	w.auditLogger.LogToolRun(
		audit.AuditEntry{
			Tool:       "webfetch",
			Args:       string(RedactArgs([]byte(args))),
			DurationMs: duration.Milliseconds(),
			ExitCode:   &code,
			Error:      errMsg,
		},
		nil,
	)
}
