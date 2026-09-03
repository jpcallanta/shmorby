package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckPathSafety_BlockedDirs checks blocked directories are rejected.
func TestCheckPathSafety_BlockedDirs(t *testing.T) {
	blocked := []string{
		".git/config",
		"vendor/github.com/foo/bar.go",
		"node_modules/pkg/index.js",
		".idea/workspace.xml",
		".vscode/settings.json",
		"src/.git/HEAD",
		"a/vendor/b/c.go",
	}
	for _, path := range blocked {
		if err := CheckPathSafety(path); err == nil {
			t.Errorf("want error for blocked path %q, got nil", path)
		}
	}
}

// TestCheckPathSafety_SafePaths checks safe paths are allowed.
func TestCheckPathSafety_SafePaths(t *testing.T) {
	safe := []string{
		"main.go",
		"src/main.go",
		"internal/tools/file_read.go",
		"./config.yaml",
		"../other-project/file.go",
	}
	for _, path := range safe {
		if err := CheckPathSafety(path); err != nil {
			t.Errorf("want nil error for safe path %q, got %v", path, err)
		}
	}
}

// TestFileWriteTool_BlockedPath拒绝写入被阻止的目录。
func TestFileWriteTool_BlockedPath(t *testing.T) {
	tool := NewFileWriteTool("allow")
	args, _ := json.Marshal(fileWriteArgs{
		Path:    ".git/config",
		Content: "evil",
	})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for blocked path, got nil")
	}
}

// TestFileEditTool_BlockedPath拒绝编辑被阻止的目录。
func TestFileEditTool_BlockedPath(t *testing.T) {
	tmpDir := t.TempDir()
	blocked := filepath.Join(tmpDir, "vendor", "foo.go")
	os.MkdirAll(filepath.Dir(blocked), 0o755)
	os.WriteFile(blocked, []byte("old"), 0o644)

	tool := NewFileEditTool("allow")
	args, _ := json.Marshal(fileEditArgs{
		Path:      blocked,
		OldString: "old",
		NewString: "new",
	})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for blocked path, got nil")
	}
}

// TestFileReadTool_BlockedPath拒绝读取被阻止的目录。
func TestFileReadTool_BlockedPath(t *testing.T) {
	tool := NewFileReadTool("allow")
	args, _ := json.Marshal(fileReadArgs{
		Path: "node_modules/pkg/index.js",
	})

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for blocked path, got nil")
	}
}

// TestCheckPathSafety_AbsoluteUnixPaths verifies the loop terminates
// at the Unix root "/" without hanging. Absolute paths are only
// blocked if they contain a blocked directory name.
func TestCheckPathSafety_AbsoluteUnixPaths(t *testing.T) {
	// Must not hang — the loop terminates via fixed-point check.
	// /etc/passwd is safe (no blocked dir); /home/.git/config is blocked.
	if err := CheckPathSafety("/etc/passwd"); err != nil {
		t.Errorf("want nil for safe absolute path, got %v", err)
	}
	if err := CheckPathSafety("/home/user/.git/config"); err == nil {
		t.Error("want error for absolute path with .git, got nil")
	}
	if err := CheckPathSafety("/"); err != nil {
		t.Errorf("want nil for root path, got %v", err)
	}
}

// TestCheckPathSafety_WindowsStylePaths verifies the loop terminates
// at Windows drive roots (e.g. C:\) without infinite looping.
// On Unix, filepath.Clean normalizes these to forward-slash form,
// so we test the forward-slash equivalent to ensure the fixed-point
// termination logic works on both platforms.
func TestCheckPathSafety_WindowsStylePaths(t *testing.T) {
	// Windows drive roots normalized by filepath.Clean on Unix:
	// "C:/repo/internal/file.go" → cleaned as-is (no drive on Unix)
	// but the loop must still terminate via the fixed-point check.
	paths := []string{
		"C:/repo/vendor/foo.go",     // blocked (vendor)
		"C:/repo/.git/config",       // blocked (.git)
		"C:/repo/src/main.go",       // safe
		"C:/repo/node_modules/x.js", // blocked (node_modules)
	}
	for _, path := range paths {
		// Verify it doesn't hang — fail after 1 second.
		done := make(chan error, 1)
		go func() {
			done <- CheckPathSafety(path)
		}()
		select {
		case <-done:
			// OK — function returned without hanging.
		case <-time.After(time.Second):
			t.Fatalf("CheckPathSafety hung on path %q", path)
		}
	}
}

// TestCheckPathConfinement_BlockedDirs verifies that blocked directory
// names (.git/, vendor/, node_modules/, .idea/, .vscode/) are rejected
// at ANY depth through the resolved path — regression test for C1
// (filepath.Match treats ** as *, so glob patterns alone don't work).
func TestCheckPathConfinement_BlockedDirs(t *testing.T) {
	root := &ProjectRoot{
		Root:            t.TempDir(),
		BlockedPatterns: nil, // no extra patterns needed
	}

	blocked := []string{
		".git/config",
		"src/.git/HEAD",
		"a/vendor/b/c.go",
		"deep/nested/node_modules/pkg/index.js",
		".idea/workspace.xml",
		".vscode/settings.json",
		"vendor/github.com/foo/bar.go",
		"node_modules/pkg/index.js",
	}
	for _, path := range blocked {
		_, err := root.CheckPathConfinement(path)
		if err == nil {
			t.Errorf("want error for blocked path %q, got nil", path)
		}
	}
}

