package tools

import (
	"context"
	"encoding/json"
)

// Tool interface for agent-callable tools.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	PermLevel() string
	Run(ctx context.Context, args json.RawMessage) (string, error)
}
