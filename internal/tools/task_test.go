package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTaskTool_Basic(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			return TaskResult{
				TaskID: task.ID, Description: task.Description,
				Status: "ok", Output: "result: " + task.Prompt,
			}
		},
	}
	tool := NewTaskTool(orch)
	args := `{"tasks":[{"id":"x","description":"test","prompt":"hello"}],"parallel":false}`
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var results []TaskResult
	if err := json.Unmarshal([]byte(result), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].TaskID != "x" {
		t.Errorf("want task_id x, got %s", results[0].TaskID)
	}
	if results[0].Status != "ok" {
		t.Errorf("want status ok, got %s", results[0].Status)
	}
	if results[0].Output != "result: hello" {
		t.Errorf("want output 'result: hello', got %s", results[0].Output)
	}
}

func TestTaskTool_EmptyTasks(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			return TaskResult{TaskID: task.ID, Status: "ok"}
		},
	}
	tool := NewTaskTool(orch)
	result, err := tool.Run(context.Background(), json.RawMessage(`{"tasks":[],"parallel":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != `[]` {
		t.Errorf("empty tasks should return '[]', got %s", result)
	}
}

func TestTaskTool_Schema(t *testing.T) {
	tool := NewTaskTool(nil)
	var schema map[string]interface{}
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
		t.Fatal("invalid schema:", err)
	}
	if _, ok := schema["properties"]; !ok {
		t.Error("schema missing properties")
	}
	if _, ok := schema["required"]; !ok {
		t.Error("schema missing required")
	}
}

func TestTaskTool_InvalidArgs(t *testing.T) {
	tool := NewTaskTool(nil)
	_, err := tool.Run(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

func TestTaskTool_NameDescriptionPermLevel(t *testing.T) {
	tool := NewTaskTool(nil)
	if tool.Name() != "task" {
		t.Errorf("want name 'task', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
	if tool.PermLevel() != "ask" {
		t.Errorf("want perm 'ask', got %q", tool.PermLevel())
	}
}

func TestTaskTool_SetPerm(t *testing.T) {
	tool := NewTaskTool(nil)
	if tool.PermLevel() != "ask" {
		t.Errorf("default perm: want 'ask', got %q", tool.PermLevel())
	}
	tool.SetPerm("allow")
	if tool.PermLevel() != "allow" {
		t.Errorf("after SetPerm('allow'): want 'allow', got %q", tool.PermLevel())
	}
	tool.SetPerm("deny")
	if tool.PermLevel() != "deny" {
		t.Errorf("after SetPerm('deny'): want 'deny', got %q", tool.PermLevel())
	}
	tool.SetPerm("ask")
	if tool.PermLevel() != "ask" {
		t.Errorf("after SetPerm('ask'): want 'ask', got %q", tool.PermLevel())
	}
}

func TestTaskTool_Cancellation(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			select {
			case <-time.After(1 * time.Second):
			case <-ctx.Done():
				return TaskResult{
					TaskID: task.ID, Status: "error",
					Error: ctx.Err().Error(),
				}
			}
			return TaskResult{TaskID: task.ID, Status: "ok"}
		},
	}
	tool := NewTaskTool(orch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	args := `{"tasks":[{"id":"x","description":"test","prompt":"hello"}],"parallel":false}`
	result, err := tool.Run(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var results []TaskResult
	if err := json.Unmarshal([]byte(result), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Status != "error" {
		t.Errorf("want error status for cancelled context, got %s",
			results[0].Status)
	}
}

func TestTaskTool_MultipleTasks(t *testing.T) {
	orch := &TaskOrchestrator{
		RunSubtask: func(ctx context.Context, task Subtask) TaskResult {
			return TaskResult{
				TaskID: task.ID, Status: "ok",
				Output: "done: " + task.ID,
			}
		},
	}
	tool := NewTaskTool(orch)
	args := `{"tasks":[
		{"id":"a","description":"first","prompt":"do a"},
		{"id":"b","description":"second","prompt":"do b"},
		{"id":"c","description":"third","prompt":"do c"}
	],"parallel":true}`
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var results []TaskResult
	if err := json.Unmarshal([]byte(result), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	idSet := make(map[string]bool)
	for _, r := range results {
		idSet[r.TaskID] = true
	}
	if !idSet["a"] || !idSet["b"] || !idSet["c"] {
		t.Error("results missing expected task IDs")
	}
}
