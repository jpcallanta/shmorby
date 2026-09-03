package tools

import (
	"context"
	"sync"
	"testing"
	"time"
)

// slowSubtask returns a RunSubtaskFunc that simulates work by sleeping.
func slowSubtask(delay time.Duration) RunSubtaskFunc {
	return func(ctx context.Context, task Subtask) TaskResult {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return TaskResult{
				TaskID: task.ID, Status: "error",
				Error: ctx.Err().Error(),
			}
		}
		return TaskResult{
			TaskID: task.ID, Description: task.Description,
			Status: "ok", Output: "done",
		}
	}
}

func TestOrchestrator_ParallelDispatch(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: slowSubtask(50 * time.Millisecond),
	}
	tasks := []Subtask{
		{ID: "a", Description: "task a", Prompt: "do a"},
		{ID: "b", Description: "task b", Prompt: "do b"},
		{ID: "c", Description: "task c", Prompt: "do c"},
	}
	start := time.Now()
	results, err := orch.DispatchTasks(context.Background(), tasks, true)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("task %s: want ok, got %s", r.TaskID, r.Status)
		}
	}
	if elapsed >= 150*time.Millisecond {
		t.Errorf("parallel dispatch too slow (%v), tasks not concurrent", elapsed)
	}
}

func TestOrchestrator_SequentialDispatch(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: slowSubtask(30 * time.Millisecond),
	}
	tasks := []Subtask{
		{ID: "a", Prompt: "do a"},
		{ID: "b", Prompt: "do b"},
	}
	start := time.Now()
	results, err := orch.DispatchTasks(context.Background(), tasks, false)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("sequential dispatch too fast (%v), tasks concurrent", elapsed)
	}
	if results[0].TaskID != "a" || results[1].TaskID != "b" {
		t.Error("sequential dispatch should preserve order")
	}
}

func TestOrchestrator_Cancellation(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: slowSubtask(1 * time.Second),
	}
	tasks := []Subtask{
		{ID: "a", Prompt: "do a"},
		{ID: "b", Prompt: "do b"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := orch.DispatchTasks(ctx, tasks, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "error" {
			t.Errorf("task %s: want error status for cancelled ctx, got %s",
				r.TaskID, r.Status)
		}
	}
}

func TestOrchestrator_PartialFailure(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			if task.ID == "b" {
				return TaskResult{
					TaskID: task.ID, Description: task.Description,
					Status: "error", Error: "something went wrong",
				}
			}
			return TaskResult{
				TaskID: task.ID, Description: task.Description,
				Status: "ok", Output: "success",
			}
		},
	}
	tasks := []Subtask{
		{ID: "a", Description: "ok task", Prompt: "do a"},
		{ID: "b", Description: "fail task", Prompt: "do b"},
		{ID: "c", Description: "ok task", Prompt: "do c"},
	}
	results, err := orch.DispatchTasks(context.Background(), tasks, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	statuses := make(map[string]string)
	for _, r := range results {
		statuses[r.TaskID] = r.Status
	}
	if statuses["a"] != "ok" {
		t.Errorf("task a: want ok, got %s", statuses["a"])
	}
	if statuses["b"] != "error" {
		t.Errorf("task b: want error, got %s", statuses["b"])
	}
	if statuses["c"] != "ok" {
		t.Errorf("task c: want ok, got %s", statuses["c"])
	}
}

func TestOrchestrator_MaxParallel(t *testing.T) {
	var mu sync.Mutex
	var concurrent int
	var maxConcurrent int
	orch := &TaskOrchestrator{
		MaxParallel: 2,
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			mu.Lock()
			concurrent++
			if concurrent > maxConcurrent {
				maxConcurrent = concurrent
			}
			mu.Unlock()
			defer func() {
				mu.Lock()
				concurrent--
				mu.Unlock()
			}()
			time.Sleep(50 * time.Millisecond)
			return TaskResult{
				TaskID: task.ID, Status: "ok", Output: "done",
			}
		},
	}
	tasks := make([]Subtask, 6)
	for i := range tasks {
		tasks[i] = Subtask{ID: string(rune('a' + i)), Prompt: "do"}
	}
	_, err := orch.DispatchTasks(context.Background(), tasks, true)
	if err != nil {
		t.Fatal(err)
	}
	if maxConcurrent > 2 {
		t.Errorf("max parallel exceeded: want ≤2, got %d", maxConcurrent)
	}
}

func TestOrchestrator_EmptyTasks(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			return TaskResult{TaskID: task.ID, Status: "ok"}
		},
	}
	results, err := orch.DispatchTasks(context.Background(), []Subtask{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Error("empty tasks should return empty results")
	}
}

