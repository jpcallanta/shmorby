package fileread

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadFileLimited_NormalFile verifies normal files are read successfully.
func TestReadFileLimited_NormalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFileLimited(path, 0)
	if err != nil {
		t.Fatalf("ReadFileLimited: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("ReadFileLimited = %q, want %q", got, content)
	}
}

// TestReadFileLimited_EmptyFile verifies empty files are read successfully.
func TestReadFileLimited_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFileLimited(path, 0)
	if err != nil {
		t.Fatalf("ReadFileLimited: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadFileLimited(empty) = %d bytes, want 0", len(got))
	}
}

// TestReadFileLimited_FileExceedsLimit verifies files exceeding limit are rejected.
func TestReadFileLimited_FileExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Set limit to 5 bytes (smaller than the file)
	_, err := ReadFileLimited(path, 5)
	if err == nil {
		t.Fatal("ReadFileLimited should have returned error for file exceeding limit")
	}
}

// TestReadFileLimited_CustomLimit verifies custom limits are respected.
func TestReadFileLimited_CustomLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := []byte("hello")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Should succeed with 10 byte limit
	got, err := ReadFileLimited(path, 10)
	if err != nil {
		t.Fatalf("ReadFileLimited: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("ReadFileLimited = %q, want %q", got, content)
	}

	// Should fail with 3 byte limit
	_, err = ReadFileLimited(path, 3)
	if err == nil {
		t.Fatal("ReadFileLimited should have returned error for file exceeding custom limit")
	}
}

// TestReadFileLimited_NonexistentFile verifies error on missing file.
func TestReadFileLimited_NonexistentFile(t *testing.T) {
	_, err := ReadFileLimited("/nonexistent/file/path.txt", 0)
	if err == nil {
		t.Fatal("ReadFileLimited should have returned error for nonexistent file")
	}
}

// TestMaxFileReadSize verifies the default max file read size is 10MB.
func TestMaxFileReadSize(t *testing.T) {
	expected := int64(10 * 1024 * 1024)
	if MaxFileReadSize != expected {
		t.Errorf("MaxFileReadSize = %d, want %d", MaxFileReadSize, expected)
	}
}
