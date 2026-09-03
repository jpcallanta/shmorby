package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var fileWriteParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "absolute or relative path to the file"
		},
		"content": {
			"type": "string",
			"description": "content to write to the file"
		}
	},
	"required": ["path", "content"]
}`)

type fileWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileWriteTool creates or overwrites a file.
// When a ProjectRoot is configured, paths are resolved and confined
// to the project directory.
type FileWriteTool struct {
	perm string
	root *ProjectRoot
}

// NewFileWriteTool creates a FileWriteTool with the given permission level.
func NewFileWriteTool(permLevel string) *FileWriteTool {
	return &FileWriteTool{perm: permLevel}
}

// SetProjectRoot anchors file writes to the given project root.
func (f *FileWriteTool) SetProjectRoot(r *ProjectRoot) { f.root = r }

// Name returns the tool name.
func (f *FileWriteTool) Name() string { return "file_write" }

// Description returns the tool description.
func (f *FileWriteTool) Description() string {
	return `Create or overwrite a file with the given content.
Creates parent directories if they don't exist.
Returns a confirmation with the byte count.`
}

// Parameters returns the JSON schema for file_write parameters.
func (f *FileWriteTool) Parameters() json.RawMessage { return fileWriteParams }

// PermLevel returns the configured permission level.
func (f *FileWriteTool) PermLevel() string { return f.perm }

// SetPerm updates the permission level at runtime.
func (f *FileWriteTool) SetPerm(level string) { f.perm = level }

// Run creates or overwrites a file.
// Paths are resolved against the project root when configured.
func (f *FileWriteTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a fileWriteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid file_write args: %w", err)
	}

	if a.Path == "" {
		return "", fmt.Errorf(`file_write: missing required field "path"`)
	}

	// Resolve and confine the path to the project root.
	resolved, err := resolvePath(f.root, a.Path, ToolFileWrite)
	if err != nil {
		return "", err
	}

	// Create parent directories if needed. The 0o777/0o666 modes are
	// filtered by the process umask at creation (umask 022 yields the
	// previous 0755/0644), so a restrictive umask — common on hosts
	// holding secrets — is honored instead of being overridden by a
	// hardcoded world-readable mode. Existing files keep their own
	// permissions: WriteFile's mode only applies to new files.
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "", fmt.Errorf("file_write: mkdir: %w", err)
	}

	if err := os.WriteFile(resolved, []byte(a.Content), 0o666); err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}

	return fmt.Sprintf(
		"file_write: wrote %d bytes to %s",
		len(a.Content), resolved,
	), nil
}
