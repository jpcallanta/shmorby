// Package config handles layered YAML configuration loading.
package config

// TUIKeybindsConfig holds keybinding overrides for the TUI.
type TUIKeybindsConfig struct {
	Leader            string `yaml:"leader"`
	AgentCycle        string `yaml:"agent_cycle"`
	AgentCycleReverse string `yaml:"agent_cycle_reverse"`
	CommandList       string `yaml:"command_list"`
	HistorySearch     string `yaml:"history_search"`
	SessionNew        string `yaml:"session_new"`
	SessionList       string `yaml:"session_list"`
	SessionCompact    string `yaml:"session_compact"`
	ModelList         string `yaml:"model_list"`
	ThemeList         string `yaml:"theme_list"`
	AgentList         string `yaml:"agent_list"`
	SessionUndo       string `yaml:"session_undo"`
	SessionRedo       string `yaml:"session_redo"`
	EditorOpen        string `yaml:"editor_open"`
	SessionExport     string `yaml:"session_export"`
	AppExit           string `yaml:"app_exit"`
	StatusView        string `yaml:"status_view"`
	SidebarToggle     string `yaml:"sidebar_toggle"`
	TipsToggle        string `yaml:"tips_toggle"`
	MessagesCopy      string `yaml:"messages_copy"`
	SessionChildFirst string `yaml:"session_child_first"`
	SessionParent     string `yaml:"session_parent"`
	SessionChildCycle string `yaml:"session_child_cycle"`
	SessionChildRev   string `yaml:"session_child_cycle_reverse"`
}

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
	LeaderTimeout      int                   `yaml:"leader_timeout"`
	Keybinds           TUIKeybindsConfig     `yaml:"keybinds"`
	ScrollAcceleration TUIScrollAccelConfig  `yaml:"scroll_acceleration"`
	HistorySize        int                   `yaml:"history_size"`
	References         []TUIReferencesConfig `yaml:"references"`
}