// TestCheckPathConfinement_SafePaths verifies ordinary inside-root paths
// pass through. A real temp root is used so confinement (which now
// compares against the canonical root) is exercised correctly.
func TestCheckPathConfinement_SafePaths(t *testing.T) {
	root := &ProjectRoot{
		Root:            t.TempDir(),
		BlockedPatterns: nil,
	}

	safe := []string{
		"main.go",
		"src/main.go",
		"internal/tools/file_read.go",
	}
	for _, path := range safe {
		if _, err := root.CheckPathConfinement(path); err != nil {
			t.Errorf("want nil for safe path %q, got %v", path, err)
		}
	}
}

// TestCheckPathConfinement_OutsideRootBlocked verifies paths resolving
// outside the project root are now rejected. Real
// temp dirs are used so the symlink-canonical containment check is
// exercised.
func TestCheckPathConfinement_OutsideRootBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	pr := &ProjectRoot{Root: root}

	// Absolute path outside root — must be blocked.
	if _, err := pr.CheckPathConfinement(outsideFile); err == nil {
		t.Error("want error for absolute path outside root, got nil")
	}

	// ../ traversal outside root — must be blocked.
	rel, _ := filepath.Rel(root, outsideFile)
	if _, err := pr.CheckPathConfinement(rel); err == nil {
		t.Error("want error for traversal outside root, got nil")
	}

	// The project root itself is allowed and resolves to itself.
	got, err := pr.CheckPathConfinement(".")
	if err != nil {
		t.Fatalf("want nil for project root, got %v", err)
	}
	want, wErr := filepath.EvalSymlinks(root)
	if wErr != nil {
		want = filepath.Clean(root)
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Errorf("want root %q, got %q", want, got)
	}
}

// TestCheckPathConfinement_FileSystemRoot verifies a project root at
// the filesystem/drive root confines nothing, so inside paths must not
// false-reject (a naive string-prefix test compares against "//" and
// flags every path as an escape).
func TestCheckPathConfinement_FileSystemRoot(t *testing.T) {
	tmp := t.TempDir()
	// "/" on POSIX, "C:\" on Windows.
	root := filepath.VolumeName(tmp) + string(filepath.Separator)
	pr := &ProjectRoot{Root: root}

	path := filepath.Join(tmp, "a.txt")
	if _, err := pr.CheckPathConfinement(path); err != nil {
		t.Errorf("want nil for path under root %q, got %v", root, err)
	}

	if _, err := pr.CheckPathConfinement(root); err != nil {
		t.Errorf("want nil for root %q itself, got %v", root, err)
	}
}

// TestCheckPathConfinement_AllowedPatternsOutsideRoot verifies an
// explicit AllowedPattern permits operating outside the root
// (opt-out path).
func TestCheckPathConfinement_AllowedPatternsOutsideRoot(
	t *testing.T,
) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "ok.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// No allow pattern: blocked.
	blocked := &ProjectRoot{Root: root}
	if _, err := blocked.CheckPathConfinement(outsideFile); err == nil {
		t.Error("want blocked without allowlist, got nil")
	}

	// Allowlist covering the outside file: permitted.
	allowed := &ProjectRoot{
		Root:            root,
		AllowedPatterns: []string{outsideFile},
	}
	if _, err := allowed.CheckPathConfinement(outsideFile); err != nil {
		t.Errorf("want nil for allowlisted path, got %v", err)
	}
}

// TestCheckPathSafety_SymlinkBypass_Rejected checks a symlink (or a
// symlinked parent for not-yet-existing targets) pointing into a
// blocked directory is rejected even though the literal path names
// look safe.
func TestCheckPathSafety_SymlinkBypass_Rejected(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "repo")
	gitDir := filepath.Join(realDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(gitDir, "config"), []byte("[x]"), 0o600,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	link := filepath.Join(tmp, "evil")
	if err := os.Symlink(gitDir, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := CheckPathSafety(filepath.Join(link, "config")); err == nil {
		t.Error("want error for existing file via symlink, got nil")
	}
	// Nonexistent target under the symlinked blocked dir (the
	// file_write create case) must fail the same way.
	if err := CheckPathSafety(filepath.Join(link, "newfile")); err == nil {
		t.Error("want error for missing file via symlink, got nil")
	}
}

// TestCheckPathSafety_SymlinkSafe_Passes checks a symlink into a
// non-blocked directory still passes.
func TestCheckPathSafety_SymlinkSafe_Passes(t *testing.T) {
	tmp := t.TempDir()
	safe := filepath.Join(tmp, "src")
	if err := os.MkdirAll(safe, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(tmp, "current")
	if err := os.Symlink(safe, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := CheckPathSafety(filepath.Join(link, "main.go")); err != nil {
		t.Errorf("want nil for safe symlink, got %v", err)
	}
}
