package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shmorby/internal/ledger"
)

// Tests that --help output contains all expected sections.
func TestHelpOutput_AllSections(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	sections := []string{
		"shmorby",
		"Flags:",
		"Config file",
		"Slash commands",
		"Quick start",
	}
	for _, s := range sections {
		if !strings.Contains(output, s) {
			t.Errorf("--help missing section %q", s)
		}
	}
}

// Tests that --help lists all flags.
func TestHelpOutput_AllFlags(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	flags := []string{
		"--validate", "--provider", "--model", "--config",
		"--scope-file", "--agent", "--system-prompt-file",
		"--no-tui", "--log-level", "--version",
	}
	for _, f := range flags {
		if !strings.Contains(output, f) {
			t.Errorf("--help missing flag %q", f)
		}
	}
}

// Tests that --help lists all slash commands including /tui.
func TestHelpOutput_AllSlashCommands(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	commands := []string{
		"/help", "/quit", "/reset", "/model", "/agent",
		"/scope", "/memory", "/context", "/log", "/tui",
	}
	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("--help missing slash command %q", cmd)
		}
	}
}

// Tests that --help has correct default values.
func TestHelpOutput_Defaults(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	defaults := []string{
		`default "ollama"`,
		`default "llama3.2"`,
		`default "operate"`,
		`default "info"`,
	}
	for _, d := range defaults {
		if !strings.Contains(output, d) {
			t.Errorf("--help missing default %q", d)
		}
	}
}

// TestRootCmd_ValidateFlag_ValidConfig_ExitsZero checks that --validate with
// valid config exits successfully.
func TestRootCmd_ValidateFlag_ValidConfig_ExitsZero(t *testing.T) {
	rootCmd.InitDefaultHelpFlag()
	rootCmd.Flags().Set("help", "false")
	rootCmd.SetArgs([]string{"--validate"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected no error for valid config, got: %v", err)
	}
}

// Checks that tilde-prefixed workdir is expanded to the user's home
// directory instead of creating a literal ~/ directory in CWD.
func TestRootCmd_WorkdirTildeExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Pre-create log dir so xdg.UserDataDir()/shmorby.log doesn't fail.
	logDir := filepath.Join(home, ".local", "share", "shmorby")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}

	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "shmorby.yaml")
	workdirRel := "test-shmorby-expand-" + t.Name()
	cfgContent := fmt.Sprintf(
		"memory:\n  enabled: false\nscope:\n  workdir: \"~/%s\"\n",
		workdirRel)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	expectedDir := filepath.Join(home, workdirRel)
	literalDir := filepath.Join("~", workdirRel)

	// Redirect stdin to /dev/null so REPL exits immediately.
	oldStdin := os.Stdin
	devNull, err := os.Open("/dev/null")
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	os.Stdin = devNull

	// Save all flag vars that might be polluted by previous tests.
	saveValidate := validateFlag
	saveNoTUI := noTuiFlag
	saveProvider := providerFlag
	saveModel := modelFlag
	saveConfig := configFile
	saveAgent := agentFlag
	saveScope := scopeFile
	saveSysPrompt := systemPrompt
	saveLogLevel := logLevelFlag
	t.Cleanup(func() {
		os.Stdin = oldStdin
		devNull.Close()
		validateFlag = saveValidate
		noTuiFlag = saveNoTUI
		providerFlag = saveProvider
		modelFlag = saveModel
		configFile = saveConfig
		agentFlag = saveAgent
		scopeFile = saveScope
		systemPrompt = saveSysPrompt
		logLevelFlag = saveLogLevel
		os.RemoveAll(expectedDir)
	})

	// Reset flag vars to defaults (they may have been set by previous
	// tests and cobra does NOT reset them between runs).
	validateFlag = false
	noTuiFlag = false
	providerFlag = ""
	modelFlag = ""
	configFile = ""
	agentFlag = ""
	scopeFile = ""
	systemPrompt = ""
	logLevelFlag = "info"

	rootCmd.InitDefaultHelpFlag()
	rootCmd.Flags().Set("help", "false")
	rootCmd.SetArgs([]string{
		"--provider", "ollama", "--config", cfgPath, "--no-tui",
	})

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The literal ~/ directory should NOT exist.
	if _, err := os.Stat(literalDir); !os.IsNotExist(err) {
		t.Errorf("literal tilde dir %s should not exist", literalDir)
	}

	// The expanded home-relative directory should exist.
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expanded workdir %s should exist", expectedDir)
	}
}

// TestRootCmd_ValidateFlag_InvalidConfig_ExitsOne checks that --validate with
// invalid config returns an error.
func TestRootCmd_ValidateFlag_InvalidConfig_ExitsOne(t *testing.T) {
	rootCmd.InitDefaultHelpFlag()
	rootCmd.Flags().Set("help", "false")
	rootCmd.SetArgs([]string{"--validate", "--provider", "nonexistent"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
	if !strings.Contains(err.Error(), "config invalid:") {
		t.Errorf("want 'config invalid:' prefix in error, got: %v", err)
	}
}

// TestLedgerSetCLI_RedactionAndCaps verifies `shmorby ledger set`
// applies the same redaction and size caps as the ledger_set agent
// tool (review fix: CLI previously bypassed both).
func TestLedgerSetCLI_RedactionAndCaps(t *testing.T) {

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "shmorby")
	t.Setenv("XDG_DATA_HOME", baseDir)
	t.Setenv("LOCALAPPDATA", baseDir)

	// JSON-keyed secrets must be redacted before storage.
	err := runLedgerSet(nil, []string{
		"creds", `{"password":"hunter2","host":"web1"}`,
	})
	if err != nil {
		t.Fatalf("runLedgerSet: %v", err)
	}

	l, err := ledger.OpenAt(dataDir)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	data, ok := l.Get("creds")
	if err := l.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	if !ok {
		t.Fatal("creds section not found")
	}
	if strings.Contains(string(data), "hunter2") {
		t.Errorf("secret not redacted via CLI: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got: %s", data)
	}
	if !strings.Contains(string(data), "web1") {
		t.Errorf("non-secret data lost: %s", data)
	}

	// Oversized payloads must be rejected.
	largeData := make([]byte, ledger.MaxSectionBytes+100)
	for i := range largeData {
		largeData[i] = 'x'
	}
	err = runLedgerSet(nil, []string{"big", `"` + string(largeData) + `"`})
	if err == nil {
		t.Error("want error for oversized payload, got nil")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error should mention cap: %v", err)
	}
}
