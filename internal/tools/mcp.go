package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	cmdexec "shmorby/internal/exec"
)

// MCPServerConfig defines a single MCP server. Servers run either as
// a subprocess (command) or as a remote streamable HTTP endpoint (url).
// Set exactly one of command or url.
type MCPServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     []string          `yaml:"env,omitempty"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// MCPManager manages MCP server processes and tool registration.
type MCPManager struct {
	mu       sync.Mutex
	sessions map[string]*mcpSession
	registry *Registry
	cfg      map[string]MCPServerConfig
}

type mcpSession struct {
	cancel  context.CancelFunc
	session *mcp.ClientSession
	cmd     *exec.Cmd
	tools   []*mcp.Tool
}

// Creates a manager with the configured servers and reference to the
// tool registry.
func NewMCPManager(
	cfg map[string]MCPServerConfig,
	reg *Registry,
) *MCPManager {
	return &MCPManager{
		sessions: make(map[string]*mcpSession),
		registry: reg,
		cfg:      cfg,
	}
}

// Starts all configured MCP servers and registers their tools.
func (m *MCPManager) Start(ctx context.Context) error {
	for name, cfg := range m.cfg {
		if err := m.startServer(ctx, name, cfg); err != nil {
			slog.Warn("MCP server failed to start",
				"server", name, "err", err)
			continue
		}
	}

	return nil
}

// Starts a single MCP server, performs the handshake, discovers tools,
// and registers them with the registry.
func (m *MCPManager) startServer(
	ctx context.Context,
	name string,
	cfg MCPServerConfig,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	transport, cmd, err := newTransport(ctx, cfg)
	if err != nil {
		return fmt.Errorf("mcp %q: %w", name, err)
	}

	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "shmorby",
			Version: "0.0.5",
		},
		nil,
	)

	m.installListChangedMiddleware(ctx, client, name)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("mcp connect: %w", err)
	}

	initResult := session.InitializeResult()
	slog.Info("MCP server initialized",
		"server", name,
		"serverInfo", initResult.ServerInfo,
		"capabilities", initResult.Capabilities,
	)

	if initResult.Capabilities == nil ||
		initResult.Capabilities.Tools == nil {
		session.Close()

		return fmt.Errorf(
			"MCP server %q does not support tools", name,
		)
	}

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		session.Close()

		return fmt.Errorf("mcp list tools: %w", err)
	}

	ms := &mcpSession{
		cancel:  cancel,
		session: session,
		cmd:     cmd,
		tools:   toolsResult.Tools,
	}

	for _, t := range toolsResult.Tools {
		adapter := &mcpTool{
			serverName: name,
			session:    session,
			tool:       t,
			permLevel:  "ask",
		}
		// Skip tools whose computed name collides with an
		// already-registered tool (built-in or from another
		// MCP server).  This prevents a malicious or buggy
		// MCP server from crashing the process via panic.
		if err := m.registry.Register(adapter); err != nil {
			slog.Warn("skipping duplicate MCP tool",
				"server", name, "tool", t.Name, "err", err)
			continue
		}
	}

	m.mu.Lock()
	m.sessions[name] = ms
	m.mu.Unlock()

	return nil
}

// Builds the transport for a server config: a subprocess transport
// when command is set, otherwise a streamable HTTP transport for the
// url endpoint. Rejects configs that set neither or both.
func newTransport(
	ctx context.Context,
	cfg MCPServerConfig,
) (mcp.Transport, *exec.Cmd, error) {
	if cfg.Command != "" && cfg.URL != "" {
		return nil, nil, errors.New(
			"set either command or url, not both",
		)
	}

	if cfg.Command != "" {
		cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
		cmdexec.SetupProcessGroup(cmd)
		if len(cfg.Env) > 0 {
			cmd.Env = append(os.Environ(), cfg.Env...)
		}

		return &mcp.CommandTransport{Command: cmd}, cmd, nil
	}

	if cfg.URL == "" {
		return nil, nil, errors.New("set either command or url")
	}

	transport := &mcp.StreamableClientTransport{Endpoint: cfg.URL}
	if len(cfg.Headers) > 0 {
		u, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid url: %w", err)
		}

		headers, err := expandHeaders(cfg.Headers)
		if err != nil {
			return nil, nil, err
		}
		transport.HTTPClient = &http.Client{
			Transport: headerRoundTripper{
				base:    http.DefaultTransport,
				headers: headers,
				host:    u.Host,
			},
		}
	}

	return transport, nil, nil
}

// Injects static headers into requests for the endpoint host. The
// host check mirrors Go's own cross-domain sensitive-header stripping
// on redirects, which transport-level injection would otherwise
// defeat and leak credentials to other hosts.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
	host    string // endpoint host; headers are dropped on other hosts
}

// Applies the configured headers to same-host requests before
// delegating to the wrapped transport.
func (rt headerRoundTripper) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	if strings.EqualFold(req.URL.Host, rt.host) {
		for k, v := range rt.headers {
			req.Header.Set(k, v)
		}
	}

	return rt.base.RoundTrip(req)
}

// Returns the headers with ${VAR} env-var substitution applied to
// values, so secrets can stay out of the config file. Fails when a
// referenced variable is not set.
func expandHeaders(headers map[string]string) (map[string]string, error) {
	expanded := make(map[string]string, len(headers))
	for k, v := range headers {
		var missing []string
		val := os.Expand(v, func(key string) string {
			if got, ok := os.LookupEnv(key); ok {
				return got
			}
			missing = append(missing, key)

			return ""
		})
		if len(missing) > 0 {
			return nil, fmt.Errorf(
				"header %q: unset env vars: %s",
				k, strings.Join(missing, ", "),
			)
		}

		expanded[k] = val
	}

	return expanded, nil
}

// SetDefaultPermLevel applies the given permission level to all
// registered MCP tools.
func (m *MCPManager) SetDefaultPermLevel(level string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, s := range m.sessions {
		for _, t := range s.tools {
			key := name + "_" + t.Name
			if tool, ok := m.registry.Lookup(key); ok {
				if mcpT, ok := tool.(*mcpTool); ok {
					mcpT.SetPermLevel(level)
				}
			}
		}
	}
}

// Shutdown gracefully shuts down all MCP sessions.
func (m *MCPManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, s := range m.sessions {
		m.unregisterTools(name, s.tools)
		s.cancel()
		s.session.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			s.cmd.Process.Kill()
		}

		delete(m.sessions, name)
	}
}

// Registers a middleware on the client that detects
// notifications/tools/list_changed and triggers a re-list.
func (m *MCPManager) installListChangedMiddleware(
	ctx context.Context,
	client *mcp.Client,
	name string,
) {
	listChanged := make(chan struct{}, 1)

	client.AddReceivingMiddleware(
		func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(
				ctx context.Context,
				method string,
				req mcp.Request,
			) (mcp.Result, error) {
				result, err := next(ctx, method, req)
				// Check for ToolListChanged notification.
				if _, ok := req.(*mcp.ClientRequest[*mcp.ToolListChangedParams]); ok {
					select {
					case listChanged <- struct{}{}:
					default:
					}
				}

				return result, err
			}
		},
	)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-listChanged:
				m.handleListChanged(name)
			}
		}
	}()
}

// Re-lists tools from a server and updates the registry.
func (m *MCPManager) handleListChanged(name string) {
	m.mu.Lock()
	ms, ok := m.sessions[name]
	m.mu.Unlock()

	if !ok {
		return
	}

	slog.Info("MCP tools changed, re-listing", "server", name)

	newTools, err := ms.session.ListTools(context.Background(), nil)
	if err != nil {
		slog.Warn("re-list tools failed",
			"server", name, "err", err)

		return
	}

	m.mu.Lock()
	m.unregisterTools(name, ms.tools)
	for _, t := range newTools.Tools {
		adapter := &mcpTool{
			serverName: name,
			session:    ms.session,
			tool:       t,
			permLevel:  "ask",
		}
		// On re-list, skip tools whose name collides with a
		// tool registered after the original list (e.g. a
		// built-in or tool from another server).  Logs and
		// continues instead of crashing the process.
		if err := m.registry.Register(adapter); err != nil {
			slog.Warn("skipping duplicate MCP tool on re-list",
				"server", name, "tool", t.Name, "err", err)
			continue
		}
	}
	ms.tools = newTools.Tools
	m.mu.Unlock()
}

// Unregisters all tools for a given server.
func (m *MCPManager) unregisterTools(
	serverName string,
	tools []*mcp.Tool,
) {
	for _, t := range tools {
		m.registry.Unregister(serverName + "_" + t.Name)
	}
}
