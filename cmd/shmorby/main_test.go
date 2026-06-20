package main

import (
	"bytes"
	"strings"
	"testing"
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
