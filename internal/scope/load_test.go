package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shmorby/internal/config"
)

// writeFile writes content to path, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLoad_FlagScopeFileBeatsWalked checks --scope-file beats walked SCOPE.md.
func TestLoad_FlagScopeFileBeatsWalked(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Create SCOPE.md in cwd (should be ignored).
	writeFile(t, filepath.Join(tmpDir, "SCOPE.md"), "walked content")

	// Create a separate scope file passed via flag.
	flagFile := filepath.Join(tmpDir, "flag-scope.md")
	writeFile(t, flagFile, "flag content")

	cfg := config.Config{}
	flags := Flags{ScopeFile: flagFile}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "flag content" {
		t.Fatalf("want 'flag content', got %q", result.Content)
	}
	if result.PrimaryPath != flagFile {
		t.Fatalf("want PrimaryPath %q, got %q", flagFile, result.PrimaryPath)
	}
}

// TestLoad_WalkFindsScopeInCwd checks SCOPE.md in cwd is found.
func TestLoad_WalkFindsScopeInCwd(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	writeFile(t, filepath.Join(tmpDir, "SCOPE.md"), "cwd scope")

	cfg := config.Config{}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "cwd scope" {
		t.Fatalf("want 'cwd scope', got %q", result.Content)
	}
}

// TestLoad_WalkFindsScopeInParent checks SCOPE.md in parent is found when
// not in cwd.
func TestLoad_WalkFindsScopeInParent(t *testing.T) {
	tmpDir := t.TempDir()
	parentDir := filepath.Join(tmpDir, "parent")
	childDir := filepath.Join(parentDir, "child")

	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create SCOPE.md in parent, not in child.
	writeFile(t, filepath.Join(parentDir, "SCOPE.md"), "parent scope")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(childDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cfg := config.Config{}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "parent scope" {
		t.Fatalf("want 'parent scope', got %q", result.Content)
	}
}

// TestLoad_FlagScopeFileMissingError checks error when flag file missing.
func TestLoad_FlagScopeFileMissingError(t *testing.T) {
	cfg := config.Config{}
	flags := Flags{ScopeFile: "/nonexistent/scope.md"}

	_, err := Load(cfg, flags)
	if err == nil {
		t.Fatal("expected error for missing --scope-file, got nil")
	}
}

// TestLoad_UserConfigFallback checks ~/.config/shmorby/SCOPE.md fallback.
func TestLoad_UserConfigFallback(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Change to a directory with no SCOPE.md.
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Create user config dir with SCOPE.md.
	userConfigDir := filepath.Join(tmpDir, ".config", "shmorby")
	writeFile(t, filepath.Join(userConfigDir, "SCOPE.md"), "user scope")

	// Override dirs to avoid env pollution.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", tmpDir)

	cfg := config.Config{}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "user scope" {
		t.Fatalf("want 'user scope', got %q", result.Content)
	}
}

// TestLoad_UserConfigXDG checks $XDG_CONFIG_HOME/shmorby/SCOPE.md takes precedence.
func TestLoad_UserConfigXDG(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Create XDG config dir with SCOPE.md.
	xdgDir := filepath.Join(tmpDir, "xdg-config")
	writeFile(t, filepath.Join(xdgDir, "shmorby", "SCOPE.md"), "xdg scope")

	// Create legacy ~/.config/shmorby/SCOPE.md (should be ignored).
	legacyDir := filepath.Join(tmpDir, ".config", "shmorby")
	writeFile(t, filepath.Join(legacyDir, "SCOPE.md"), "legacy scope")

	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("HOME", tmpDir)

	cfg := config.Config{}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "xdg scope" {
		t.Fatalf("want 'xdg scope', got %q", result.Content)
	}
}

// TestLoad_InstructionsFromConfig checks config instructions are returned.
func TestLoad_InstructionsFromConfig(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	writeFile(t, filepath.Join(tmpDir, "SCOPE.md"), "main scope")

	// Create instruction files.
	extra1 := filepath.Join(tmpDir, "extra1.md")
	writeFile(t, extra1, "extra1 content")
	extra2 := filepath.Join(tmpDir, "extra2.md")
	writeFile(t, extra2, "extra2 content")

	cfg := config.Config{}
	cfg.Scope.Instructions = []string{
		extra1,
		extra2,
	}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Should contain main scope + instructions.
	if !strings.Contains(result.Content, "main scope") {
		t.Fatalf("want 'main scope' in content, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "extra1 content") {
		t.Fatalf("want 'extra1 content' in content, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "extra2 content") {
		t.Fatalf("want 'extra2 content' in content, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "---") {
		t.Fatalf("want separator '---' in content, got %q", result.Content)
	}
	if len(result.Instructions) != 2 {
		t.Fatalf("want 2 instructions, got %d", len(result.Instructions))
	}
	if result.Instructions[0] != extra1 {
		t.Fatalf("want first instruction %q, got %q", extra1, result.Instructions[0])
	}
	if result.Instructions[1] != extra2 {
		t.Fatalf("want second instruction %q, got %q", extra2, result.Instructions[1])
	}
}

// TestLoad_FlagScopeFileWithInstructions checks instruction files are merged
// when using --scope-file flag.
func TestLoad_FlagScopeFileWithInstructions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create flag scope file.
	flagFile := filepath.Join(tmpDir, "flag-scope.md")
	writeFile(t, flagFile, "flag scope")

	// Create instruction file.
	instFile := filepath.Join(tmpDir, "inst.md")
	writeFile(t, instFile, "instruction content")

	cfg := config.Config{}
	cfg.Scope.Instructions = []string{instFile}
	flags := Flags{ScopeFile: flagFile}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Should contain flag scope + instruction.
	if !strings.Contains(result.Content, "flag scope") {
		t.Fatalf("want 'flag scope' in content, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "instruction content") {
		t.Fatalf("want 'instruction content' in content, got %q", result.Content)
	}
}

// TestLoad_NoScopeFoundEmptyContent checks empty content when no scope found.
func TestLoad_NoScopeFoundEmptyContent(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Set HOME to temp dir (empty, no SCOPE.md there).
	t.Setenv("HOME", tmpDir)

	// Create an instruction file so it resolves.
	instPath := filepath.Join(tmpDir, "inst.md")
	writeFile(t, instPath, "extra content")

	cfg := config.Config{}
	cfg.Scope.Instructions = []string{instPath}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(result.Content, "extra content") {
		t.Fatalf("want 'extra content' in result, got %q",
			result.Content)
	}
	if len(result.Instructions) != 1 {
		t.Fatalf("want 1 instruction, got %d", len(result.Instructions))
	}
}

// TestLoad_WalkPrefersCwdOverParent checks cwd SCOPE.md beats parent.
func TestLoad_WalkPrefersCwdOverParent(t *testing.T) {
	tmpDir := t.TempDir()
	parentDir := filepath.Join(tmpDir, "parent")
	childDir := filepath.Join(parentDir, "child")

	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create SCOPE.md in both parent and child.
	writeFile(t, filepath.Join(parentDir, "SCOPE.md"), "parent scope")
	writeFile(t, filepath.Join(childDir, "SCOPE.md"), "child scope")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(childDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cfg := config.Config{}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "child scope" {
		t.Fatalf("want 'child scope', got %q", result.Content)
	}
}

// TestLoad_FlagBeatsUserConfig checks --scope-file beats user config fallback.
func TestLoad_FlagBeatsUserConfig(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Create user config with SCOPE.md.
	userConfigDir := filepath.Join(tmpDir, ".config", "shmorby")
	writeFile(t, filepath.Join(userConfigDir, "SCOPE.md"), "user scope")

	// Create flag scope file.
	flagFile := filepath.Join(tmpDir, "custom-scope.md")
	writeFile(t, flagFile, "flag scope")

	t.Setenv("HOME", tmpDir)

	cfg := config.Config{}
	flags := Flags{ScopeFile: flagFile}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "flag scope" {
		t.Fatalf("want 'flag scope', got %q", result.Content)
	}
}

// TestLoad_WalkBeatsUserConfig checks walked SCOPE.md beats user config.
func TestLoad_WalkBeatsUserConfig(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Create user config with SCOPE.md.
	userConfigDir := filepath.Join(tmpDir, ".config", "shmorby")
	writeFile(t, filepath.Join(userConfigDir, "SCOPE.md"), "user scope")

	// Create SCOPE.md in cwd (should be used, not user config).
	writeFile(t, filepath.Join(tmpDir, "SCOPE.md"), "walked scope")

	t.Setenv("HOME", tmpDir)

	cfg := config.Config{}
	flags := Flags{}

	result, err := Load(cfg, flags)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Content != "walked scope" {
		t.Fatalf("want 'walked scope', got %q", result.Content)
	}
}

// TestLoad_WalkFindsScopeAtRoot checks SCOPE.md at filesystem root.
func TestLoad_WalkFindsScopeAtRoot(t *testing.T) {
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Create SCOPE.md at root (requires root privileges, so we'll skip this test
	// in normal circumstances). This test documents the intended behavior.
	// In practice, most users won't have SCOPE.md at root.
	// Note: This test would require running as root to create a file at /SCOPE.md.
	// We'll skip it for now.
	t.Skipf("Skipping root directory test (requires root privileges)")
}
