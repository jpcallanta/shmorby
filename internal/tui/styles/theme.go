// Package styles defines lipgloss themes for the TUI.
package styles

import "github.com/charmbracelet/lipgloss"

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
	ModeOperate lipgloss.Style
	ModeDiag    lipgloss.Style
	WhichKey    lipgloss.Style
	FilterBox   lipgloss.Style
}

// Catppuccin Mocha palette.
var (
	Base      = lipgloss.Color("#1e1e2e")
	Mantle    = lipgloss.Color("#181825")
	Surface0  = lipgloss.Color("#313244")
	Surface1  = lipgloss.Color("#45475a")
	Surface2  = lipgloss.Color("#585b70")
	Overlay0  = lipgloss.Color("#6c7086")
	Overlay1  = lipgloss.Color("#7f849c")
	Overlay2  = lipgloss.Color("#9399b2")
	Subtext0  = lipgloss.Color("#a6adc8")
	Subtext1  = lipgloss.Color("#bac2de")
	Text      = lipgloss.Color("#cdd6f4")
	Lavender  = lipgloss.Color("#b4befe")
	Blue      = lipgloss.Color("#89b4fa")
	Sapphire  = lipgloss.Color("#74c7ec")
	Sky       = lipgloss.Color("#89dceb")
	Teal      = lipgloss.Color("#94e2d5")
	Green     = lipgloss.Color("#a6e3a1")
	Yellow    = lipgloss.Color("#f9e2af")
	Peach     = lipgloss.Color("#fab387")
	Red       = lipgloss.Color("#f38ba8")
	Maroon    = lipgloss.Color("#eba0ac")
	Pink      = lipgloss.Color("#f5c2e7")
	Mauve     = lipgloss.Color("#cba6f7")
	Flamingo  = lipgloss.Color("#f2cdcd")
	Rosewater = lipgloss.Color("#f5e0dc")
)

// Latte palette (light theme).
var (
	LatteBase      = lipgloss.Color("#eff1f5")
	LatteMantle    = lipgloss.Color("#e6e9ef")
	LatteSurface0  = lipgloss.Color("#ccd0da")
	LatteSurface1  = lipgloss.Color("#bcc0cc")
	LatteSurface2  = lipgloss.Color("#acb0be")
	LatteOverlay0  = lipgloss.Color("#9ca0b0")
	LatteOverlay1  = lipgloss.Color("#8c8fa1")
	LatteOverlay2  = lipgloss.Color("#7c7f93")
	LatteSubtext0  = lipgloss.Color("#6c6f85")
	LatteSubtext1  = lipgloss.Color("#5c5f77")
	LatteText      = lipgloss.Color("#4c4f69")
	LatteLavender  = lipgloss.Color("#7287fd")
	LatteBlue      = lipgloss.Color("#1e66f5")
	LatteSapphire  = lipgloss.Color("#209fb5")
	LatteSky       = lipgloss.Color("#04a5e5")
	LatteTeal      = lipgloss.Color("#179299")
	LatteGreen     = lipgloss.Color("#40a02b")
	LatteYellow    = lipgloss.Color("#df8e1d")
	LattePeach     = lipgloss.Color("#fe640b")
	LatteRed       = lipgloss.Color("#d20f39")
	LatteMaroon    = lipgloss.Color("#e64553")
	LattePink      = lipgloss.Color("#ea76cb")
	LatteMauve     = lipgloss.Color("#8839ef")
	LatteFlamingo  = lipgloss.Color("#dd7878")
	LatteRosewater = lipgloss.Color("#dc8a78")
)

// Frappe palette.
var (
	FrappeBase      = lipgloss.Color("#303446")
	FrappeMantle    = lipgloss.Color("#292c3c")
	FrappeSurface0  = lipgloss.Color("#414559")
	FrappeSurface1  = lipgloss.Color("#51576d")
	FrappeSurface2  = lipgloss.Color("#626880")
	FrappeOverlay0  = lipgloss.Color("#737994")
	FrappeOverlay1  = lipgloss.Color("#838ba7")
	FrappeOverlay2  = lipgloss.Color("#949cba")
	FrappeSubtext0  = lipgloss.Color("#a5adce")
	FrappeSubtext1  = lipgloss.Color("#b5bfe2")
	FrappeText      = lipgloss.Color("#c6d0f5")
	FrappeLavender  = lipgloss.Color("#babbf1")
	FrappeBlue      = lipgloss.Color("#8caaee")
	FrappeSapphire  = lipgloss.Color("#85c1dc")
	FrappeSky       = lipgloss.Color("#99d1db")
	FrappeTeal      = lipgloss.Color("#81c8be")
	FrappeGreen     = lipgloss.Color("#a6d189")
	FrappeYellow    = lipgloss.Color("#e5c890")
	FrappePeach     = lipgloss.Color("#ef9f76")
	FrappeRed       = lipgloss.Color("#e78284")
	FrappeMaroon    = lipgloss.Color("#ea999c")
	FrappePink      = lipgloss.Color("#f4b8e4")
	FrappeMauve     = lipgloss.Color("#ca9ee6")
	FrappeFlamingo  = lipgloss.Color("#eebebe")
	FrappeRosewater = lipgloss.Color("#f2d5cf")
)

