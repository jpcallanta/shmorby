package styles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// writeThemeFile writes content to a temp theme file and returns its path.
func writeThemeFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "themes.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	return path
}

// nordPalette is a complete example palette for tests.
const nordPalette = `
    base: "#2e3440"
    mantle: "#292e39"
    surface0: "#3b4252"
    surface1: "#434c5e"
    surface2: "#4c566a"
    overlay0: "#616e88"
    overlay1: "#7b88a1"
    overlay2: "#9199ae"
    subtext0: "#a9b1c5"
    subtext1: "#bcc4cf"
    text: "#eceff4"
    lavender: "#b48ead"
    blue: "#81a1c1"
    sapphire: "#88c0d0"
    sky: "#8fbcbb"
    teal: "#88c0d0"
    green: "#a3be8c"
    yellow: "#ebcb8b"
    peach: "#d08770"
    red: "#bf616a"
    maroon: "#c77d84"
    pink: "#b48ead"
    mauve: "#81a1c1"
    flamingo: "#bf616a"
    rosewater: "#d8dee9"
`

// themeYAML returns a complete theme definition for the given name
// and base color (all other roles reuse the nord fixture values).
func themeYAML(name, base string) string {
	return "  " + name + ":" + strings.Replace(
		nordPalette, `"#2e3440"`, `"`+base+`"`, 1,
	)
}

func TestLoadThemeFile_Valid(t *testing.T) {
	path := writeThemeFile(t, "themes:\n"+
		themeYAML("nord", "#292e39")+
		themeYAML("solarized", "#002b36"))

	if err := LoadThemeFile(path); err != nil {
		t.Fatalf("LoadThemeFile: %v", err)
	}
	t.Cleanup(func() { delete(Themes, "nord"); delete(Themes, "solarized") })

	theme := GetTheme("nord")
	fg := theme.PromptActive.GetForeground()
	if fg != lipgloss.Color("#81a1c1") {
		t.Errorf("want #81a1c1, got %v", fg)
	}
	if _, ok := Themes["solarized"]; !ok {
		t.Error("solarized not registered")
	}
}

func TestLoadThemeFile_ExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}

	err = LoadThemeFile("~/nonexistent-themes.yaml")
	if err == nil {
		t.Fatal("want error for missing file")
	}
	if !strings.Contains(err.Error(), home) {
		t.Errorf("error should use expanded home path, got: %v", err)
	}
}

// Tests that a file with one invalid theme registers nothing.
func TestLoadThemeFile_AllOrNothing(t *testing.T) {
	path := writeThemeFile(t, `themes:
  good:`+nordPalette+`
  bad:
    base: "#1e1e2e"
`)

	err := LoadThemeFile(path)
	if err == nil {
		t.Fatal("want error for invalid theme in file")
	}
	if _, ok := Themes["good"]; ok {
		t.Error("valid theme must not register when file has errors")
	}
}

func TestLoadThemeFile_MissingFile(t *testing.T) {
	err := LoadThemeFile("/nonexistent/themes.yaml")
	if err == nil {
		t.Fatal("want error for missing file")
	}
	if !strings.Contains(err.Error(), "read theme file") {
		t.Errorf("error should mention read failure, got: %v", err)
	}
}

func TestLoadThemeFile_MissingPaletteKeys(t *testing.T) {
	path := writeThemeFile(t, `themes:
  partial:
    base: "#1e1e2e"
    text: "#cdd6f4"
`)

	err := LoadThemeFile(path)
	if err == nil {
		t.Fatal("want error for incomplete palette")
	}
	for _, want := range []string{"partial", "mauve", "rosewater"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q, got: %v", want, err)
		}
	}
}

func TestLoadThemeFile_InvalidHex(t *testing.T) {
	path := writeThemeFile(t, `themes:
  badhex:
`+strings.ReplaceAll(nordPalette, "#81a1c1", "blue"))

	err := LoadThemeFile(path)
	if err == nil {
		t.Fatal("want error for invalid hex")
	}
	if !strings.Contains(err.Error(), "blue") {
		t.Errorf("error should name the bad color, got: %v", err)
	}
}

// Tests that short hex forms (#rgb) are rejected.
func TestLoadThemeFile_ShortHexRejected(t *testing.T) {
	path := writeThemeFile(t, `themes:
  shorthex:
`+strings.ReplaceAll(nordPalette, "#81a1c1", "#81a"))

	if err := LoadThemeFile(path); err == nil {
		t.Fatal("want error for short hex")
	}
}

func TestLoadThemeFile_InvalidYAML(t *testing.T) {
	path := writeThemeFile(t, "{{invalid")

	if err := LoadThemeFile(path); err == nil {
		t.Fatal("want error for invalid YAML")
	}
}

func TestLoadThemeFile_NoThemes(t *testing.T) {
	path := writeThemeFile(t, "themes: {}\n")

	err := LoadThemeFile(path)
	if err == nil {
		t.Fatal("want error for empty themes map")
	}
	if !strings.Contains(err.Error(), "no themes defined") {
		t.Errorf("error should mention no themes, got: %v", err)
	}
}
