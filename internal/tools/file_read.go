package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

var fileReadParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "absolute or relative path to the file"
		},
		"offset": {
			"type": "integer",
			"description": "1-indexed line to start reading from (default: 1)"
		},
		"limit": {
			"type": "integer",
			"description": "maximum number of lines to return (default: 2000)"
		}
	},
	"required": ["path"]
}`)

type fileReadArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// maxReadableBytes bounds how large a file os.ReadFile may load before
// line selection. Character devices such as /dev/zero make ReadFile loop
// until the process is OOM-killed, and any huge regular file can exhaust
// memory regardless of offset/limit. 10 MiB is far above any source file
// opened interactively.
const maxReadableBytes = 10 << 20

// FileReadTool reads file contents with optional line ranges.
// When a ProjectRoot is configured, paths are resolved and confined
// to the project directory (matching opencode's launch-CWD behavior).
type FileReadTool struct {
	perm string
	root *ProjectRoot
}

// NewFileReadTool creates a FileReadTool with the given permission level.
func NewFileReadTool(permLevel string) *FileReadTool {
	return &FileReadTool{perm: permLevel}
}

// SetProjectRoot anchors file reads to the given project root.
func (f *FileReadTool) SetProjectRoot(r *ProjectRoot) { f.root = r }

// Name returns the tool name.
func (f *FileReadTool) Name() string { return "file_read" }

// Description returns the tool description.
func (f *FileReadTool) Description() string {
	return `Read the contents of a file with optional line ranges.
Returns line-numbered content. Use offset and limit to read
specific sections of large files.`
}

// Parameters returns the JSON schema for file_read parameters.
func (f *FileReadTool) Parameters() json.RawMessage { return fileReadParams }

// PermLevel returns the configured permission level.
func (f *FileReadTool) PermLevel() string { return f.perm }

// SetPerm updates the permission level at runtime.
func (f *FileReadTool) SetPerm(level string) { f.perm = level }

// Run reads a file and returns line-numbered content, rejecting paths
// outside the project root and non-regular or oversized targets.
// Paths are resolved against the project root when configured.
func (f *FileReadTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a fileReadArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid file_read args: %w", err)
	}

	if a.Path == "" {
		return "", fmt.Errorf(`file_read: missing required field "path"`)
	}

	// Resolve and confine the path to the project root.
	resolved, err := resolvePath(f.root, a.Path, ToolFileRead)
	if err != nil {
		return "", err
	}

	// Reject non-regular files (device nodes, FIFOs, directories) and
	// oversized files up front: os.ReadFile on /dev/zero or a huge file
	// would block or exhaust memory before offset/limit apply.
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf(
			"file_read: not a regular file: %s", resolved,
		)
	}
	if info.Size() > maxReadableBytes {
		return "", fmt.Errorf(
			"file_read: file too large (%d bytes, max %d): %s",
			info.Size(), maxReadableBytes, resolved,
		)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	// Drop trailing empty line from Split.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Clamp offset to valid range.
	offset := a.Offset
	if offset < 1 {
		offset = 1
	}
	if offset > len(lines) {
		return fmt.Sprintf(
			"file_read: offset %d exceeds file length %d lines in %s",
			offset, len(lines), a.Path,
		), nil
	}

	// Clamp limit.
	limit := a.Limit
	if limit <= 0 {
		limit = 2000
	}

	end := offset - 1 + limit
	if end > len(lines) {
		end = len(lines)
	}

	selected := lines[offset-1 : end : end]
	var sb strings.Builder
	for i, line := range selected {
		fmt.Fprintf(&sb, "%d: %s\n", offset+i, line)
	}

	return sb.String(), nil
}