// Macchiato palette.
var (
	MacchiatoBase      = lipgloss.Color("#24273a")
	MacchiatoMantle    = lipgloss.Color("#1e2030")
	MacchiatoSurface0  = lipgloss.Color("#363a4f")
	MacchiatoSurface1  = lipgloss.Color("#494d64")
	MacchiatoSurface2  = lipgloss.Color("#5b6078")
	MacchiatoOverlay0  = lipgloss.Color("#6e738d")
	MacchiatoOverlay1  = lipgloss.Color("#8087a2")
	MacchiatoOverlay2  = lipgloss.Color("#939ab7")
	MacchiatoSubtext0  = lipgloss.Color("#a5adcb")
	MacchiatoSubtext1  = lipgloss.Color("#b8c0e0")
	MacchiatoText      = lipgloss.Color("#cad3f5")
	MacchiatoLavender  = lipgloss.Color("#b7bdf8")
	MacchiatoBlue      = lipgloss.Color("#8aadf4")
	MacchiatoSapphire  = lipgloss.Color("#7dc4e4")
	MacchiatoSky       = lipgloss.Color("#91d7e3")
	MacchiatoTeal      = lipgloss.Color("#8bd5ca")
	MacchiatoGreen     = lipgloss.Color("#a6da95")
	MacchiatoYellow    = lipgloss.Color("#eed49f")
	MacchiatoPeach     = lipgloss.Color("#f5a97f")
	MacchiatoRed       = lipgloss.Color("#ed8796")
	MacchiatoMaroon    = lipgloss.Color("#ee99a0")
	MacchiatoPink      = lipgloss.Color("#f5bde6")
	MacchiatoMauve     = lipgloss.Color("#c6a0f6")
	MacchiatoFlamingo  = lipgloss.Color("#f0c6c6")
	MacchiatoRosewater = lipgloss.Color("#f4dbd6")
)

// DefaultTheme returns the Catppuccin Mocha theme.
func DefaultTheme() Theme {
	return Theme{
		PromptNormal: lipgloss.NewStyle().Foreground(Overlay2),
		PromptActive: lipgloss.NewStyle().Foreground(Mauve),
		Separator:    lipgloss.NewStyle().Foreground(Surface2),
		Spinner:      lipgloss.NewStyle().Foreground(Yellow),
		StatusKey:    lipgloss.NewStyle().Foreground(Overlay1),
		StatusValue:  lipgloss.NewStyle().Foreground(Text),
		UserInput:    lipgloss.NewStyle().Foreground(Subtext0),
		AgentReply:   lipgloss.NewStyle().Foreground(Text),
		ToolRunning:  lipgloss.NewStyle().Foreground(Sky),
		ToolSuccess:  lipgloss.NewStyle().Foreground(Green),
		ToolError:    lipgloss.NewStyle().Foreground(Red),
		Error:        lipgloss.NewStyle().Foreground(Red),
		PermPrompt:   lipgloss.NewStyle().Foreground(Peach),
		PermAllow:    lipgloss.NewStyle().Foreground(Green),
		PermDeny:     lipgloss.NewStyle().Foreground(Red),
		Selection: lipgloss.NewStyle().
			Background(Surface2).
			Foreground(Text),
		PopupTitle: lipgloss.NewStyle().
			Foreground(Mauve).
			Bold(true),
		PopupItem: lipgloss.NewStyle().
			Foreground(Text),
		PopupSelect: lipgloss.NewStyle().
			Foreground(Base).
			Background(Mauve),
		PopupDesc: lipgloss.NewStyle().
			Foreground(Overlay1),
		TabActive: lipgloss.NewStyle().
			Foreground(Base).
			Background(Blue).
			Bold(true),
		TabInactive: lipgloss.NewStyle().
			Foreground(Overlay1).
			Background(Surface0),
		TabSpin: lipgloss.NewStyle().
			Foreground(Yellow).
			Background(Surface0),
		ModeOperate: lipgloss.NewStyle().
			Foreground(Green).
			Bold(true),
		ModeDiag: lipgloss.NewStyle().
			Foreground(Peach).
			Bold(true),
		WhichKey: lipgloss.NewStyle().
			Foreground(Overlay2).
			Background(Surface0),
		FilterBox: lipgloss.NewStyle().
			Foreground(Text).
			Background(Surface0),
	}
}

