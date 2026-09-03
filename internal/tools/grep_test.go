package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGrepTool_BasicSearch checks basic pattern matching.
func TestGrepTool_BasicSearch(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, filepath.Join(tmpDir, "a.go"), "package main\nfunc main() {}\n")
	writeFile(t, filepath.Join(tmpDir, "b.go"), "package utils\nfunc Helper() {}\n")

	tool := NewGrepTool("allow")
	args, _ := json.Marshal(grepArgs{
		Pattern: "func",
		Path:    tmpDir,
		Include: "*.go",
	})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !containsStr(result, "func main()") {
		t.Errorf("want 'func main()' in results, got:\n%s", result)
	}
	if !containsStr(result, "func Helper()") {
		t.Errorf("want 'func Helper()' in results, got:\n%s", result)
	}
}

// TestGrepTool_WithInclude checks file filter works.
func TestGrepTool_WithInclude(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, filepath.Join(tmpDir, "a.go"), "hello world\n")
	writeFile(t, filepath.Join(tmpDir, "b.txt"), "hello world\n")

	tool := NewGrepTool("allow")
	args, _ := json.Marshal(grepArgs{
		Pattern: "hello",
		Path:    tmpDir,
		Include: "*.go",
	})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !containsStr(result, "a.go") {
		t.Errorf("want a.go in results, got:\n%s", result)
	}
	if containsStr(result, "b.txt") {
		t.Errorf("should not contain b.txt, got:\n%s", result)
	}
}

// TestGrepTool_NoMatches checks empty result for no matches.
func TestGrepTool_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, filepath.Join(tmpDir, "a.txt"), "hello world\n")

	tool := NewGrepTool("allow")
	args, _ := json.Marshal(grepArgs{
		Pattern: "nonexistent",
		Path:    tmpDir,
	})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result != "no matches" {
		t.Errorf("want 'no matches', got %q", result)
	}
}

// TestGrepTool_EmptyPattern checks error on empty pattern.
func TestGrepTool_EmptyPattern(t *testing.T) {
	tool := NewGrepTool("allow")
	args, _ := json.Marshal(grepArgs{Pattern: ""})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty pattern, got nil")
	}
}

// TestGrepTool_InvalidRegex checks error on invalid regex.
func TestGrepTool_InvalidRegex(t *testing.T) {
	tool := NewGrepTool("allow")
	args, _ := json.Marshal(grepArgs{Pattern: "[invalid"})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

// TestGrepTool_Metadata checks tool metadata.
func TestGrepTool_Metadata(t *testing.T) {
	tool := NewGrepTool("read")
	if tool.Name() != "grep" {
		t.Errorf("want name 'grep', got %q", tool.Name())
	}
	if tool.PermLevel() != "read" {
		t.Errorf("want perm 'read', got %q", tool.PermLevel())
	}
}

// writeFile is a test helper to write content to a file.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}
