// Package execution tracks durable tool executions, their lifecycle,
// and execution-scoped cancellation.
package execution

import (
	"context"
	"errors"
	"time"
)

type correlationKey uint8

const (
	messageIDKey correlationKey = iota
	toolCallIDKey
)

// WithCorrelation stores the transcript input and tool-call IDs.
func WithCorrelation(
	ctx context.Context, messageID, toolCallID string,
) context.Context {
	ctx = context.WithValue(ctx, messageIDKey, messageID)

	return context.WithValue(ctx, toolCallIDKey, toolCallID)
}

// MessageIDFrom returns the transcript input ID carried by ctx.
func MessageIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(messageIDKey).(string)

	return id
}

// ToolCallIDFrom returns the tool-call ID carried by ctx.
func ToolCallIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(toolCallIDKey).(string)

	return id
}

// Status is the lifecycle state of an execution.
type Status string

const (
	// StatusPendingApproval is set before permission evaluation.
	StatusPendingApproval Status = "pending_approval"
	// StatusDenied is set when permission rejects the execution.
	StatusDenied Status = "denied"
	// StatusRunning is set immediately before the tool starts.
	StatusRunning Status = "running"
	// StatusSucceeded is set when the tool completes without error.
	StatusSucceeded Status = "succeeded"
	// StatusFailed is set when the tool returns an error.
	StatusFailed Status = "failed"
	// StatusCancelRequested is set when cancellation is requested but
	// termination is not yet confirmed.
	StatusCancelRequested Status = "cancel_requested"
	// StatusCancelled is set after cancellation is confirmed.
	StatusCancelled Status = "cancelled"
	// StatusKillFailed is set when cancellation could not terminate
	// the process group.
	StatusKillFailed Status = "kill_failed"
	// StatusTimedOut is set when the tool hit its deadline.
	StatusTimedOut Status = "timed_out"
	// StatusOrphaned is set when a previous process left the execution
	// running and its process group is still alive.
	StatusOrphaned Status = "orphaned"
	// StatusUnknown is set when a previous process left the execution
	// running but no live process group was found.
	StatusUnknown Status = "unknown"
)

// ParseStatus returns the status for name and reports whether it is
// a known lifecycle value.
func ParseStatus(name string) (Status, bool) {
	status := Status(name)
	switch status {
	case StatusPendingApproval, StatusDenied, StatusRunning,
		StatusSucceeded, StatusFailed, StatusCancelRequested,
		StatusCancelled, StatusKillFailed, StatusTimedOut,
		StatusOrphaned, StatusUnknown:
		return status, true
	}

	return "", false
}

// ErrNotFound is returned when an execution does not exist.
var ErrNotFound = errors.New("execution not found")

// Record is one durable tool execution.
type Record struct {
	ID           string
	SessionID    string
	MessageID    string
	ToolCallID   string
	Tool         string
	Command      string
	ArgsRedacted string
	ArgsHash     string
	Status       Status
	PID          int
	OwnerPID     int
	OwnerStart   string
	ProcessStart string
	CreatedAt    time.Time
	StartedAt    time.Time
	EndedAt      time.Time
	ExitCode     *int
	Error        string
	OutputRef    string
	ApprovalID   string
}

// New describes an execution before it is persisted.
type New struct {
	SessionID    string
	MessageID    string
	ToolCallID   string
	Tool         string
	Command      string
	ArgsRedacted string
	ArgsHash     string
	ApprovalID   string
}

// Output bounds for persisted execution output.
const (
	OutputChunkBytes = 32 << 10
	MaxOutputBytes   = 1 << 20
)

// OutputChunk is one page of persisted execution output.
type OutputChunk struct {
	ExecutionID string
	Cursor      string
	Data        string
	Next        string
	Done        bool
}

// Filter restricts ListExecutions results.
type Filter struct {
	SessionID string
	Statuses  []Status
	Limit     int
}

// Recorder persists execution records. session.Store satisfies it.
type Recorder interface {
	// CreateExecution inserts a new record.
	CreateExecution(r Record) error

	// UpdateExecution updates an existing record by id.
	UpdateExecution(r Record) error

	// GetExecution returns the record with id.
	GetExecution(id string) (Record, error)

	// ListExecutions returns records matching f, newest first.
	ListExecutions(f Filter) ([]Record, error)

	// AppendExecutionOutput replaces the output chunks for id.
	AppendExecutionOutput(id string, chunks []string) error

	// ReadExecutionOutput returns one output chunk and whether it is
	// the last chunk.
	ReadExecutionOutput(id string, cursor int) (string, bool, error)
}

// Reconcile marks executions left running by a previous process as
// orphaned or unknown. It never marks them succeeded.
func Reconcile(rec Recorder, now time.Time) ([]Record, error) {
	rows, err := rec.ListExecutions(Filter{Statuses: []Status{
		StatusPendingApproval,
		StatusRunning,
		StatusCancelRequested,
		StatusOrphaned,
	}})
	if err != nil {
		return nil, err
	}

	out := make([]Record, 0, len(rows))

	for i := range rows {
		r := rows[i]
		if ownerIsAlive(r) {
			continue
		}

		r.Status = StatusUnknown
		r.EndedAt = now
		r.Error = "stale execution recovered; " +
			"no verifiable live process group"

		if r.PID > 0 && ProcessGroupAlive(r.PID) {
			switch {
			case r.ProcessStart == "":
				r.Error = "stale execution recovered; " +
					"live process group cannot be verified"
			case ProcessIdentity(r.PID) != r.ProcessStart:
				r.Error = "stale execution recovered; " +
					"pid now belongs to a different process"
			default:
				r.Status = StatusOrphaned
				r.Error = "stale execution recovered; " +
					"process group is still alive"
			}
		}

		if uErr := rec.UpdateExecution(r); uErr != nil {
			return nil, uErr
		}
		out = append(out, r)
	}

	return out, nil
}

// ownerIsAlive reports whether the shmorby process that created an
// execution still exists with the same identity. Reconcile must not
// touch another live instance's rows.
func ownerIsAlive(r Record) bool {
	if r.OwnerPID <= 0 || r.OwnerStart == "" {
		return false
	}

	return ProcessIdentity(r.OwnerPID) == r.OwnerStart
}