// LatteTheme returns the Catppuccin Latte (light) theme.
func LatteTheme() Theme {
	return Theme{
		PromptNormal: lipgloss.NewStyle().Foreground(LatteOverlay2),
		PromptActive: lipgloss.NewStyle().Foreground(LatteMauve),
		Separator:    lipgloss.NewStyle().Foreground(LatteSurface2),
		Spinner:      lipgloss.NewStyle().Foreground(LatteYellow),
		StatusKey:    lipgloss.NewStyle().Foreground(LatteOverlay1),
		StatusValue:  lipgloss.NewStyle().Foreground(LatteText),
		UserInput:    lipgloss.NewStyle().Foreground(LatteSubtext0),
		AgentReply:   lipgloss.NewStyle().Foreground(LatteText),
		ToolRunning:  lipgloss.NewStyle().Foreground(LatteSky),
		ToolSuccess:  lipgloss.NewStyle().Foreground(LatteGreen),
		ToolError:    lipgloss.NewStyle().Foreground(LatteRed),
		Error:        lipgloss.NewStyle().Foreground(LatteRed),
		PermPrompt:   lipgloss.NewStyle().Foreground(LattePeach),
		PermAllow:    lipgloss.NewStyle().Foreground(LatteGreen),
		PermDeny:     lipgloss.NewStyle().Foreground(LatteRed),
		Selection: lipgloss.NewStyle().
			Background(LatteSurface2).
			Foreground(LatteText),
		PopupTitle: lipgloss.NewStyle().
			Foreground(LatteMauve).
			Bold(true),
		PopupItem: lipgloss.NewStyle().
			Foreground(LatteText),
		PopupSelect: lipgloss.NewStyle().
			Foreground(LatteBase).
			Background(LatteMauve),
		PopupDesc: lipgloss.NewStyle().
			Foreground(LatteOverlay1),
		TabActive: lipgloss.NewStyle().
			Foreground(LatteBase).
			Background(LatteBlue).
			Bold(true),
		TabInactive: lipgloss.NewStyle().
			Foreground(LatteOverlay1).
			Background(LatteSurface0),
		TabSpin: lipgloss.NewStyle().
			Foreground(LatteYellow).
			Background(LatteSurface0),
		ModeOperate: lipgloss.NewStyle().
			Foreground(LatteGreen).
			Bold(true),
		ModeDiag: lipgloss.NewStyle().
			Foreground(LattePeach).
			Bold(true),
		WhichKey: lipgloss.NewStyle().
			Foreground(LatteOverlay2).
			Background(LatteSurface0),
		FilterBox: lipgloss.NewStyle().
			Foreground(LatteText).
			Background(LatteSurface0),
	}
}

