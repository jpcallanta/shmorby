// Package styles defines lipgloss themes for the TUI.
package styles

import "github.com/charmbracelet/lipgloss"

// Palette holds the named color roles a Theme is built from. All
// roles are required; external theme files must define every one.
type Palette struct {
	Base      lipgloss.Color
	Mantle    lipgloss.Color
	Surface0  lipgloss.Color
	Surface1  lipgloss.Color
	Surface2  lipgloss.Color
	Overlay0  lipgloss.Color
	Overlay1  lipgloss.Color
	Overlay2  lipgloss.Color
	Subtext0  lipgloss.Color
	Subtext1  lipgloss.Color
	Text      lipgloss.Color
	Lavender  lipgloss.Color
	Blue      lipgloss.Color
	Sapphire  lipgloss.Color
	Sky       lipgloss.Color
	Teal      lipgloss.Color
	Green     lipgloss.Color
	Yellow    lipgloss.Color
	Peach     lipgloss.Color
	Red       lipgloss.Color
	Maroon    lipgloss.Color
	Pink      lipgloss.Color
	Mauve     lipgloss.Color
	Flamingo  lipgloss.Color
	Rosewater lipgloss.Color
}

// Theme holds all lipgloss styles for the TUI.
type Theme struct {
	PromptNormal lipgloss.Style
	PromptActive lipgloss.Style
	Separator    lipgloss.Style
	Spinner      lipgloss.Style
	StatusKey    lipgloss.Style
	StatusValue  lipgloss.Style
	UserInput    lipgloss.Style
	AgentReply   lipgloss.Style
	ToolRunning  lipgloss.Style
	ToolSuccess  lipgloss.Style
	ToolError    lipgloss.Style
	Error        lipgloss.Style
	PermPrompt   lipgloss.Style
	PermAllow    lipgloss.Style
	PermDeny     lipgloss.Style
	Selection    lipgloss.Style

	PopupTitle  lipgloss.Style
	PopupItem   lipgloss.Style
	PopupSelect lipgloss.Style
	PopupDesc   lipgloss.Style
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style
	TabSpin     lipgloss.Style
	TabOverflow lipgloss.Style
	ModeOperate lipgloss.Style
	ModeDiag    lipgloss.Style
	ModeChat    lipgloss.Style
	FilterBox   lipgloss.Style
}

// Mocha palette (default).
var MochaPalette = Palette{
	Base:      "#1e1e2e",
	Mantle:    "#181825",
	Surface0:  "#313244",
	Surface1:  "#45475a",
	Surface2:  "#585b70",
	Overlay0:  "#6c7086",
	Overlay1:  "#7f849c",
	Overlay2:  "#9399b2",
	Subtext0:  "#a6adc8",
	Subtext1:  "#bac2de",
	Text:      "#cdd6f4",
	Lavender:  "#b4befe",
	Blue:      "#89b4fa",
	Sapphire:  "#74c7ec",
	Sky:       "#89dceb",
	Teal:      "#94e2d5",
	Green:     "#a6e3a1",
	Yellow:    "#f9e2af",
	Peach:     "#fab387",
	Red:       "#f38ba8",
	Maroon:    "#eba0ac",
	Pink:      "#f5c2e7",
	Mauve:     "#cba6f7",
	Flamingo:  "#f2cdcd",
	Rosewater: "#f5e0dc",
}

// Latte palette (light theme).
var LattePalette = Palette{
	Base:      "#eff1f5",
	Mantle:    "#e6e9ef",
	Surface0:  "#ccd0da",
	Surface1:  "#bcc0cc",
	Surface2:  "#acb0be",
	Overlay0:  "#9ca0b0",
	Overlay1:  "#8c8fa1",
	Overlay2:  "#7c7f93",
	Subtext0:  "#6c6f85",
	Subtext1:  "#5c5f77",
	Text:      "#4c4f69",
	Lavender:  "#7287fd",
	Blue:      "#1e66f5",
	Sapphire:  "#209fb5",
	Sky:       "#04a5e5",
	Teal:      "#179299",
	Green:     "#40a02b",
	Yellow:    "#df8e1d",
	Peach:     "#fe640b",
	Red:       "#d20f39",
	Maroon:    "#e64553",
	Pink:      "#ea76cb",
	Mauve:     "#8839ef",
	Flamingo:  "#dd7878",
	Rosewater: "#dc8a78",
}