func TestOrchestrator_ContextTimeout(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			select {
			case <-time.After(1 * time.Second):
				return TaskResult{TaskID: task.ID, Status: "ok"}
			case <-ctx.Done():
				return TaskResult{
					TaskID: task.ID, Status: "error",
					Error: ctx.Err().Error(),
				}
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	tasks := []Subtask{
		{ID: "a", Prompt: "do a"},
		{ID: "b", Prompt: "do b"},
	}
	results, err := orch.DispatchTasks(ctx, tasks, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "error" {
			t.Errorf("task %s: want error for deadline, got %s",
				r.TaskID, r.Status)
		}
	}
}

func TestOrchestrator_NoRunSubtask(t *testing.T) {
	orch := &TaskOrchestrator{}
	tasks := []Subtask{{ID: "a", Prompt: "do a"}}
	results, err := orch.DispatchTasks(context.Background(), tasks, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("want error status when RunSubtask is nil, got %s",
			results[0].Status)
	}
}

func TestOrchestrator_DepthLimit(t *testing.T) {
	// At max depth, subtasks should be rejected.
	orch := &TaskOrchestrator{
		Depth: maxSubagentDepth,
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			return TaskResult{TaskID: task.ID, Status: "ok"}
		},
	}
	tasks := []Subtask{{ID: "a", Prompt: "do a"}}
	results, err := orch.DispatchTasks(context.Background(), tasks, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("want error status at max depth, got %s", results[0].Status)
	}
	if results[0].Error == "" {
		t.Error("want error message at max depth, got empty")
	}
}

func TestOrchestrator_DepthBelowLimit(t *testing.T) {
	// Below max depth, subtasks should succeed.
	orch := &TaskOrchestrator{
		Depth: maxSubagentDepth - 1,
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			return TaskResult{TaskID: task.ID, Status: "ok", Output: "done"}
		},
	}
	tasks := []Subtask{{ID: "a", Prompt: "do a"}}
	results, err := orch.DispatchTasks(context.Background(), tasks, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Status != "ok" {
		t.Errorf("want ok status below max depth, got %s", results[0].Status)
	}
}

func TestOrchestrator_SubtaskTimeout(t *testing.T) {
	// SubtaskTimeout should apply a per-subtask deadline and cause
	// DispatchTasks to return an error when subtasks exceed it.
	orch := &TaskOrchestrator{
		SubtaskTimeout: 20 * time.Millisecond,
		RunSubtask:     slowSubtask(500 * time.Millisecond),
	}
	tasks := []Subtask{
		{ID: "a", Prompt: "do a"},
		{ID: "b", Prompt: "do b"},
	}
	start := time.Now()
	_, err := orch.DispatchTasks(context.Background(), tasks, true)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error for subtask timeout, got nil")
	}
	// Should return promptly, not wait for the full subtask duration.
	if elapsed >= 500*time.Millisecond {
		t.Errorf("timeout took too long (%v), subtask deadline not enforced", elapsed)
	}
}

func TestOrchestrator_TotalTimeout(t *testing.T) {
	// TotalTimeout caps the entire DispatchTasks wall-clock time.
	orch := &TaskOrchestrator{
		TotalTimeout: 30 * time.Millisecond,
		RunSubtask:   slowSubtask(1 * time.Second),
	}
	tasks := []Subtask{
		{ID: "a", Prompt: "do a"},
		{ID: "b", Prompt: "do b"},
	}
	start := time.Now()
	_, err := orch.DispatchTasks(context.Background(), tasks, true)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error for total timeout, got nil")
	}
	if elapsed >= 200*time.Millisecond {
		t.Errorf("total timeout took too long (%v), hard deadline not enforced", elapsed)
	}
}

func TestOrchestrator_TotalTimeoutPrecedence(t *testing.T) {
	// TotalTimeout takes precedence over SubtaskTimeout.
	// Even though SubtaskTimeout is 1s, TotalTimeout at 20ms
	// should fire first.
	orch := &TaskOrchestrator{
		SubtaskTimeout: 1 * time.Second,
		TotalTimeout:   20 * time.Millisecond,
		RunSubtask:     slowSubtask(500 * time.Millisecond),
	}
	tasks := []Subtask{{ID: "a", Prompt: "do a"}}
	start := time.Now()
	_, err := orch.DispatchTasks(context.Background(), tasks, true)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error for total timeout, got nil")
	}
	if elapsed >= 200*time.Millisecond {
		t.Errorf("total timeout precedence not enforced (%v)", elapsed)
	}
}

func TestOrchestrator_SubtaskTimeoutZero(t *testing.T) {
	// Zero SubtaskTimeout means no per-subtask deadline (legacy).
	orch := &TaskOrchestrator{
		RunSubtask: slowSubtask(10 * time.Millisecond),
	}
	tasks := []Subtask{{ID: "a", Prompt: "do a"}}
	results, err := orch.DispatchTasks(context.Background(), tasks, false)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "ok" {
		t.Errorf("want ok with zero timeout, got %s", results[0].Status)
	}
}
