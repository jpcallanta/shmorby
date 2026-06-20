package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Helper to restore stdoutIsTerminal after test manipulation.
func restoreTerminal() {
	stdoutIsTerminal.Store(checkTerminal(os.Stdout))
}

func TestColorize_Terminal_AddsANSI(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := colorize(ansiCyan, "hello")
	want := "\033[36mhello\033[0m"
	if got != want {
		t.Errorf("colorize = %q, want %q", got, want)
	}
}

func TestColorize_Pipe_NoANSI(t *testing.T) {
	defer SetTerminalForTest(false)()
	got := colorize(ansiCyan, "hello")
	if got != "hello" {
		t.Errorf("colorize = %q, want %q", got, "hello")
	}
}

func TestSeparator_Empty(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := Separator("")
	if !strings.HasPrefix(got, "\033[2m") {
		t.Errorf("Separator('') = %q, want dim prefix", got)
	}
	if !strings.HasSuffix(got, "\033[0m") {
		t.Errorf("Separator('') = %q, want reset suffix", got)
	}
}

func TestSeparator_WithLabel(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := Separator("agent")
	if !strings.Contains(got, "agent") {
		t.Errorf("Separator('agent') = %q, want 'agent' in output", got)
	}
}

func TestSeparator_NarrowTerminal(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := Separator("a very long label that exceeds any reasonable width")
	if !strings.Contains(got, "a very long label") {
		t.Errorf("narrow separator = %q, want label present", got)
	}
}

func TestSeparator_Pipe_NoANSI(t *testing.T) {
	defer SetTerminalForTest(false)()
	got := Separator("agent")
	if strings.Contains(got, "\033[") {
		t.Errorf("pipe separator = %q, want no ANSI", got)
	}
}

func TestToolStart_Basic(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := ToolStart("shell", "df -h")
	if !strings.Contains(got, "shell: df -h") {
		t.Errorf("ToolStart = %q, want 'shell: df -h'", got)
	}
	if !strings.Contains(got, "\033[36m") {
		t.Errorf("ToolStart = %q, want cyan", got)
	}
}

func TestToolStart_EmptyCommand(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := ToolStart("shell", "")
	if !strings.Contains(got, "shell: shell") {
		t.Errorf("ToolStart empty = %q, want 'shell: shell'", got)
	}
}

func TestToolEnd_Success(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := ToolEnd("shell", "done (exit 0)", "output text")
	if !strings.Contains(got, "✓ done (exit 0)") {
		t.Errorf("ToolEnd success = %q, want '✓ done (exit 0)'", got)
	}
	if !strings.Contains(got, "\033[32m") {
		t.Errorf("ToolEnd success = %q, want green", got)
	}
	if !strings.Contains(got, "output text") {
		t.Errorf("ToolEnd success = %q, want output", got)
	}
}

func TestToolEnd_Error(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := ToolEnd("shell", "error: connection refused", "err output")
	if !strings.Contains(got, "✗ error: connection refused") {
		t.Errorf("ToolEnd error = %q, want '✗ error: connection refused'", got)
	}
	if !strings.Contains(got, "\033[31m") {
		t.Errorf("ToolEnd error = %q, want red", got)
	}
}

func TestToolEnd_ExplicitX(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := ToolEnd("shell", "✗ something failed", "err output")
	if !strings.Contains(got, "✗ ✗ something failed") {
		t.Errorf("ToolEnd explicit ✗ = %q, want '✗ ✗ something failed'", got)
	}
}

func TestPrompt_Terminal(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := Prompt()
	if !strings.Contains(got, "shmorby>") {
		t.Errorf("Prompt = %q, want 'shmorby>'", got)
	}
	if !strings.HasPrefix(got, "\033[") {
		t.Errorf("Prompt = %q, want ANSI prefix", got)
	}
}

func TestPrompt_Pipe(t *testing.T) {
	defer SetTerminalForTest(false)()
	got := Prompt()
	if got != "shmorby> " {
		t.Errorf("Prompt = %q, want 'shmorby> '", got)
	}
}

func TestThinkingLine_Format(t *testing.T) {
	defer SetTerminalForTest(true)()
	elapsed := 1200 * time.Millisecond
	got := ThinkingLine(elapsed)
	if !strings.HasPrefix(got, "\r") {
		t.Errorf("ThinkingLine = %q, want \\r prefix", got)
	}
	if !strings.Contains(got, "thinking") {
		t.Errorf("ThinkingLine = %q, want 'thinking'", got)
	}
	if !strings.Contains(got, "1.2s") {
		t.Errorf("ThinkingLine = %q, want '1.2s'", got)
	}
}

func TestRunningLine_Format(t *testing.T) {
	defer SetTerminalForTest(true)()
	elapsed := 300 * time.Millisecond
	got := RunningLine(elapsed)
	if !strings.HasPrefix(got, "\r") {
		t.Errorf("RunningLine = %q, want \\r prefix", got)
	}
	if !strings.Contains(got, "running") {
		t.Errorf("RunningLine = %q, want 'running'", got)
	}
}

func TestSpinnerChar_Rotates(t *testing.T) {
	defer SetTerminalForTest(true)()
	c1 := spinnerChar(0)
	c2 := spinnerChar(200 * time.Millisecond)
	if c1 == c2 {
		t.Errorf("spinnerChar same for 0ms and 200ms: both %q", c1)
	}
	c3 := spinnerChar(2000 * time.Millisecond)
	if c3 == "" {
		t.Errorf("spinnerChar at 2000ms = empty")
	}
}

func TestClearLine_Format(t *testing.T) {
	got := ClearLine()
	if got != "\r\033[2K\r" {
		t.Errorf("ClearLine = %q, want '\\r\\033[2K\\r'", got)
	}
}

