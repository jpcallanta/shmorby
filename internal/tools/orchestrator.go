package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"shmorby/internal/audit"
)

// maxSubagentDepth limits recursive subagent spawning to prevent
// arbitrary-depth permission bypass chains.
const maxSubagentDepth = 3

// TaskOrchestrator manages subagent dispatch and result collection.
type TaskOrchestrator struct {
	// RunSubtask runs a single subtask. Must be set by the caller.
	RunSubtask RunSubtaskFunc

	// MaxParallel limits concurrent subagents (default 5).
	MaxParallel int

	// AuditLogger is the audit logger for subagent events.
	AuditLogger *audit.Logger

	// ParentSessionID is the parent session ID for subagent linkage.
	ParentSessionID string

	// PermFunc is the parent session's permission callback. Stored as
	// any to avoid an import cycle (tools → agent → tools). The
	// concrete type is agent.ToolPermissionFunc. Passed to subagents
	// so they respect the same permission constraints.
	PermFunc any

	// Depth is the current subagent nesting depth (0 = top-level).
	Depth int

	// SubtaskTimeout is the per-subtask deadline (0 = no timeout).
	// Applied in runSubtask via context.WithTimeout so individual
	// slow subagents cannot block the entire dispatch. Also used as
	// the fallback for TotalTimeout when that field is unset.
	SubtaskTimeout time.Duration

	// TotalTimeout is the maximum wall-clock time for the entire
	// DispatchTasks call including all subtasks (0 = no timeout).
	// This is a hard upper bound on how long the parent task waits
	// for subtask completion via wg.Wait(). Takes precedence over
	// SubtaskTimeout when both are set.
	TotalTimeout time.Duration
}

// defaultMaxParallel is the default concurrency limit.
const defaultMaxParallel = 5

// DispatchTasks runs subtasks and returns structured results.
func (o *TaskOrchestrator) DispatchTasks(
	ctx context.Context,
	tasks []Subtask,
	parallel bool,
) ([]TaskResult, error) {
	// Fail fast when context is already cancelled — no point
	// launching subtasks that would immediately error.
	if ctx.Err() != nil {
		results := make([]TaskResult, len(tasks))
		for i, t := range tasks {
			results[i] = TaskResult{
				TaskID:      t.ID,
				Description: t.Description,
				Status:      "error",
				Error:       ctx.Err().Error(),
			}
		}
		return results, nil
	}

	if len(tasks) == 0 {
		return []TaskResult{}, nil
	}

	results := make([]TaskResult, len(tasks))

	type item struct {
		idx  int
		task Subtask
	}

	ch := make(chan item, len(tasks))
	for i, t := range tasks {
		ch <- item{idx: i, task: t}
	}
	close(ch)

	var wg sync.WaitGroup
	maxPar := o.MaxParallel
	if maxPar < 1 {
		maxPar = defaultMaxParallel
	}
	if !parallel {
		maxPar = 1
	}
	throttle := make(chan struct{}, maxPar)

	for it := range ch {
		wg.Add(1)
		throttle <- struct{}{}
		go func(it item) {
			defer wg.Done()
			defer func() { <-throttle }()

			start := time.Now()
			result := o.runSubtask(ctx, it.task)
			duration := time.Since(start)

			if o.AuditLogger != nil && o.ParentSessionID != "" {
				status := "completed"
				if result.Status == "error" {
					status = "failed"
				}
				o.AuditLogger.LogSubagent(audit.SubagentAudit{
					ParentSessionID: o.ParentSessionID,
					ChildSessionID:  result.TaskID,
					Tool:            "task",
					Status:          status,
				})
				durMs := duration.Milliseconds()
				o.AuditLogger.CompleteSubagent(
					result.TaskID, durMs, status,
				)
			}

			results[it.idx] = result
		}(it)
	}

	// Wait for all subtasks to complete with a deadline.
	// Without this, a single slow or blocked subtask causes the
	// parent to hang indefinitely.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Determine the effective timeout: TotalTimeout takes
	// precedence; SubtaskTimeout is a fallback.
	timeout := o.TotalTimeout
	if timeout <= 0 {
		timeout = o.SubtaskTimeout
	}

	if timeout > 0 {
		select {
		case <-done:
			return results, nil
		case <-time.After(timeout):
			return results, fmt.Errorf(
				"orchestrator: timeout exceeded after %v",
				timeout,
			)
		case <-ctx.Done():
			// Context cancellation (e.g. SIGINT) — each subtask
			// captures the error in its result individually; no
			// need to double-report via the return error.
			return results, nil
		}
	}

	// No orchestrator-imposed timeout — wait for all subtasks to
	// finish. Context cancellation is handled inside each subtask
	// via RunSubtask, which returns an error result on ctx.Done().
	<-done
	return results, nil
}

// runSubtask delegates to the configured RunSubtask function.
// When SubtaskTimeout is set, the context is wrapped with a
// per-subtask deadline so individual slow subagents cannot block
// the entire dispatch indefinitely.
func (o *TaskOrchestrator) runSubtask(
	ctx context.Context,
	task Subtask,
) TaskResult {
	if o.RunSubtask == nil {
		return TaskResult{
			TaskID:      task.ID,
			Description: task.Description,
			Status:      "error",
			Error:       "orchestrator: RunSubtask not configured",
		}
	}
	if o.Depth >= maxSubagentDepth {
		return TaskResult{
			TaskID:      task.ID,
			Description: task.Description,
			Status:      "error",
			Error:       fmt.Sprintf("subagent depth limit exceeded (max %d)", maxSubagentDepth),
		}
	}
	// Apply per-subtask timeout if configured. This caps how long
	// a single subtask can run before context cancellation.
	if o.SubtaskTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.SubtaskTimeout)
		defer cancel()
	}
	return o.RunSubtask(ctx, task)
}