// Frappe palette.
var FrappePalette = Palette{
	Base:      "#303446",
	Mantle:    "#292c3c",
	Surface0:  "#414559",
	Surface1:  "#51576d",
	Surface2:  "#626880",
	Overlay0:  "#737994",
	Overlay1:  "#838ba7",
	Overlay2:  "#949cba",
	Subtext0:  "#a5adce",
	Subtext1:  "#b5bfe2",
	Text:      "#c6d0f5",
	Lavender:  "#babbf1",
	Blue:      "#8caaee",
	Sapphire:  "#85c1dc",
	Sky:       "#99d1db",
	Teal:      "#81c8be",
	Green:     "#a6d189",
	Yellow:    "#e5c890",
	Peach:     "#ef9f76",
	Red:       "#e78284",
	Maroon:    "#ea999c",
	Pink:      "#f4b8e4",
	Mauve:     "#ca9ee6",
	Flamingo:  "#eebebe",
	Rosewater: "#f2d5cf",
}

// Macchiato palette.
var MacchiatoPalette = Palette{
	Base:      "#24273a",
	Mantle:    "#1e2030",
	Surface0:  "#363a4f",
	Surface1:  "#494d64",
	Surface2:  "#5b6078",
	Overlay0:  "#6e738d",
	Overlay1:  "#8087a2",
	Overlay2:  "#939ab7",
	Subtext0:  "#a5adcb",
	Subtext1:  "#b8c0e0",
	Text:      "#cad3f5",
	Lavender:  "#b7bdf8",
	Blue:      "#8aadf4",
	Sapphire:  "#7dc4e4",
	Sky:       "#91d7e3",
	Teal:      "#8bd5ca",
	Green:     "#a6da95",
	Yellow:    "#eed49f",
	Peach:     "#f5a97f",
	Red:       "#ed8796",
	Maroon:    "#ee99a0",
	Pink:      "#f5bde6",
	Mauve:     "#c6a0f6",
	Flamingo:  "#f0c6c6",
	Rosewater: "#f4dbd6",
}

// Package-level color roles for styles built outside Theme.
var (
	Base      = MochaPalette.Base
	Mantle    = MochaPalette.Mantle
	Surface0  = MochaPalette.Surface0
	Surface1  = MochaPalette.Surface1
	Surface2  = MochaPalette.Surface2
	Overlay0  = MochaPalette.Overlay0
	Overlay1  = MochaPalette.Overlay1
	Overlay2  = MochaPalette.Overlay2
	Subtext0  = MochaPalette.Subtext0
	Subtext1  = MochaPalette.Subtext1
	Text      = MochaPalette.Text
	Lavender  = MochaPalette.Lavender
	Blue      = MochaPalette.Blue
	Sapphire  = MochaPalette.Sapphire
	Sky       = MochaPalette.Sky
	Teal      = MochaPalette.Teal
	Green     = MochaPalette.Green
	Yellow    = MochaPalette.Yellow
	Peach     = MochaPalette.Peach
	Red       = MochaPalette.Red
	Maroon    = MochaPalette.Maroon
	Pink      = MochaPalette.Pink
	Mauve     = MochaPalette.Mauve
	Flamingo  = MochaPalette.Flamingo
	Rosewater = MochaPalette.Rosewater
)

