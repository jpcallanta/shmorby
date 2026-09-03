package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var fileEditParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "absolute or relative path to the file"
		},
		"old_string": {
			"type": "string",
			"description": "exact text to find and replace"
		},
		"new_string": {
			"type": "string",
			"description": "replacement text"
		}
	},
	"required": ["path", "old_string", "new_string"]
}`)

type fileEditArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// FileEditTool performs precise string replacement in files.
// When a ProjectRoot is configured, paths are resolved and confined
// to the project directory.
type FileEditTool struct {
	perm string
	root *ProjectRoot
}

// NewFileEditTool creates a FileEditTool with the given permission level.
func NewFileEditTool(permLevel string) *FileEditTool {
	return &FileEditTool{perm: permLevel}
}

// SetProjectRoot anchors file edits to the given project root.
func (f *FileEditTool) SetProjectRoot(r *ProjectRoot) { f.root = r }

// Name returns the tool name.
func (f *FileEditTool) Name() string { return "file_edit" }

// Description returns the tool description.
func (f *FileEditTool) Description() string {
	return `Replace an exact string match in a file with new text.
Fails if the old_string is not found or matches multiple times.
Use enough surrounding context to make the match unique.`
}

// Parameters returns the JSON schema for file_edit parameters.
func (f *FileEditTool) Parameters() json.RawMessage { return fileEditParams }

// PermLevel returns the configured permission level.
func (f *FileEditTool) PermLevel() string { return f.perm }

// SetPerm updates the permission level at runtime.
func (f *FileEditTool) SetPerm(level string) { f.perm = level }

// Run performs an exact string replacement in a file.
// Paths are resolved against the project root when configured. The
// target is opened once and read through that handle, then replaced via
// an atomic rename, so a concurrent edit or symlink swap cannot corrupt
// data or cause a write-through to another file.
func (f *FileEditTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a fileEditArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid file_edit args: %w", err)
	}

	if a.Path == "" {
		return "", fmt.Errorf(`file_edit: missing required field "path"`)
	}
	if a.OldString == "" {
		return "", fmt.Errorf(`file_edit: missing required field "old_string"`)
	}

	// Resolve and confine the path to the project root.
	resolved, err := resolvePath(f.root, a.Path, ToolFileEdit)
	if err != nil {
		return "", err
	}

	// Open once and read through the handle so the file's identity (mod
	// time + size) is captured at read time and re-checked before writing.
	fh, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("file_edit: %w", err)
	}

	info, err := fh.Stat()
	if err != nil {
		fh.Close()
		return "", fmt.Errorf("file_edit: stat: %w", err)
	}

	// Reject non-regular targets (device nodes, FIFOs, directories) so a
	// swap onto /dev/zero cannot hang or flood the write.
	if !info.Mode().IsRegular() {
		fh.Close()
		return "", fmt.Errorf(
			"file_edit: not a regular file: %s", resolved,
		)
	}

	data, err := io.ReadAll(fh)
	if closeErr := fh.Close(); closeErr != nil && err == nil {
		return "", fmt.Errorf("file_edit: close: %w", closeErr)
	}
	if err != nil {
		return "", fmt.Errorf("file_edit: %w", err)
	}

	content := string(data)
	// Count occurrences of old_string.
	count := strings.Count(content, a.OldString)

	switch {
	case count == 0:
		return "", fmt.Errorf(
			"file_edit: old_string not found in %s; "+
				"check spelling and include surrounding context",
			resolved,
		)
	case count > 1:
		return "", fmt.Errorf(
			"file_edit: old_string matches %d times in %s; "+
				"include more surrounding context to make the match unique",
			count, resolved,
		)
	}

	// Single match — replace.
	updated := strings.Replace(content, a.OldString, a.NewString, 1)

	// Confirm the on-disk file still matches what we opened before
	// writing; a change here means another editor (or a symlink swap)
	// touched it mid-flight, and overwriting would lose their work.
	// The atomic rename below is still the hard guarantee against
	// a write-through in the residual race window.
	cur, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("file_edit: stat: %w", err)
	}
	if !os.SameFile(info, cur) ||
		cur.ModTime() != info.ModTime() || cur.Size() != info.Size() {
		return "", fmt.Errorf(
			"file_edit: file changed during edit, retry: %s", resolved,
		)
	}

	// Write to a temp file in the same directory and rename over the
	// target. rename replaces the directory entry rather than following a
	// symlink, so a swapped-in link to /etc/crontab is replaced instead
	// of written through, and readers never see a half-written file.
	if err := writeAtomic(
		resolved, []byte(updated), info.Mode().Perm(),
	); err != nil {
		return "", fmt.Errorf("file_edit: write: %w", err)
	}

	return fmt.Sprintf(
		"file_edit: replaced 1 occurrence in %s (%d bytes)",
		resolved, len(updated),
	), nil
}

// writeAtomic replaces the file at path with data via a same-directory
// temp file plus an atomic rename, preserving perm. The temp file is
// removed on any failure so no stray partial writes are left behind.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".file-edit-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Single cleanup path: close the handle and remove the temp file on
	// any error. done flips only after the rename succeeds, when the
	// temp path no longer exists; the deferred Close of an already-closed
	// handle returns an ignorable error.
	done := false
	defer func() {
		tmp.Close()
		if !done {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	// Flush to disk before renaming so a crash cannot leave an empty
	// file at the final path.
	if err := tmp.Sync(); err != nil {
		return err
	}
	// Close explicitly before renaming: Windows rejects renames of open
	// files, and a close-time flush failure must not be swallowed.
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	done = true

	return nil
}