// FrappeTheme returns the Catppuccin Frappe theme.
func FrappeTheme() Theme {
	return Theme{
		PromptNormal: lipgloss.NewStyle().Foreground(FrappeOverlay2),
		PromptActive: lipgloss.NewStyle().Foreground(FrappeMauve),
		Separator:    lipgloss.NewStyle().Foreground(FrappeSurface2),
		Spinner:      lipgloss.NewStyle().Foreground(FrappeYellow),
		StatusKey:    lipgloss.NewStyle().Foreground(FrappeOverlay1),
		StatusValue:  lipgloss.NewStyle().Foreground(FrappeText),
		UserInput:    lipgloss.NewStyle().Foreground(FrappeSubtext0),
		AgentReply:   lipgloss.NewStyle().Foreground(FrappeText),
		ToolRunning:  lipgloss.NewStyle().Foreground(FrappeSky),
		ToolSuccess:  lipgloss.NewStyle().Foreground(FrappeGreen),
		ToolError:    lipgloss.NewStyle().Foreground(FrappeRed),
		Error:        lipgloss.NewStyle().Foreground(FrappeRed),
		PermPrompt:   lipgloss.NewStyle().Foreground(FrappePeach),
		PermAllow:    lipgloss.NewStyle().Foreground(FrappeGreen),
		PermDeny:     lipgloss.NewStyle().Foreground(FrappeRed),
		Selection: lipgloss.NewStyle().
			Background(FrappeSurface2).
			Foreground(FrappeText),
		PopupTitle: lipgloss.NewStyle().
			Foreground(FrappeMauve).
			Bold(true),
		PopupItem: lipgloss.NewStyle().
			Foreground(FrappeText),
		PopupSelect: lipgloss.NewStyle().
			Foreground(FrappeBase).
			Background(FrappeMauve),
		PopupDesc: lipgloss.NewStyle().
			Foreground(FrappeOverlay1),
		TabActive: lipgloss.NewStyle().
			Foreground(FrappeBase).
			Background(FrappeBlue).
			Bold(true),
		TabInactive: lipgloss.NewStyle().
			Foreground(FrappeOverlay1).
			Background(FrappeSurface0),
		TabSpin: lipgloss.NewStyle().
			Foreground(FrappeYellow).
			Background(FrappeSurface0),
		ModeOperate: lipgloss.NewStyle().
			Foreground(FrappeGreen).
			Bold(true),
		ModeDiag: lipgloss.NewStyle().
			Foreground(FrappePeach).
			Bold(true),
		WhichKey: lipgloss.NewStyle().
			Foreground(FrappeOverlay2).
			Background(FrappeSurface0),
		FilterBox: lipgloss.NewStyle().
			Foreground(FrappeText).
			Background(FrappeSurface0),
	}
}

// MacchiatoTheme returns the Catppuccin Macchiato theme.
func MacchiatoTheme() Theme {
	return Theme{
		PromptNormal: lipgloss.NewStyle().Foreground(MacchiatoOverlay2),
		PromptActive: lipgloss.NewStyle().Foreground(MacchiatoMauve),
		Separator:    lipgloss.NewStyle().Foreground(MacchiatoSurface2),
		Spinner:      lipgloss.NewStyle().Foreground(MacchiatoYellow),
		StatusKey:    lipgloss.NewStyle().Foreground(MacchiatoOverlay1),
		StatusValue:  lipgloss.NewStyle().Foreground(MacchiatoText),
		UserInput:    lipgloss.NewStyle().Foreground(MacchiatoSubtext0),
		AgentReply:   lipgloss.NewStyle().Foreground(MacchiatoText),
		ToolRunning:  lipgloss.NewStyle().Foreground(MacchiatoSky),
		ToolSuccess:  lipgloss.NewStyle().Foreground(MacchiatoGreen),
		ToolError:    lipgloss.NewStyle().Foreground(MacchiatoRed),
		Error:        lipgloss.NewStyle().Foreground(MacchiatoRed),
		PermPrompt:   lipgloss.NewStyle().Foreground(MacchiatoPeach),
		PermAllow:    lipgloss.NewStyle().Foreground(MacchiatoGreen),
		PermDeny:     lipgloss.NewStyle().Foreground(MacchiatoRed),
		Selection: lipgloss.NewStyle().
			Background(MacchiatoSurface2).
			Foreground(MacchiatoText),
		PopupTitle: lipgloss.NewStyle().
			Foreground(MacchiatoMauve).
			Bold(true),
		PopupItem: lipgloss.NewStyle().
			Foreground(MacchiatoText),
		PopupSelect: lipgloss.NewStyle().
			Foreground(MacchiatoBase).
			Background(MacchiatoMauve),
		PopupDesc: lipgloss.NewStyle().
			Foreground(MacchiatoOverlay1),
		TabActive: lipgloss.NewStyle().
			Foreground(MacchiatoBase).
			Background(MacchiatoBlue).
			Bold(true),
		TabInactive: lipgloss.NewStyle().
			Foreground(MacchiatoOverlay1).
			Background(MacchiatoSurface0),
		TabSpin: lipgloss.NewStyle().
			Foreground(MacchiatoYellow).
			Background(MacchiatoSurface0),
		ModeOperate: lipgloss.NewStyle().
			Foreground(MacchiatoGreen).
			Bold(true),
		ModeDiag: lipgloss.NewStyle().
			Foreground(MacchiatoPeach).
			Bold(true),
		WhichKey: lipgloss.NewStyle().
			Foreground(MacchiatoOverlay2).
			Background(MacchiatoSurface0),
		FilterBox: lipgloss.NewStyle().
			Foreground(MacchiatoText).
			Background(MacchiatoSurface0),
	}
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
		ModeOperate:  lipgloss.NewStyle().Bold(true),
		ModeDiag:     lipgloss.NewStyle().Bold(true),
		WhichKey:     lipgloss.NewStyle(),
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
