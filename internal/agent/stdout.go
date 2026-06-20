package agent

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	ansiCyan    = "\033[36m"
	ansiGreen   = "\033[32m"
	ansiRed     = "\033[31m"
	ansiYellow  = "\033[33m"
	ansiMagenta = "\033[35m"
	ansiDim     = "\033[2m"
	ansiItalic  = "\033[3m"
	ansiBold    = "\033[1m"
	ansiClearLn = "\033[2K"
	ansiReset   = "\033[0m"
)

var stdoutIsTerminal atomic.Bool

func init() {
	stdoutIsTerminal.Store(checkTerminal(os.Stdout))
}

func checkTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func colorize(code, text string) string {
	if !stdoutIsTerminal.Load() {
		return text
	}
	return code + text + ansiReset
}

// Returns terminal width or 0 when not a terminal.
func terminalWidth() int {
	if !stdoutIsTerminal.Load() {
		return 0
	}
	_, cols, err := getTermSize(os.Stdout.Fd())
	if err != nil {
		return 80
	}
	return cols
}

func getTermSize(fd uintptr) (rows, cols int, err error) {
	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL, fd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.Row), int(ws.Col), nil
}

// Full-width dim separator with optional centred label.
//
//	──────────────────────────────────────
//	──── agent ───────────────────────────
func Separator(label string) string {
	w := terminalWidth()
	if w == 0 {
		w = 40
	}
	if label == "" {
		return colorize(ansiDim, strings.Repeat("─", w))
	}
	mid := "── " + label + " ──"
	n := w - len(mid)
	if n < 4 {
		return mid
	}
	left := n / 2
	right := n - left
	return colorize(
		ansiDim,
		strings.Repeat("─", left)+mid+strings.Repeat("─", right),
	)
}

// Tool header for tool invocation.
//
//	───── shell: df -h ─────
func ToolStart(name, command string) string {
	if command == "" {
		command = name
	}
	label := name + ": " + command
	return Separator("") + "\n" + colorize(ansiCyan, label)
}

// Tool completion status and output.
//
//	✓ done (exit 0)
func ToolEnd(name, status, output string) string {
	var b strings.Builder
	if strings.HasPrefix(status, "error") || strings.HasPrefix(status, "✗") {
		b.WriteString(colorize(ansiRed, "✗ "+status))
	} else {
		b.WriteString(colorize(ansiGreen, "✓ "+status))
	}
	b.WriteString("\n")
	b.WriteString(output)
	return b.String()
}

// Input prompt.
//
//	shmorby>
func Prompt() string {
	return colorize(ansiBold+ansiCyan, "shmorby> ")
}

// One-line thinking spinner (use \r to update in place).
//
//	⟳ thinking… (1.2s)
func ThinkingLine(elapsed time.Duration) string {
	s := spinnerChar(elapsed)
	return fmt.Sprintf(
		"\r%s %s… (%v)",
		colorize(ansiCyan, s),
		colorize(ansiItalic+ansiDim, "thinking"),
		elapsed.Round(100*time.Millisecond),
	)
}

// One-line running spinner (use \r to update in place).
//
//	⟳ running… (0.3s)
func RunningLine(elapsed time.Duration) string {
	s := spinnerChar(elapsed)
	return fmt.Sprintf(
		"\r%s %s… (%v)",
		colorize(ansiCyan, s),
		colorize(ansiBold, "running"),
		elapsed.Round(100*time.Millisecond),
	)
}

// Rotating braille spinner character based on elapsed time.
func spinnerChar(elapsed time.Duration) string {
	chars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return chars[int(elapsed.Milliseconds()/200)%len(chars)]
}

// ANSI sequence to erase the current line.
func ClearLine() string {
	return "\r" + ansiClearLn + "\r"
}

