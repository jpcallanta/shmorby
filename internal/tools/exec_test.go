package tools

import (
	"context"
	"testing"
	"time"

	"shmorby/internal/audit"
	"shmorby/internal/execution"
)

func TestLogAuditEvent_Correlation(t *testing.T) {
	auditStore, err := audit.NewAuditStore(":memory:")
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer auditStore.Close()
	logger := audit.NewLogger(auditStore, 10, time.Hour, 0)
	defer logger.Close()

	ctx := WithSessionID(context.Background(), "session-1")
	ctx = WithInputID(ctx, "input-1")
	ctx = execution.WithCorrelation(ctx, "input-1", "call-1")
	handle, err := execution.NewManager(nil).Begin(
		ctx,
		execution.New{
			SessionID:  "session-1",
			MessageID:  execution.MessageIDFrom(ctx),
			ToolCallID: execution.ToolCallIDFrom(ctx),
			Tool:       "shell",
		},
	)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	ctx = execution.WithHandle(ctx, handle)

	logAuditEvent(
		logger, ctx, "shell", []byte(`{"command":"echo hi"}`),
		0, nil, nil, nil,
	)
	logger.Close()

	entries, err := auditStore.QueryEntries(
		audit.QueryFilter{ExecutionID: handle.ID()},
	)
	if err != nil {
		t.Fatalf("QueryEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].MessageID != "input-1" ||
		entries[0].ToolCallID != "call-1" {
		t.Errorf("entry = %+v, want transcript and tool-call IDs",
			entries[0])
	}
}

// mockExecutor records the last command run and returns canned output.
type mockExecutor struct {
	Name   string
	Args   []string
	Out    []byte
	RunErr error
}

// Records the call and returns canned results.
func (m *mockExecutor) Run(
	_ context.Context, name string, args ...string,
) ([]byte, error) {
	m.Name = name
	m.Args = append([]string{}, args...)

	return m.Out, m.RunErr
}
