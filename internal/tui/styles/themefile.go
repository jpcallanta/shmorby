package styles

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"shmorby/internal/fileread"
	"shmorby/internal/util"
)

// hexColor requires the full #rrggbb form used by built-in themes.
var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// paletteRoles returns role name → color in definition order.
func (p Palette) paletteRoles() []struct {
	name  string
	color lipgloss.Color
} {
	return []struct {
		name  string
		color lipgloss.Color
	}{
		{"base", p.Base}, {"mantle", p.Mantle},
		{"surface0", p.Surface0}, {"surface1", p.Surface1},
		{"surface2", p.Surface2}, {"overlay0", p.Overlay0},
		{"overlay1", p.Overlay1}, {"overlay2", p.Overlay2},
		{"subtext0", p.Subtext0}, {"subtext1", p.Subtext1},
		{"text", p.Text}, {"lavender", p.Lavender},
		{"blue", p.Blue}, {"sapphire", p.Sapphire},
		{"sky", p.Sky}, {"teal", p.Teal},
		{"green", p.Green}, {"yellow", p.Yellow},
		{"peach", p.Peach}, {"red", p.Red},
		{"maroon", p.Maroon}, {"pink", p.Pink},
		{"mauve", p.Mauve}, {"flamingo", p.Flamingo},
		{"rosewater", p.Rosewater},
	}
}

// ThemeDoc is the YAML document of an external theme file.
type ThemeDoc struct {
	Themes map[string]Palette `yaml:"themes"`
}

// LoadThemeFile reads an external theme file and registers its themes
// into Themes so GetTheme can select them by name. The whole file is
// validated before any theme is registered; registration overrides
// existing names (later files win, built-ins may be shadowed).
func LoadThemeFile(path string) error {
	path = util.ExpandPath(path)

	b, err := fileread.ReadFileLimited(path, 0)
	if err != nil {
		return fmt.Errorf("read theme file %q: %w", path, err)
	}

	var doc ThemeDoc
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("parse theme file %q: %w", path, err)
	}
	if len(doc.Themes) == 0 {
		return fmt.Errorf("theme file %q: no themes defined", path)
	}

	// Validate everything first so a partially valid file never
	// registers anything.
	for name, p := range doc.Themes {
		if err := p.validate(); err != nil {
			return fmt.Errorf("theme file %q: theme %q: %w", path, name, err)
		}
	}

	for name, p := range doc.Themes {
		p := p
		Themes[name] = func() Theme { return newTheme(p) }
	}

	return nil
}

// validate reports palette roles that are missing or not valid hex.
func (p Palette) validate() error {
	var bad []string
	for _, role := range p.paletteRoles() {
		if !hexColor.MatchString(string(role.color)) {
			bad = append(bad, role.name)
		}
	}

	if len(bad) > 0 {
		return fmt.Errorf(
			"missing or invalid hex colors (#rrggbb required, all "+
				"%d palette roles): %s", len(p.paletteRoles()),
			strings.Join(bad, ", "),
		)
	}

	return nil
}
