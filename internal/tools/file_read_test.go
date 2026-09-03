package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileReadTool_BasicRead checks basic file reading with line numbers.
func TestFileReadTool_BasicRead(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileReadTool("allow")
	args, _ := json.Marshal(fileReadArgs{Path: path})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !containsAll(result, "1: line one", "2: line two", "3: line three") {
		t.Errorf("want line-numbered content, got:\n%s", result)
	}
}

// TestFileReadTool_LineRange checks offset/limit parameters.
func TestFileReadTool_LineRange(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "a\nb\nc\nd\ne\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileReadTool("allow")
	args, _ := json.Marshal(fileReadArgs{Path: path, Offset: 2, Limit: 2})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !containsAll(result, "2: b", "3: c") {
		t.Errorf("want lines 2-3, got:\n%s", result)
	}
	if containsStr(result, "1: a") {
		t.Errorf("should not contain line 1, got:\n%s", result)
	}
	if containsStr(result, "4: d") {
		t.Errorf("should not contain line 4, got:\n%s", result)
	}
}

// TestFileReadTool_MissingFile checks error on missing file.
func TestFileReadTool_MissingFile(t *testing.T) {
	tool := NewFileReadTool("allow")
	args, _ := json.Marshal(fileReadArgs{Path: "/nonexistent/file.txt"})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestFileReadTool_EmptyPath checks error on empty path.
func TestFileReadTool_EmptyPath(t *testing.T) {
	tool := NewFileReadTool("allow")
	args, _ := json.Marshal(fileReadArgs{Path: ""})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

// TestFileReadTool_RejectsNonRegular verifies directories and character
// devices are rejected before os.ReadFile.
func TestFileReadTool_RejectsNonRegular(t *testing.T) {
	tmpDir := t.TempDir()

	tool := NewFileReadTool("allow")

	// A directory is not a regular file.
	dirArgs, _ := json.Marshal(fileReadArgs{Path: tmpDir})
	if _, err := tool.Run(context.Background(), dirArgs); err == nil {
		t.Error("want error reading directory, got nil")
	}

	// /dev/zero (if present) must be rejected, not read forever.
	if _, statErr := os.Stat("/dev/zero"); statErr == nil {
		devArgs, _ := json.Marshal(fileReadArgs{Path: "/dev/zero"})
		if _, err := tool.Run(context.Background(), devArgs); err == nil {
			t.Error("want error reading /dev/zero, got nil")
		}
	}
}

// TestFileReadTool_RejectsOversized verifies files above the read cap
// are rejected before being loaded into memory.
func TestFileReadTool_RejectsOversized(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "big.bin")

	// Create a sparse file just over the cap; Size() reflects the
	// logical length without physically writing bytes.
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tErr := f.Truncate(maxReadableBytes + 1); tErr != nil {
		f.Close()
		t.Fatalf("truncate: %v", tErr)
	}
	f.Close()

	tool := NewFileReadTool("allow")
	args, _ := json.Marshal(fileReadArgs{Path: path})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Error("want error for oversized file, got nil")
	}
}

// TestFileReadTool_Metadata checks tool metadata.
func TestFileReadTool_Metadata(t *testing.T) {
	tool := NewFileReadTool("read")
	if tool.Name() != "file_read" {
		t.Errorf("want name 'file_read', got %q", tool.Name())
	}
	if tool.PermLevel() != "read" {
		t.Errorf("want perm 'read', got %q", tool.PermLevel())
	}

	tool.SetPerm("write")
	if tool.PermLevel() != "write" {
		t.Errorf("want perm 'write' after SetPerm, got %q", tool.PermLevel())
	}
}

// containsAll checks if s contains all substrings using stdlib.
func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// containsStr is a thin alias for strings.Contains.
func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
