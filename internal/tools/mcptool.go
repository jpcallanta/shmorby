package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpTool adapts an MCP tool session into the tools.Tool interface.
type mcpTool struct {
	serverName string
	session    *mcp.ClientSession
	tool       *mcp.Tool
	permLevel  string
}

// Returns the tool name prefixed with server name.
func (w *mcpTool) Name() string {
	return w.serverName + "_" + w.tool.Name
}

// Returns the MCP tool description.
func (w *mcpTool) Description() string {
	return w.tool.Description
}

// Returns the MCP inputSchema as JSON.
func (w *mcpTool) Parameters() json.RawMessage {
	schema, err := json.Marshal(w.tool.InputSchema)
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return schema
}

// Returns the configured permission level.
func (w *mcpTool) PermLevel() string { return w.permLevel }

// SetPermLevel updates the permission level at runtime.
func (w *mcpTool) SetPermLevel(level string) { w.permLevel = level }

// Delegates the call to the MCP session's CallTool and renders the
// content items to a text output.
func (w *mcpTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var arguments map[string]any
	if err := json.Unmarshal(args, &arguments); err != nil {
		return "", fmt.Errorf(
			"mcp %s: invalid args: %w", w.Name(), err,
		)
	}

	result, err := w.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      w.tool.Name,
		Arguments: arguments,
	})
	if err != nil {
		return "", fmt.Errorf("mcp %s: %w", w.Name(), err)
	}

	var parts []string
	for _, c := range result.Content {
		parts = append(parts, renderContent(c))
	}

	output := strings.Join(parts, "\n")

	if result.IsError {
		if output != "" {
			output += "\n"
		}
		output += "[MCP tool returned error status]"
	}

	return output, nil
}

// Renders an MCP content item to a string.
func renderContent(c mcp.Content) string {
	switch ct := c.(type) {
	case *mcp.TextContent:
		return ct.Text
	case *mcp.EmbeddedResource:
		if ct.Resource != nil {
			text := ct.Resource.Text
			if text == "" && len(ct.Resource.Blob) > 0 {
				text = fmt.Sprintf("[base64 blob: %s]",
					ct.Resource.MIMEType)
			}

			return text
		}

		return "[empty resource]"
	case *mcp.ImageContent:
		return fmt.Sprintf("[image: %s (%d bytes)]",
			ct.MIMEType, len(ct.Data))
	case *mcp.AudioContent:
		return fmt.Sprintf("[audio: %s (%d bytes)]",
			ct.MIMEType, len(ct.Data))
	default:
		return fmt.Sprintf("[%T content]", c)
	}
}