func TestMemoryIndicator_Terminal(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := MemoryIndicator(3)
	if !strings.Contains(got, "◎ memory: 3 entries") {
		t.Errorf("MemoryIndicator = %q, want '◎ memory: 3 entries'", got)
	}
	if !strings.HasPrefix(got, "\n") {
		t.Errorf("MemoryIndicator = %q, want leading newline", got)
	}
	if !strings.Contains(got, "\033[2") {
		t.Errorf("MemoryIndicator = %q, want dim", got)
	}
	if !strings.Contains(got, "\033[35m") {
		t.Errorf("MemoryIndicator = %q, want magenta", got)
	}
}

func TestMemoryIndicator_Pipe(t *testing.T) {
	defer SetTerminalForTest(false)()
	got := MemoryIndicator(3)
	if got != "\n◎ memory: 3 entries" {
		t.Errorf("MemoryIndicator = %q, want '\n◎ memory: 3 entries'", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("MemoryIndicator = %q, want no ANSI", got)
	}
}

func TestTerminalWidth_Pipe_ReturnsZero(t *testing.T) {
	defer SetTerminalForTest(false)()
	if w := terminalWidth(); w != 0 {
		t.Errorf("terminalWidth pipe = %d, want 0", w)
	}
}

// FormatMarkdown tests.

func TestFormatMarkdown_Bold(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("this is **bold** text")
	want := "this is \033[1mbold\033[0m text"
	if got != want {
		t.Errorf("FormatMarkdown bold = %q, want %q", got, want)
	}
}

func TestFormatMarkdown_Italic(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("this is *italic* text")
	want := "this is \033[3mitalic\033[0m text"
	if got != want {
		t.Errorf("FormatMarkdown italic = %q, want %q", got, want)
	}
}

func TestFormatMarkdown_InlineCode(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("use `code` inline")
	want := "use \033[36mcode\033[0m inline"
	if got != want {
		t.Errorf("FormatMarkdown code = %q, want %q", got, want)
	}
}

func TestFormatMarkdown_FencedCode(t *testing.T) {
	defer SetTerminalForTest(true)()
	input := "```go\nfunc main() {}\n```"
	got := FormatMarkdown(input)
	if !strings.Contains(got, "code: go") {
		t.Errorf("FormatMarkdown fenced = %q, want 'code: go'", got)
	}
	if !strings.Contains(got, "\033[36mfunc main() {}\033[0m") {
		t.Errorf("FormatMarkdown fenced = %q, want cyan body", got)
	}
}

func TestFormatMarkdown_FencedNoLang(t *testing.T) {
	defer SetTerminalForTest(true)()
	input := "```\ntext\n```"
	got := FormatMarkdown(input)
	if !strings.Contains(got, "─── code ───") {
		t.Errorf("FormatMarkdown fenced no lang = %q, want 'code' header", got)
	}
}

func TestFormatMarkdown_Header(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("# Title")
	if !strings.Contains(got, "Title") {
		t.Errorf("FormatMarkdown header = %q, want 'Title'", got)
	}
	if !strings.Contains(got, "\033[1m") {
		t.Errorf("FormatMarkdown header = %q, want bold", got)
	}
	if !strings.Contains(got, "\033[4m") {
		t.Errorf("FormatMarkdown header = %q, want underline", got)
	}
}

func TestFormatMarkdown_Blockquote(t *testing.T) {
	defer SetTerminalForTest(true)()
	input := "> quote text"
	got := FormatMarkdown(input)
	if !strings.Contains(got, "▌") {
		t.Errorf("FormatMarkdown blockquote = %q, want '▌'", got)
	}
	if !strings.Contains(got, "quote text") {
		t.Errorf("FormatMarkdown blockquote = %q, want 'quote text'", got)
	}
}

func TestFormatMarkdown_UnorderedList(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("- item")
	if !strings.Contains(got, "•") {
		t.Errorf("FormatMarkdown list = %q, want bullet", got)
	}
	if !strings.Contains(got, "item") {
		t.Errorf("FormatMarkdown list = %q, want 'item'", got)
	}
}

func TestFormatMarkdown_OrderedList(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("1. item")
	if !strings.Contains(got, "1.") {
		t.Errorf("FormatMarkdown ordered = %q, want '1.'", got)
	}
	if !strings.Contains(got, "item") {
		t.Errorf("FormatMarkdown ordered = %q, want 'item'", got)
	}
}

func TestFormatMarkdown_HorizontalRule(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("---")
	if !strings.Contains(got, "\033[2m") {
		t.Errorf("FormatMarkdown HR = %q, want dim", got)
	}
}

func TestFormatMarkdown_EmptyString(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("")
	if got != "" {
		t.Errorf("FormatMarkdown empty = %q, want ''", got)
	}
}

func TestFormatMarkdown_NoMarkdown(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("plain text")
	if got != "plain text" {
		t.Errorf("FormatMarkdown plain = %q, want 'plain text'", got)
	}
}

func TestFormatMarkdown_NestedBoldCode(t *testing.T) {
	defer SetTerminalForTest(true)()
	got := FormatMarkdown("`**nope**`")
	want := "\033[36m**nope**\033[0m"
	if got != want {
		t.Errorf("FormatMarkdown nested = %q, want %q (code wins)", got, want)
	}
}

func TestFormatMarkdown_Pipe_NoANSI(t *testing.T) {
	defer SetTerminalForTest(false)()
	got := FormatMarkdown("**bold** and `code`")
	want := "**bold** and `code`"
	if got != want {
		t.Errorf("FormatMarkdown pipe = %q, want %q", got, want)
	}
}
