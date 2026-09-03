package tools

import (
	"context"
	"encoding/json"

	"shmorby/internal/audit"
)

// Tool interface for agent-callable tools.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	PermLevel() string
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// Auditable is implemented by tools that accept an audit logger
// for recording command execution. Tools are wired to the logger
// by looping over the registry, avoiding per-tool type assertions.
type Auditable interface {
	SetAuditLogger(l *audit.Logger)
}
