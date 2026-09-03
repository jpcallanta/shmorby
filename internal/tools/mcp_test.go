package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

// TestMCPManager_StartStop verifies the manager starts a server and
// registers its tools, then shuts down cleanly.
func TestMCPManager_StartStop(t *testing.T) {
	reg := NewRegistry()

	// Use config with no servers — verifies graceful empty start.
	mgr := NewMCPManager(
		map[string]MCPServerConfig{}, reg,
	)

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	mgr.Shutdown()
}

// TestMCPTool_Name verifies the tool name is prefixed with server name.
func TestMCPTool_Name(t *testing.T) {
	adapter := &mcpTool{
		serverName: "db",
		tool:       &mcp.Tool{Name: "query"},
	}

	want := "db_query"
	if got := adapter.Name(); got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestMCPTool_Description verifies description delegation.
func TestMCPTool_Description(t *testing.T) {
	adapter := &mcpTool{
		tool: &mcp.Tool{Description: "Run a query"},
	}

	want := "Run a query"
	if got := adapter.Description(); got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestMCPTool_Parameters verifies the schema is returned as-is.
func TestMCPTool_Parameters(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"q": map[string]any{"type": "string"}},
	}
	adapter := &mcpTool{
		tool: &mcp.Tool{InputSchema: schema},
	}

	raw := adapter.Parameters()
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("want type=object, got %v", parsed["type"])
	}
}

// TestMCPTool_PermLevel verifies default permission level.
func TestMCPTool_PermLevel(t *testing.T) {
	adapter := &mcpTool{permLevel: "allow"}

	if got := adapter.PermLevel(); got != "allow" {
		t.Errorf("want allow, got %q", got)
	}
}

// TestMCPTool_SetPermLevel verifies runtime permission update.
func TestMCPTool_SetPermLevel(t *testing.T) {
	adapter := &mcpTool{permLevel: "ask"}
	adapter.SetPermLevel("deny")

	if got := adapter.PermLevel(); got != "deny" {
		t.Errorf("want deny, got %q", got)
	}
}

// TestMCPManager_ServerFailure verifies startup continues when a
// server fails to start.
func TestMCPManager_ServerFailure(t *testing.T) {
	reg := NewRegistry()
	mgr := NewMCPManager(
		map[string]MCPServerConfig{
			"bad": {
				Command: "nonexistent-binary-12345",
				Args:    []string{},
			},
		}, reg,
	)

	// Should not return error — individual server failures are logged.
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("want no error, got %v", err)
	}

	// No tools registered from a failed server.
	schemas := reg.Schemas()
	if len(schemas) != 0 {
		t.Errorf("want 0 tools, got %d", len(schemas))
	}

	mgr.Shutdown()
}

// TestMCPServerConfig_YAML_URLAndHeaders verifies url and headers
// parse from yaml.
func TestMCPServerConfig_YAML_URLAndHeaders(t *testing.T) {
	var cfg MCPServerConfig
	doc := `
url: https://mcp.atlassian.com/v1/mcp
headers:
  Authorization: "Basic Zm9vOmJhcg=="
`
	if err := yaml.Unmarshal([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := "https://mcp.atlassian.com/v1/mcp"
	if cfg.URL != want {
		t.Errorf("want url %q, got %q", want, cfg.URL)
	}

	want = "Basic Zm9vOmJhcg=="
	if got := cfg.Headers["Authorization"]; got != want {
		t.Errorf("want Authorization %q, got %q", want, got)
	}
}

// TestNewTransport_Validation verifies configs missing or duplicating
// the endpoint source are rejected.
func TestNewTransport_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MCPServerConfig
		wantErr string
	}{
		{"neither", MCPServerConfig{}, "command or url"},
		{"both", MCPServerConfig{Command: "npx", URL: "http://x"}, "not both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := newTransport(
				context.Background(), tt.cfg,
			)
			if err == nil {
				t.Fatalf("want error containing %q, got nil",
					tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("want error containing %q, got %q",
					tt.wantErr, err)
			}
		})
	}
}

