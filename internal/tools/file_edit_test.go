package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileEditTool_Replace checks exact match replacement.
func TestFileEditTool_Replace(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "hello world\nfoo bar\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{
		Path:      path,
		OldString: "hello world",
		NewString: "goodbye world",
	})

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !containsStr(result, "replaced 1 occurrence") {
		t.Errorf("want replacement confirmation, got:\n%s", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !containsStr(string(data), "goodbye world") {
		t.Errorf("want 'goodbye world' in file, got:\n%s", string(data))
	}
	if containsStr(string(data), "hello world") {
		t.Errorf("old string should be gone, got:\n%s", string(data))
	}
}

// TestFileEditTool_NoMatch checks error when old_string not found.
func TestFileEditTool_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{
		Path:      path,
		OldString: "nonexistent",
		NewString: "replacement",
	})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for no match, got nil")
	}
}

// TestFileEditTool_MultipleMatches checks error for ambiguous matches.
func TestFileEditTool_MultipleMatches(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	content := "foo bar foo baz foo\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{
		Path:      path,
		OldString: "foo",
		NewString: "qux",
	})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for multiple matches, got nil")
	}
}

// TestFileEditTool_MissingPath checks error on empty path.
func TestFileEditTool_MissingPath(t *testing.T) {
	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{Path: ""})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

// TestFileEditTool_MissingOldString checks error on empty old_string.
func TestFileEditTool_MissingOldString(t *testing.T) {
	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{Path: "/some/path"})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty old_string, got nil")
	}
}

// TestFileEditTool_RejectsNonRegular verifies a directory or device
// target is rejected before any write.
func TestFileEditTool_RejectsNonRegular(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{
		Path:      tmpDir,
		OldString: "x",
		NewString: "y",
	})

	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Error("want error editing a directory, got nil")
	}
}

// TestFileEditTool_AtomicWriteNoTempLeftover verifies a successful edit
// leaves no temp file and preserves the original permissions.
func TestFileEditTool_AtomicWriteNoTempLeftover(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte("alpha"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{
		Path:      path,
		OldString: "alpha",
		NewString: "beta",
	})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Permissions preserved across the atomic rename.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("want perms 0600 preserved, got %v", fi.Mode().Perm())
	}

	// No stray .file-edit-* temp files remain in the directory.
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".file-edit-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// TestFileEditTool_SymlinkSwapNotFollowed verifies a file replaced by a
// symlink before the write is not written through. The atomic
// rename replaces the link entry rather than following it.
func TestFileEditTool_SymlinkSwapNotFollowed(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target.txt")
	link := filepath.Join(tmpDir, "link.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	if err := os.WriteFile(link, []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup link: %v", err)
	}

	tool := NewFileEditTool("allow")
	// Edit the real file; then swap a symlink onto a second path and
	// confirm editing that path does not corrupt the symlink target.
	// Simulate the swap directly, then rely on SameFile/atomic rename.
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove link: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	args, _ := json.Marshal(fileEditArgs{
		Path:      link,
		OldString: "keep",
		NewString: "edited",
	})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// After an atomic rename, the link path becomes a regular file and
	// the original target is untouched (no write-through of new content
	// into target via following the symlink a second time).
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("link should be replaced by a regular file after rename")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "keep" {
		t.Errorf("target content mutated by write-through: %q", data)
	}
}

// TestFileEditTool_Metadata checks tool metadata.
func TestFileEditTool_Metadata(t *testing.T) {
	tool := NewFileEditTool("write")
	if tool.Name() != "file_edit" {
		t.Errorf("want name 'file_edit', got %q", tool.Name())
	}
	if tool.PermLevel() != "write" {
		t.Errorf("want perm 'write', got %q", tool.PermLevel())
	}
}
