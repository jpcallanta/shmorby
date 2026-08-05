package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServerConfig defines a single MCP server subprocess.
type MCPServerConfig struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env,omitempty"`
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

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	setupProcessGroup(cmd)
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}

	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "shmorby",
			Version: "0.0.5",
		},
		nil,
	)

	m.installListChangedMiddleware(ctx, client, name)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
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
		m.registry.Register(adapter)
	}

	m.mu.Lock()
	m.sessions[name] = ms
	m.mu.Unlock()

	return nil
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
		m.registry.Register(adapter)
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
