package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSystemPrompt_Operate_ContainsEmbed checks operate mode contains embedded
// prompt.
func TestSystemPrompt_Operate_ContainsEmbed(t *testing.T) {
	prompt, err := SystemPrompt("operate", "", "")
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	if !strings.Contains(prompt, "senior systems engineer") {
		t.Fatalf("want prompt to contain 'senior systems engineer'")
	}
	if !strings.Contains(prompt, "observe") {
		t.Fatalf("want prompt to contain 'observe'")
	}
	if !strings.Contains(prompt, "plan") {
		t.Fatalf("want prompt to contain 'plan'")
	}
}

// TestSystemPrompt_Diagnose_ContainsEmbed checks diagnose mode contains embedded
// prompt.
func TestSystemPrompt_Diagnose_ContainsEmbed(t *testing.T) {
	prompt, err := SystemPrompt("diagnose", "", "")
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	if !strings.Contains(prompt, "diagnose mode") {
		t.Fatalf("want prompt to contain 'diagnose mode'")
	}
	if !strings.Contains(prompt, "DO NOT execute") {
		t.Fatalf("want prompt to contain constraint warning")
	}
}

// TestSystemPrompt_WithScope_AppendsScope checks scope content is appended.
func TestSystemPrompt_WithScope_AppendsScope(t *testing.T) {
	prompt, err := SystemPrompt("operate", "SCOPE CONTENT HERE", "")
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	if !strings.Contains(prompt, "SCOPE CONTENT HERE") {
		t.Fatalf("want prompt to contain scope content")
	}
	if !strings.Contains(prompt, "Scope Context") {
		t.Fatalf("want prompt to contain 'Scope Context' header")
	}
}

// TestSystemPrompt_Override_ReplacesEmbed checks override replaces embed body.
func TestSystemPrompt_Override_ReplacesEmbed(t *testing.T) {
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom.txt")
	if err := os.WriteFile(customFile, []byte("CUSTOM PROMPT BODY"), 0o600); err != nil {
		t.Fatalf("write custom file: %v", err)
	}

	prompt, err := SystemPrompt("operate", "", customFile)
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	if prompt != "CUSTOM PROMPT BODY" {
		t.Fatalf("want prompt to be exactly override content, got:\n%s", prompt)
	}
}

// TestSystemPrompt_OverrideKeepsScope checks override + scope combo works.
func TestSystemPrompt_OverrideKeepsScope(t *testing.T) {
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom.txt")
	if err := os.WriteFile(customFile, []byte("CUSTOM BODY"), 0o600); err != nil {
		t.Fatalf("write custom file: %v", err)
	}

	scope := "MY SCOPE"
	prompt, err := SystemPrompt("operate", scope, customFile)
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	if !strings.Contains(prompt, "CUSTOM BODY") {
		t.Fatalf("want prompt to contain custom body")
	}
	if !strings.Contains(prompt, scope) {
		t.Fatalf("want prompt to contain scope (override replaces body only)")
	}
	if !strings.Contains(prompt, "Scope Context") {
		t.Fatalf("want prompt to contain scope appendix header")
	}
}

// TestSystemPrompt_OperateWithScope_EmbedAndScope checks operate + scope.
func TestSystemPrompt_OperateWithScope_EmbedAndScope(t *testing.T) {
	scope := "production environment"
	prompt, err := SystemPrompt("operate", scope, "")
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	// Should contain embedded content.
	if !strings.Contains(prompt, "senior systems engineer") {
		t.Fatalf("want embedded content")
	}
	// Should contain scope.
	if !strings.Contains(prompt, scope) {
		t.Fatalf("want scope content")
	}
	// Should have Scope Context header.
	if !strings.Contains(prompt, "## Scope Context") {
		t.Fatalf("want Scope Context header")
	}
}

// TestSystemPrompt_UnknownMode_ReturnsError checks unknown mode returns error.
func TestSystemPrompt_UnknownMode_ReturnsError(t *testing.T) {
	_, err := SystemPrompt("unknown", "", "")
	if err == nil {
		t.Fatal("expected error for unknown mode, got nil")
	}
	if !strings.Contains(err.Error(), "invalid agent mode") {
		t.Fatalf("want error about invalid agent mode, got %v", err)
	}
}

// TestSystemPrompt_DiagnoseWithScope_ContainsBoth checks diagnose + scope.
func TestSystemPrompt_DiagnoseWithScope_ContainsBoth(t *testing.T) {
	scope := "test environment"
	prompt, err := SystemPrompt("diagnose", scope, "")
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	// Should contain diagnose content.
	if !strings.Contains(prompt, "inspection") {
		t.Fatalf("want diagnose content")
	}
	// Should contain scope.
	if !strings.Contains(prompt, scope) {
		t.Fatalf("want scope content")
	}
}

// TestSystemPrompt_EmptyScope_NoScopeSection checks empty scope omits section.
func TestSystemPrompt_EmptyScope_NoScopeSection(t *testing.T) {
	prompt, err := SystemPrompt("operate", "", "")
	if err != nil {
		t.Fatalf("SystemPrompt: %v", err)
	}

	// Should NOT contain Scope Context when scope is empty.
	if strings.Contains(prompt, "## Scope Context") {
		t.Fatalf("want no Scope Context section when scope is empty")
	}
}

// TestSystemPrompt_OverrideFileMissingError checks error when override file missing.
func TestSystemPrompt_OverrideFileMissingError(t *testing.T) {
	_, err := SystemPrompt("operate", "", "/nonexistent/prompt.txt")
	if err == nil {
		t.Fatal("expected error for missing override file, got nil")
	}
}
