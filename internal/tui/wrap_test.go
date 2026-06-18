package tui

import "testing"

func TestWrapText_WordBoundaries(t *testing.T) {
	text := "hello world foo bar"
	want := "hello world\nfoo bar"
	got := wrapText(text, 11)
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestWrapText_HardBreaksLongWord(t *testing.T) {
	text := "superlongword"
	want := "superl\nongwor\nd"
	got := wrapText(text, 6)
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestWrapText_PreservesExplicitNewlines(t *testing.T) {
	text := "hello\nworld foo bar baz"
	want := "hello\nworld foo\nbar baz"
	got := wrapText(text, 10)
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestWrapText_EmptyInput(t *testing.T) {
	got := wrapText("", 10)
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestWrapText_WidthZero(t *testing.T) {
	text := "hello world"
	got := wrapText(text, 0)
	if got != text {
		t.Errorf("want %q, got %q", text, got)
	}
}

func TestWrapText_WidthNegative(t *testing.T) {
	text := "hello world"
	got := wrapText(text, -1)
	if got != text {
		t.Errorf("want %q, got %q", text, got)
	}
}

func TestWrapText_ShorterThanWidth(t *testing.T) {
	text := "hello"
	got := wrapText(text, 80)
	if got != text {
		t.Errorf("want %q, got %q", text, got)
	}
}

func TestWrapText_MultipleExplicitNewlines(t *testing.T) {
	text := "a\n\nb"
	got := wrapText(text, 80)
	if got != text {
		t.Errorf("want %q, got %q", text, got)
	}
}

func TestWrapText_ExactWidthNoWrap(t *testing.T) {
	text := "1234567890"
	got := wrapText(text, 10)
	if got != text {
		t.Errorf("want %q, got %q", text, got)
	}
}

func TestWrapText_LeadingNewline(t *testing.T) {
	text := "\nhello"
	got := wrapText(text, 3)
	want := "\nhel\nlo"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}
