package tools

import (
	"context"
	"sync"
)

// TaskOrchestrator manages subagent dispatch and result collection.
type TaskOrchestrator struct {
	// RunSubtask runs a single subtask. Must be set by the caller.
	RunSubtask RunSubtaskFunc

	// MaxParallel limits concurrent subagents (default 5).
	MaxParallel int
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

			result := o.runSubtask(ctx, it.task)
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
	return o.RunSubtask(ctx, task)
}
