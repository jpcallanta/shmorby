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
		"Environment variables:",
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
		"--provider", "--model", "--config", "--scope-file",
		"--agent", "--system-prompt-file", "--no-tui",
		"--log-level", "--version",
	}
	for _, f := range flags {
		if !strings.Contains(output, f) {
			t.Errorf("--help missing flag %q", f)
		}
	}
}

// Tests that --help lists all env vars.
func TestHelpOutput_AllEnvVars(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	envVars := []string{
		"OPENROUTER_API_KEY", "OPENCODE_ZEN_API_KEY",
		"OPENAI_API_KEY", "OLLAMA_BASE_URL",
		"SHMORBY_PROVIDER", "SHMORBY_MODEL",
	}
	for _, v := range envVars {
		if !strings.Contains(output, v) {
			t.Errorf("--help missing env var %q", v)
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
