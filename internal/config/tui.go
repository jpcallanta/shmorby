// Package config handles layered YAML configuration loading.
package config

// TUIScrollAccelConfig holds scroll acceleration settings.
type TUIScrollAccelConfig struct {
	Enabled bool `yaml:"enabled"`
}

// TUIReferencesConfig holds @-reference source entries.
type TUIReferencesConfig struct {
	Alias string `yaml:"alias"`
	Path  string `yaml:"path"`
}

// TUILogConfig holds TUI logging settings.
type TUILogConfig struct {
	Enabled           bool   `yaml:"enabled"`
	DefaultLevel      string `yaml:"default_level"`
	MaxEntries        int    `yaml:"max_entries"`
	DisplayLimit      int    `yaml:"display_limit"`
	Collapse          bool   `yaml:"collapse"`
	CollapseThreshold int    `yaml:"collapse_threshold"`
}

// TUINavConfig holds TUI navigation settings.
type TUINavConfig struct {
	FollowMode         bool                  `yaml:"follow_mode"`
	ScrollLinesPerTick int                   `yaml:"scroll_lines_per_tick"`
	ScrollAcceleration TUIScrollAccelConfig  `yaml:"scroll_acceleration"`
	HistorySize        int                   `yaml:"history_size"`
	References         []TUIReferencesConfig `yaml:"references"`
}
