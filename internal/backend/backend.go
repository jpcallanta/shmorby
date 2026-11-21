// Package backend defines the in-process application boundary and event
// contract shared by frontends.
package backend

import (
	"context"
	"encoding/json"
	"time"

	"shmorby/internal/execution"
	"shmorby/internal/llm"
	"shmorby/internal/session"
)

// Backend is the frontend-facing service boundary.
type Backend interface {
	CreateSession(context.Context, SessionSpec) (Session, error)
	ListSessions(context.Context, SessionFilter) ([]Session, error)
	GetSession(context.Context, string) (Session, error)

	SubmitInput(context.Context, string, Input) (InputID, error)
	Interrupt(context.Context, string) error
	Compact(context.Context, string) error

	ListApprovals(context.Context, ApprovalFilter) ([]Approval, error)
	DecideApproval(context.Context, string, Decision) error

	ListExecutions(context.Context, ExecutionFilter) ([]Execution, error)
	GetExecution(context.Context, string) (Execution, error)
	CancelExecution(context.Context, string) error
	ExecutionOutput(context.Context, string, string) (OutputChunk, error)

	Subscribe(context.Context, EventFilter) (<-chan Event, error)
	DroppedEvents() uint64
}

// SessionSpec describes a new session.
type SessionSpec struct {
	Directory string
	AgentMode string
	Provider  string
	Model     string
	ParentID  string
}

// SessionFilter restricts listed sessions.
type SessionFilter struct {
	Directory       string
	IncludeArchived bool
	Limit           int
}

// Session is a frontend-safe session snapshot.
type Session struct {
	Meta     session.Meta
	Messages []session.Message
}

// InputID is a frontend-generated idempotency key.
type InputID string

// Input describes one user turn.
type Input struct {
	ID                InputID
	Text              string
	Images            []llm.ImageData
	ModelInfo         llm.ModelInfo
	Stream            bool // Forward progressive stream events.
	Mode              string
	Model             string
	Override          string
	PermissionHandler PermissionHandler
}

// PermissionRequest contains the context needed by a frontend approval
// prompt without exposing agent implementation types.
type PermissionRequest struct {
	Context         context.Context
	SessionID       string
	InputID         string
	ExecutionID     string
	Tool            string
	Command         string
	CommandRedacted string
	RulePattern     string
	RuleAction      string
	Reason          string
	Resolution      *PermissionResolution
}

// PermissionResolution carries approval attribution back to the backend.
type PermissionResolution struct {
	ApprovalID     string
	DecidedBy      string
	DecisionSource string
	Scope          string
}

// PermissionDecision is a frontend's response to a permission prompt.
type PermissionDecision int

const (
	PermissionDeny PermissionDecision = iota
	PermissionAllow
	PermissionAllowAll
)

// PermissionHandler handles an interactive permission request.
type PermissionHandler func(PermissionRequest) PermissionDecision

// ApprovalFilter restricts approval results.
type ApprovalFilter struct {
	SessionID string
	Status    string
	Limit     int
}

// Approval is one persisted approval request or decision.
type Approval = session.ApprovalRecord

// Decision is an approval decision.
type Decision string

const (
	DecisionOnce   Decision = "once"
	DecisionAlways Decision = "always"
	DecisionReject Decision = "reject"
)

// ExecutionFilter restricts execution results.
type ExecutionFilter struct {
	SessionID string
	Statuses  []execution.Status
	Limit     int
}

// Execution is one durable tool execution.
type Execution = execution.Record

// OutputChunk is one page of persisted execution output.
type OutputChunk = execution.OutputChunk

// EventType identifies an event in the stable event contract.
type EventType string

const (
	EventSessionCreated    EventType = "session.created"
	EventSessionUpdated    EventType = "session.updated"
	EventSessionDeleted    EventType = "session.deleted"
	EventInputAccepted     EventType = "input.accepted"
	EventInputCancelled    EventType = "input.cancelled"
	EventExecutionStarted  EventType = "execution.started"
	EventExecutionOutput   EventType = "execution.output"
	EventExecutionEnded    EventType = "execution.ended"
	EventApprovalRequested EventType = "approval.requested"
	EventApprovalDecided   EventType = "approval.decided"
	EventApprovalExpired   EventType = "approval.expired"
	EventMessageCreated    EventType = "message.created"
	EventTextDelta         EventType = "session.text.delta"
	EventReasoningDelta    EventType = "session.reasoning.delta"
	EventToolStarted       EventType = "tool.started"
	EventToolProgress      EventType = "tool.progress"
	EventToolEnded         EventType = "tool.ended"
	EventContextCompacted  EventType = "context.compacted"
	EventUsageUpdated      EventType = "usage.updated"
	EventConfigUpdated     EventType = "config.updated"
)

// Event is the version-neutral envelope consumed by frontends and future
// transports. Data is encoded JSON and is always present.
type Event struct {
	ID          string          `json:"id"`
	Type        EventType       `json:"type"`
	Created     time.Time       `json:"created"`
	SessionID   string          `json:"session_id"`
	InputID     string          `json:"input_id"`
	ExecutionID string          `json:"execution_id"`
	ApprovalID  string          `json:"approval_id"`
	Data        json.RawMessage `json:"data"`
}

// EventFilter restricts a subscription.
type EventFilter struct {
	Types       []EventType
	SessionID   string
	InputID     string
	ExecutionID string
	ApprovalID  string
}

// ToolEventData is the common tool lifecycle payload.
type ToolEventData struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	Status  string `json:"status,omitempty"`
	Output  string `json:"output,omitempty"`
}

// MessageEventData describes a created conversation message.
type MessageEventData struct {
	Role          string `json:"role"`
	Content       string `json:"content"`
	MemoryEntries int    `json:"memory_entries,omitempty"`
}

// DeltaEventData carries one text stream chunk.
type DeltaEventData struct {
	Delta string `json:"delta"`
}

// InputEventData identifies a submitted input.
type InputEventData struct {
	Text string `json:"text,omitempty"`
}

// UsageEventData reports token usage from an LLM response.
type UsageEventData struct {
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}
