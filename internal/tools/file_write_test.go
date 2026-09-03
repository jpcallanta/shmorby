package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestFileWriteTool_Create checks file creation with parent dirs.
func TestFileWriteTool_Create(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sub", "test.txt")
	tool := NewFileWriteTool("allow")
	args, _ := json.Marshal(fileWriteArgs{
		Path:    path,
		Content: "hello world",
	})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !containsStr(result, "wrote 11 bytes") {
		t.Errorf("want byte count confirmation, got:\n%s", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("want 'hello world', got %q", string(data))
	}
}

// TestFileWriteTool_Overwrite checks file overwrite.
func TestFileWriteTool_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileWriteTool("allow")
	args, _ := json.Marshal(fileWriteArgs{
		Path:    path,
		Content: "new content",
	})

	_, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("want 'new content', got %q", string(data))
	}
}

// TestFileWriteTool_EmptyPath checks error on empty path.
func TestFileWriteTool_EmptyPath(t *testing.T) {
	tool := NewFileWriteTool("allow")
	args, _ := json.Marshal(fileWriteArgs{Path: "", Content: "x"})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

// TestFileWriteTool_Metadata checks tool metadata.
func TestFileWriteTool_Metadata(t *testing.T) {
	tool := NewFileWriteTool("write")
	if tool.Name() != "file_write" {
		t.Errorf("want name 'file_write', got %q", tool.Name())
	}
	if tool.PermLevel() != "write" {
		t.Errorf("want perm 'write', got %q", tool.PermLevel())
	}
}
