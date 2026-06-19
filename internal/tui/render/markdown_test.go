package render

import (
	"regexp"
	"strings"
	"testing"
)

// stripANSI removes ANSI escape sequences from s for test assertions.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestRenderMarkdown_Empty(t *testing.T) {
	out := RenderMarkdown("", 80)
	if out != "" {
		t.Errorf("want empty, got %q", out)
	}
}

func TestRenderMarkdown_PlainText(t *testing.T) {
	out := stripANSI(RenderMarkdown("hello world", 80))
	if !strings.Contains(out, "hello world") {
		t.Errorf("output missing text, got %q", out)
	}
}

func TestRenderMarkdown_Headers(t *testing.T) {
	out := stripANSI(RenderMarkdown("# Header", 80))
	if !strings.Contains(out, "Header") {
		t.Errorf("output missing header text, got %q", out)
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	out := stripANSI(RenderMarkdown(md, 80))
	if !strings.Contains(out, "hello") {
		t.Errorf("output missing code content, got %q", out)
	}
}

func TestRenderMarkdown_List(t *testing.T) {
	md := "- item 1\n- item 2"
	out := stripANSI(RenderMarkdown(md, 80))
	if !strings.Contains(out, "item 1") {
		t.Errorf("output missing list item, got %q", out)
	}
	if !strings.Contains(out, "item 2") {
		t.Errorf("output missing second list item, got %q", out)
	}
}