// TestNewTransport_Command verifies command configs keep using the
// subprocess transport.
func TestNewTransport_Command(t *testing.T) {
	transport, cmd, err := newTransport(
		context.Background(),
		MCPServerConfig{Command: "sh", Args: []string{"-c", "exit 0"}},
	)
	if err != nil {
		t.Fatalf("newTransport: %v", err)
	}

	if _, ok := transport.(*mcp.CommandTransport); !ok {
		t.Errorf("want *mcp.CommandTransport, got %T", transport)
	}

	if cmd == nil {
		t.Error("want cmd, got nil")
	}
}

// TestNewTransport_URL verifies url configs build a streamable HTTP
// transport and a header-injecting client when headers are set.
func TestNewTransport_URL(t *testing.T) {
	transport, cmd, err := newTransport(
		context.Background(),
		MCPServerConfig{URL: "https://mcp.example.com/v1/mcp"},
	)
	if err != nil {
		t.Fatalf("newTransport: %v", err)
	}

	if _, ok := transport.(*mcp.StreamableClientTransport); !ok {
		t.Errorf("want *mcp.StreamableClientTransport, got %T",
			transport)
	}

	if cmd != nil {
		t.Errorf("want nil cmd, got %v", cmd)
	}
}

// TestNewTransport_URL_Headers verifies header configs build a client
// whose transport is scoped to the endpoint host.
func TestNewTransport_URL_Headers(t *testing.T) {
	t.Setenv("SHMORBY_TEST_TOKEN", "Zm9vOmJhcg==")

	transport, _, err := newTransport(
		context.Background(),
		MCPServerConfig{
			URL: "https://mcp.example.com/v1/mcp",
			Headers: map[string]string{
				"Authorization": "Basic ${SHMORBY_TEST_TOKEN}",
			},
		},
	)
	if err != nil {
		t.Fatalf("newTransport: %v", err)
	}

	st, ok := transport.(*mcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("want *mcp.StreamableClientTransport, got %T",
			transport)
	}

	if st.HTTPClient == nil {
		t.Fatal("want HTTPClient, got nil")
	}

	rt, ok := st.HTTPClient.Transport.(headerRoundTripper)
	if !ok {
		t.Fatalf("want headerRoundTripper, got %T",
			st.HTTPClient.Transport)
	}

	if rt.host != "mcp.example.com" {
		t.Errorf("want host %q, got %q", "mcp.example.com", rt.host)
	}
}

// TestNewTransport_URL_HeadersUnsetEnv verifies header configs fail
// when a referenced env var is not set.
func TestNewTransport_URL_HeadersUnsetEnv(t *testing.T) {
	_, _, err := newTransport(
		context.Background(),
		MCPServerConfig{
			URL: "https://mcp.example.com/v1/mcp",
			Headers: map[string]string{
				"Authorization": "Basic ${SHMORBY_UNSET_TEST_TOKEN}",
			},
		},
	)
	if err == nil {
		t.Fatal("want error, got nil")
	}

	if !strings.Contains(
		err.Error(), "SHMORBY_UNSET_TEST_TOKEN",
	) {
		t.Errorf("want error naming unset var, got %q", err)
	}
}

