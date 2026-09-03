package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"shmorby/internal/audit"
	cmdexec "shmorby/internal/exec"
)

//go:embed aws.txt
var awsDescription string

var awsParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"args": {
			"type": "array",
			"items": {
				"type": "string"
			},
			"description": "AWS CLI arguments"
		},
		"timeout_seconds": {
			"type": "integer",
			"description": "timeout in seconds (default: 120)"
		}
	},
	"required": ["args"]
}`)

type awsArgs struct {
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// AWSTool implements Tool for AWS CLI execution.
type AWSTool struct {
	ExecTool
}

// Creates an AWSTool with the given permission level and executor.
// Pass nil executor to use the real OS executor.
func NewAWSTool(permLevel string, executor cmdexec.Executor) *AWSTool {
	if executor == nil {
		executor = cmdexec.OSExecutor{}
	}

	return &AWSTool{
		ExecTool: ExecTool{
			perm:           permLevel,
			executor:       executor,
			defaultTimeout: 120,
		},
	}
}

// Returns the tool name.
func (a *AWSTool) Name() string { return "aws" }

// Returns the embedded LLM description.
func (a *AWSTool) Description() string { return awsDescription }

// Returns the JSON schema for AWS parameters.
func (a *AWSTool) Parameters() json.RawMessage { return awsParams }

// PermLevel returns the configured permission level.
func (a *AWSTool) PermLevel() string { return a.perm }

// SetPerm updates the permission level at runtime.
func (a *AWSTool) SetPerm(level string) { a.perm = level }

// Parses args, executes aws CLI with timeout, truncates output, and
// redacts secrets. Permission is enforced by the agent loop.
func (a *AWSTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var aa awsArgs
	if err := json.Unmarshal(args, &aa); err != nil {
		return "", fmt.Errorf("invalid aws args: %w", err)
	}
	if len(aa.Args) == 0 {
		return "", fmt.Errorf(
			`aws: missing required field "args"`,
		)
	}

	timeout := a.defaultTimeout
	if aa.TimeoutSeconds > 0 {
		timeout = aa.TimeoutSeconds
	}
	// Clamp to hard ceiling to prevent resource exhaustion.
	if timeout > maxTimeoutSeconds {
		timeout = maxTimeoutSeconds
	}

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	start := time.Now()
	out, err := a.executor.Run(cmdCtx, "aws", aa.Args...)
	elapsed := time.Since(start)

	slog.Info("tool run",
		"tool", ToolAWS,
		"duration_ms", elapsed.Milliseconds(),
		"exit", formatExitStatus(err),
		"args", string(RedactArgs(args)),
	)

	logAuditEvent(a.auditLogger, ctx, ToolAWS, args, elapsed, err, out,
		&audit.OutputCapture{
			SessionID:  SessionIDFrom(ctx),
			Stdout:     string(out),
			StdoutSize: len(out),
			Checksum:   audit.ComputeChecksum(string(out)),
		},
	)

	result := string(TruncateOutput(out))

	if err != nil {
		return exitCodeResult(result, err, elapsed, ToolAWS)
	}

	return result, nil
}
