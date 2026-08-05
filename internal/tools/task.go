package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Subtask describes a single subtask for a subagent.
type Subtask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// TaskParams is the JSON schema for the task tool arguments.
type TaskParams struct {
	Tasks    []Subtask `json:"tasks"`
	Parallel bool      `json:"parallel"`
}

// TaskResult is the structured output for one subtask.
type TaskResult struct {
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
}

// RunSubtaskFunc runs a single subtask and returns its result.
type RunSubtaskFunc func(ctx context.Context, task Subtask) TaskResult

// SubagentEvent describes a subagent lifecycle event for TUI integration.
type SubagentEvent struct {
	// Session is the child session; the TUI model receives the
	// *session.Session pointer via this field. Stored as any to
	// avoid an import cycle (tools → session → llm → config → tools).
	Session   any
	SessionID string
	ParentID  string
	Label     string
	Status    string
}

var taskParamsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tasks": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"id": {
						"type": "string",
						"description": "unique identifier for this subtask"
					},
					"description": {
						"type": "string",
						"description": "human-readable description of the subtask"
					},
					"prompt": {
						"type": "string",
						"description": "the prompt/instructions for the subagent"
					}
				},
				"required": ["id", "description", "prompt"]
			},
			"description": "list of subtasks to dispatch"
		},
		"parallel": {
			"type": "boolean",
			"description": "run subtasks concurrently (true) or sequentially (false)",
			"default": false
		}
	},
	"required": ["tasks"]
}`)

// TaskTool implements Tool for dispatching subagents.
type TaskTool struct {
	orch *TaskOrchestrator
	perm string // "allow", "ask", or "deny"; default "ask"
}

// NewTaskTool creates a TaskTool with the given orchestrator.
func NewTaskTool(orch *TaskOrchestrator) *TaskTool {
	return &TaskTool{orch: orch, perm: "ask"}
}

// SetPerm sets the permission level for this tool.
func (t *TaskTool) SetPerm(level string) { t.perm = level }

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Dispatch sub-agent tasks. Use when you have multiple " +
		"independent pieces of work that could be done concurrently. " +
		"Each subtask gets its own agent session with tool access " +
		"inherited from the parent session's permission constraints. " +
		"Set parallel: true for concurrent execution, false for sequential. " +
		"Results are returned as a JSON array of per-task outputs."
}

func (t *TaskTool) PermLevel() string { return t.perm }

func (t *TaskTool) Parameters() json.RawMessage { return taskParamsSchema }

func (t *TaskTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var params TaskParams
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("task: invalid params: %w", err)
	}
	results, err := t.orch.DispatchTasks(ctx, params.Tasks, params.Parallel)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("task: marshal results: %w", err)
	}
	return string(data), nil
}
