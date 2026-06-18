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
	Run(ctx context.Context, args json.RawMessage) (string, error)
}
