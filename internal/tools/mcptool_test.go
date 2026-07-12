package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Creates an in-memory MCP server with test tools and returns a
// connected client session.
func setupTestServer(
	t *testing.T,
	tools []*mcp.Tool,
) (*mcp.ClientSession, func()) {
	t.Helper()

	server := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "1.0"},
		nil,
	)

	for _, tool := range tools {
		t := tool
		mcp.AddTool(server, t, func(
			ctx context.Context,
			req *mcp.CallToolRequest,
			_ any,
		) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "ok"},
				},
			}, nil, nil
		})
	}

	clientTrans, serverTrans := mcp.NewInMemoryTransports()
	go server.Run(context.Background(), serverTrans) //nolint:errcheck

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "1.0"},
		nil,
	)

	session, err := client.Connect(
		context.Background(), clientTrans, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	return session, func() {
		session.Close()
	}
}

// TestMCPTool_Run_Success verifies the mcpTool adapter delegates
// correctly to the MCP session.
func TestMCPTool_Run_Success(t *testing.T) {
	toolDef := &mcp.Tool{
		Name:        "greet",
		Description: "Say hello",
		InputSchema: map[string]any{"type": "object"},
	}

	session, cleanup := setupTestServer(t, []*mcp.Tool{toolDef})
	defer cleanup()

	adapter := &mcpTool{
		serverName: "test",
		session:    session,
		tool:       toolDef,
		permLevel:  "ask",
	}

	out, err := adapter.Run(
		context.Background(), json.RawMessage(`{}`),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if out != "ok" {
		t.Errorf("want %q, got %q", "ok", out)
	}
}

// TestMCPTool_Run_InvalidArgs verifies error on bad args.
func TestMCPTool_Run_InvalidArgs(t *testing.T) {
	toolDef := &mcp.Tool{
		Name:        "greet",
		Description: "Say hello",
		InputSchema: map[string]any{"type": "object"},
	}

	session, cleanup := setupTestServer(t, []*mcp.Tool{toolDef})
	defer cleanup()

	adapter := &mcpTool{
		serverName: "test",
		session:    session,
		tool:       toolDef,
		permLevel:  "ask",
	}

	_, err := adapter.Run(
		context.Background(), json.RawMessage(`not json`),
	)
	if err == nil {
		t.Fatal("want error for invalid args, got nil")
	}
}
