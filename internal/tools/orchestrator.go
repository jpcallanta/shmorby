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
}

// defaultMaxParallel is the default concurrency limit.
const defaultMaxParallel = 5

// DispatchTasks runs subtasks and returns structured results.
func (o *TaskOrchestrator) DispatchTasks(
	ctx context.Context,
	tasks []Subtask,
	parallel bool,
) ([]TaskResult, error) {
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

	wg.Wait()
	return results, nil
}

// runSubtask delegates to the configured RunSubtask function.
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
	return o.RunSubtask(ctx, task)
}
