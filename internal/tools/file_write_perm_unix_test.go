//go:build unix

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestFileWriteTool_RestrictiveUmask_Honored checks new files and
// directories are created through the process umask instead of a
// hardcoded world-readable mode.
func TestFileWriteTool_RestrictiveUmask_Honored(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sub", ".env")
	tool := NewFileWriteTool("allow")
	args, _ := json.Marshal(fileWriteArgs{
		Path:    path,
		Content: "SECRET=1",
	})

	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("want perm 0600 under umask 077, got %04o", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("want dir perm 0700 under umask 077, got %04o", perm)
	}
}

// TestFileWriteTool_ExistingFile_PreservesPerms checks overwriting a
// 0600 file does not broaden its permissions.
func TestFileWriteTool_ExistingFile_PreservesPerms(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tool := NewFileWriteTool("allow")
	args, _ := json.Marshal(fileWriteArgs{
		Path:    path,
		Content: "new",
	})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("want perm preserved at 0600, got %04o", perm)
	}
}