// newTheme builds a Theme from a palette using the role-to-style
// mapping shared by built-in and external themes.
func newTheme(p Palette) Theme {
	return Theme{
		PromptNormal: lipgloss.NewStyle().Foreground(p.Overlay2),
		PromptActive: lipgloss.NewStyle().Foreground(p.Mauve),
		Separator:    lipgloss.NewStyle().Foreground(p.Surface2),
		Spinner:      lipgloss.NewStyle().Foreground(p.Yellow),
		StatusKey:    lipgloss.NewStyle().Foreground(p.Overlay1),
		StatusValue:  lipgloss.NewStyle().Foreground(p.Text),
		UserInput:    lipgloss.NewStyle().Foreground(p.Subtext0),
		AgentReply:   lipgloss.NewStyle().Foreground(p.Text),
		ToolRunning:  lipgloss.NewStyle().Foreground(p.Sky),
		ToolSuccess:  lipgloss.NewStyle().Foreground(p.Green),
		ToolError:    lipgloss.NewStyle().Foreground(p.Red),
		Error:        lipgloss.NewStyle().Foreground(p.Red),
		PermPrompt:   lipgloss.NewStyle().Foreground(p.Peach),
		PermAllow:    lipgloss.NewStyle().Foreground(p.Green),
		PermDeny:     lipgloss.NewStyle().Foreground(p.Red),
		Selection: lipgloss.NewStyle().
			Background(p.Surface2).
			Foreground(p.Text),
		PopupTitle: lipgloss.NewStyle().
			Foreground(p.Mauve).
			Bold(true),
		PopupItem: lipgloss.NewStyle().
			Foreground(p.Text),
		PopupSelect: lipgloss.NewStyle().
			Foreground(p.Base).
			Background(p.Mauve),
		PopupDesc: lipgloss.NewStyle().
			Foreground(p.Overlay1),
		TabActive: lipgloss.NewStyle().
			Foreground(p.Base).
			Background(p.Blue).
			Bold(true),
		TabInactive: lipgloss.NewStyle().
			Foreground(p.Overlay1).
			Background(p.Surface0),
		TabSpin: lipgloss.NewStyle().
			Foreground(p.Yellow).
			Background(p.Surface0),
		TabOverflow: lipgloss.NewStyle().
			Foreground(p.Overlay1),
		ModeOperate: lipgloss.NewStyle().
			Foreground(p.Green).
			Bold(true),
		ModeDiag: lipgloss.NewStyle().
			Foreground(p.Peach).
			Bold(true),
		ModeChat: lipgloss.NewStyle().
			Foreground(p.Teal).
			Bold(true),
		FilterBox: lipgloss.NewStyle().
			Foreground(p.Text).
			Background(p.Surface0),
	}
}

// DefaultTheme returns the Catppuccin Mocha theme.
func DefaultTheme() Theme {
	return newTheme(MochaPalette)
}

// LatteTheme returns the Catppuccin Latte (light) theme.
func LatteTheme() Theme {
	return newTheme(LattePalette)
}

// FrappeTheme returns the Catppuccin Frappe theme.
func FrappeTheme() Theme {
	return newTheme(FrappePalette)
}

// MacchiatoTheme returns the Catppuccin Macchiato theme.
func MacchiatoTheme() Theme {
	return newTheme(MacchiatoPalette)
}

// MinimalTheme returns a monochrome theme with no color.
func MinimalTheme() Theme {
	return Theme{
		PromptNormal: lipgloss.NewStyle(),
		PromptActive: lipgloss.NewStyle(),
		Separator:    lipgloss.NewStyle(),
		Spinner:      lipgloss.NewStyle(),
		StatusKey:    lipgloss.NewStyle(),
		StatusValue:  lipgloss.NewStyle(),
		UserInput:    lipgloss.NewStyle(),
		AgentReply:   lipgloss.NewStyle(),
		ToolRunning:  lipgloss.NewStyle(),
		ToolSuccess:  lipgloss.NewStyle(),
		ToolError:    lipgloss.NewStyle(),
		Error:        lipgloss.NewStyle(),
		PermPrompt:   lipgloss.NewStyle(),
		PermAllow:    lipgloss.NewStyle(),
		PermDeny:     lipgloss.NewStyle(),
		Selection:    lipgloss.NewStyle().Reverse(true),
		PopupTitle:   lipgloss.NewStyle().Bold(true),
		PopupItem:    lipgloss.NewStyle(),
		PopupSelect:  lipgloss.NewStyle().Reverse(true),
		PopupDesc:    lipgloss.NewStyle(),
		TabActive:    lipgloss.NewStyle().Bold(true).Reverse(true),
		TabInactive:  lipgloss.NewStyle(),
		TabSpin:      lipgloss.NewStyle(),
		TabOverflow:  lipgloss.NewStyle(),
		ModeOperate:  lipgloss.NewStyle().Bold(true),
		ModeDiag:     lipgloss.NewStyle().Bold(true),
		ModeChat:     lipgloss.NewStyle().Bold(true),
		FilterBox:    lipgloss.NewStyle(),
	}
}

// Themes maps built-in theme names to their constructors.
var Themes = map[string]func() Theme{
	"catppuccin-mocha":     DefaultTheme,
	"catppuccin-latte":     LatteTheme,
	"catppuccin-frappe":    FrappeTheme,
	"catppuccin-macchiato": MacchiatoTheme,
	"minimal":              MinimalTheme,
}

// GetTheme returns the named theme or the default.
func GetTheme(name string) Theme {
	if fn, ok := Themes[name]; ok {
		return fn()
	}
	return DefaultTheme()
}