// TestExpandHeaders_EnvSubstitution verifies ${VAR} values expand
// from the environment.
func TestExpandHeaders_EnvSubstitution(t *testing.T) {
	t.Setenv("SHMORBY_TEST_TOKEN", "Zm9vOmJhcg==")

	headers, err := expandHeaders(map[string]string{
		"Authorization": "Basic ${SHMORBY_TEST_TOKEN}",
	})
	if err != nil {
		t.Fatalf("expandHeaders: %v", err)
	}

	want := "Basic Zm9vOmJhcg=="
	if got := headers["Authorization"]; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestExpandHeaders_UnsetEnv verifies unset ${VAR} values are
// rejected instead of silently expanding to empty.
func TestExpandHeaders_UnsetEnv(t *testing.T) {
	_, err := expandHeaders(map[string]string{
		"Authorization": "Basic ${SHMORBY_UNSET_TEST_TOKEN}",
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}

	if !strings.Contains(
		err.Error(), "SHMORBY_UNSET_TEST_TOKEN",
	) {
		t.Errorf("want error naming unset var, got %q", err)
	}
}

// TestHeaderRoundTripper verifies configured headers are attached to
// requests for the endpoint host and dropped for other hosts.
func TestHeaderRoundTripper(t *testing.T) {
	base := &recordingRoundTripper{headers: make(map[string]string)}
	rt := headerRoundTripper{
		base: base,
		headers: map[string]string{
			"Authorization": "Basic Zm9vOmJhcg==",
		},
		host: "mcp.example.com",
	}

	// Same-host request keeps the header.
	req, err := http.NewRequest(
		"GET", "https://mcp.example.com/v1/mcp", nil,
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	want := "Basic Zm9vOmJhcg=="
	if got := base.lastHeader("Authorization"); got != want {
		t.Errorf("want header %q, got %q", want, got)
	}

	// Cross-host request drops the header.
	req, err = http.NewRequest(
		"GET", "https://other.example.com/v1/mcp", nil,
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err = rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()

	if got := base.lastHeader("Authorization"); got != "" {
		t.Errorf("want no header on cross-host, got %q", got)
	}
}

// recordingRoundTripper captures the last request headers for tests.
type recordingRoundTripper struct {
	mu      sync.Mutex
	headers map[string]string
}

// RoundTrip records the request headers and returns a 204 response.
func (r *recordingRoundTripper) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	r.mu.Lock()
	r.headers = make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		if len(v) > 0 {
			r.headers[k] = v[0]
		}
	}
	r.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}, nil
}

// lastHeader returns the recorded value for a header key.
func (r *recordingRoundTripper) lastHeader(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.headers[key]
}

// TestMCPManager_StartHTTP verifies a remote streamable HTTP server
// connects, registers tools, and shuts down cleanly.
func TestMCPManager_StartHTTP(t *testing.T) {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil,
	)
	srv.AddTool(&mcp.Tool{
		Name:        "ping",
		Description: "Pings the server",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(
		ctx context.Context, req *mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "pong"}},
		}, nil
	})

	httpSrv := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	))
	defer httpSrv.Close()

	reg := NewRegistry()
	mgr := NewMCPManager(map[string]MCPServerConfig{
		"remote": {URL: httpSrv.URL},
	}, reg)
	defer mgr.Shutdown()

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	tool, ok := reg.Lookup("remote_ping")
	if !ok {
		t.Fatal("want registered tool remote_ping")
	}

	out, err := tool.Run(
		context.Background(), json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if out != "pong" {
		t.Errorf("want %q, got %q", "pong", out)
	}
}

// TestMCPManager_ToolNameCollision_NoPanic verifies that a malicious
// or buggy MCP server returning a tool whose computed name collides
// with an existing tool does not crash the process. The colliding
// tool is skipped with a warning and the original remains registered.
func TestMCPManager_ToolNameCollision_NoPanic(t *testing.T) {
	// Register a built-in tool that would collide with an MCP tool
	// named "file_read" from a server named "file" (producing
	// "file_file_read"). We use a simpler scenario: register a
	// tool named "myserver_read", then start a server that would
	// also produce "myserver_read".
	reg := NewRegistry()
	builtin := &namedTool{name: "myserver_read"}
	if err := reg.Register(builtin); err != nil {
		t.Fatalf("Register builtin: %v", err)
	}

	// Set up an MCP server whose only tool name, when prefixed,
	// collides with the existing "myserver_read".
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil,
	)
	srv.AddTool(&mcp.Tool{
		Name:        "read",
		Description: "Reads data",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(
		ctx context.Context, req *mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "data"},
			},
		}, nil
	})

	httpSrv := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	))
	defer httpSrv.Close()

	mgr := NewMCPManager(map[string]MCPServerConfig{
		"myserver": {URL: httpSrv.URL},
	}, reg)
	defer mgr.Shutdown()

	// Must not panic. The colliding tool is skipped.
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Original builtin tool must still be registered.
	tool, ok := reg.Lookup("myserver_read")
	if !ok {
		t.Fatal("want original tool still registered")
	}

	// And it must still work.
	if tool != builtin {
		t.Error("want original tool instance preserved")
	}
}