// Lightweight markdown-to-ANSI renderer.
// Supports: **bold**, `inline code`, ```fenced code```, # headers,
// > blockquotes, - lists, 1. ordered lists, --- horizontal rules.
func FormatMarkdown(text string) string {
	if !stdoutIsTerminal.Load() {
		return text
	}

	lines := strings.Split(text, "\n")
	var out strings.Builder
	inCodeBlock := false
	codeLang := ""

	for _, line := range lines {
		// Fenced code block start/end.
		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimSpace(line[3:])
				header := "─── code"
				if codeLang != "" {
					header += ": " + codeLang
				}
				header += " " + strings.Repeat("─", 4) + "───"
				out.WriteString(colorize(ansiDim, header))
				out.WriteString("\n")
			} else {
				inCodeBlock = false
				footer := strings.Repeat("─", 80)
				out.WriteString(colorize(ansiDim, footer))
				out.WriteString("\n")
			}
			continue
		}

		if inCodeBlock {
			out.WriteString(colorize(ansiCyan, line))
			out.WriteString("\n")
			continue
		}

		rendered := renderLine(line)
		out.WriteString(rendered)
		out.WriteString("\n")
	}

	result := out.String()
	if result != "" && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	return result
}

// Renders a single non-code-block line with markdown formatting.
func renderLine(line string) string {
	trimmed := strings.TrimSpace(line)

	// Blank line.
	if trimmed == "" {
		return ""
	}

	// Header: # Title
	if trimmed[0] == '#' {
		content := strings.TrimSpace(trimmed[1:])
		return colorize(ansiBold+ansiUnderline, content)
	}

	// Blockquote: > text
	if strings.HasPrefix(trimmed, ">") {
		content := strings.TrimSpace(trimmed[1:])
		return colorize(ansiDim, "▌ "+content)
	}

	// Horizontal rule: --- (three or more dashes, rest optional spaces)
	isHR := true
	for _, ch := range trimmed {
		if ch != '-' {
			isHR = false
			break
		}
	}
	if isHR && len(trimmed) >= 3 {
		return colorize(ansiDim, strings.Repeat("─", 40))
	}

	// Unordered list: - item
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		content := strings.TrimSpace(trimmed[2:])
		return "• " + applyInline(content)
	}

	// Ordered list: 1. item
	if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
		idx := 1
		for idx < len(trimmed) && trimmed[idx] >= '0' && trimmed[idx] <= '9' {
			idx++
		}
		if idx < len(trimmed) && trimmed[idx] == '.' {
			content := strings.TrimSpace(trimmed[idx+1:])
			return line[:idx+1] + " " + applyInline(content)
		}
	}

	return applyInline(line)
}

// Applies inline formatting: code, bold, italic.
func applyInline(text string) string {
	// Process inline code first (highest priority).
	parts := splitByBackticks(text)
	var result strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			result.WriteString(colorize(ansiCyan, part))
		} else {
			result.WriteString(applyBoldItalic(part))
		}
	}
	return result.String()
}

// Applies bold and italic formatting (only outside code spans).
func applyBoldItalic(text string) string {
	// Bold: **text**
	text = replacePairs(text, "**", ansiBold)
	// Italic: *text*
	text = replacePairs(text, "*", ansiItalic)
	return text
}

// Splits text by backtick pairs for inline code detection.
func splitByBackticks(text string) []string {
	var parts []string
	var buf strings.Builder
	inCode := false
	i := 0
	for i < len(text) {
		if text[i] == '`' {
			parts = append(parts, buf.String())
			buf.Reset()
			inCode = !inCode
			i++
		} else {
			buf.WriteByte(text[i])
			i++
		}
	}
	parts = append(parts, buf.String())
	return parts
}

// Replaces paired delimiters with ANSI codes.
func replacePairs(text, delim, code string) string {
	var result strings.Builder
	i := 0
	inSpan := false
	for i < len(text) {
		idx := indexOf(text, delim, i)
		if idx == -1 {
			result.WriteString(text[i:])
			break
		}
		result.WriteString(text[i:idx])
		if !inSpan {
			result.WriteString(code)
		} else {
			result.WriteString(ansiReset)
		}
		inSpan = !inSpan
		i = idx + len(delim)
	}
	return result.String()
}

func indexOf(s, substr string, start int) int {
	if start >= len(s) {
		return -1
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Memory retrieval indicator.
//
//	◎ memory: 3 entries
func MemoryIndicator(n int) string {
	return "\n" + colorize(
		ansiDim+ansiMagenta,
		fmt.Sprintf("◎ memory: %d entries", n),
	)
}

// Define after other consts to avoid circular reference.
const ansiUnderline = "\033[4m"
