package agent

import (
	"context"
	"strings"
	"testing"
)

func TestStatusGenerator_Nil_ReturnsEmpty(t *testing.T) {
	var g *StatusGenerator
	got := g.Generate(context.Background(), "shell", "Run command", "echo hi")
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestStatusGenerator_ShellCommand(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "shell", "Run command", "df -h")
	if got != "running df" {
		t.Errorf("want 'running df', got %q", got)
	}
}

func TestStatusGenerator_SSHCommand(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "ssh", "Run remote", "uptime")
	if got != "ssh: uptime" {
		t.Errorf("want 'ssh: uptime', got %q", got)
	}
}

func TestStatusGenerator_AWSCommand(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "aws", "AWS CLI", "aws s3 ls")
	if got != "aws s3 ls" {
		t.Errorf("want 'aws s3 ls', got %q", got)
	}
}

func TestStatusGenerator_FindPattern(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "find", "Find files",
		`{"pattern":"*.go","path":"internal/"}`)
	if got != "finding *.go in internal/" {
		t.Errorf("want 'finding *.go in internal/', got %q", got)
	}
}

func TestStatusGenerator_WebSearch(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "websearch", "Search web",
		`{"query":"golang concurrency"}`)
	if got != "searching: golang concurrency" {
		t.Errorf("want 'searching: golang concurrency', got %q", got)
	}
}

func TestStatusGenerator_WebFetch(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "webfetch", "Fetch URL",
		`{"url":"https://example.com"}`)
	if got != "fetching https://example.com" {
		t.Errorf("want 'fetching https://example.com', got %q", got)
	}
}

func TestStatusGenerator_EmptyCommand(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	got := g.Generate(context.Background(), "shell", "Run command", "")
	if got != "running shell command" {
		t.Errorf("want 'running shell command', got %q", got)
	}
}

func TestStatusGenerator_TruncatesLong(t *testing.T) {
	g := NewStatusGenerator(nil, "")
	longCmd := strings.Repeat("a", 100)
	got := g.Generate(context.Background(), "shell", "Run", longCmd)
	runes := []rune(got)
	if runes[len(runes)-1] != '\u2026' {
		t.Errorf("want trailing ellipsis, got %q", got)
	}
}
