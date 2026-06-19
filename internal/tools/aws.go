package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
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
	perm           string
	executor       Executor
	defaultTimeout int
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (a *AWSTool) SetDefaultTimeout(t int) {
	if t > 0 {
		a.defaultTimeout = t
	}
}

// Creates an AWSTool with the given permission level and executor.
// Pass nil executor to use the real OS executor.
func NewAWSTool(permLevel string, executor Executor) *AWSTool {
	if executor == nil {
		executor = OSExecutor{}
	}

	return &AWSTool{
		perm:           permLevel,
		executor:       executor,
		defaultTimeout: 120,
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

// Parses args, executes aws CLI with timeout, truncates output, and
// logs audit info. Permission is enforced by the agent loop.
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

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	start := time.Now()
	out, err := a.executor.Run(cmdCtx, "aws", aa.Args...)
	elapsed := time.Since(start)

	exitStr := "0"
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitStr = fmt.Sprintf("%d", exitErr.ExitCode())
		} else {
			exitStr = fmt.Sprintf("error: %v", err)
		}
	}

	slog.Info("tool run",
		"tool", "aws",
		"duration_ms", elapsed.Milliseconds(),
		"exit", exitStr,
		"args", string(RedactArgs(args)),
	)

	truncated := TruncateOutput(out)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
			result := string(truncated)
			if result != "" && !strings.HasSuffix(result, "\n") {
				result += "\n"
			}
			result += fmt.Sprintf("exit code: %d",
				exitErr.ExitCode())

			return result, nil
		}

		return string(truncated), err
	}

	return string(truncated), nil
}
