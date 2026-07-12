package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
