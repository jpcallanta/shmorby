package config

import (
	"fmt"

	"shmorby/internal/tools"
)

// Config is Phase 1 configuration.
//
// Merge behavior: later sources override earlier keys.
// Secrets are expected to be provided via environment variables.
type Config struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`

	Ollama struct {
		BaseURL string `yaml:"base_url"`
	} `yaml:"ollama"`

	OpenRouter struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"openrouter"`

	OpencodeZen struct {
		APIKey  string `yaml:"api_key"`
		BaseURL string `yaml:"base_url"`
	} `yaml:"opencode_zen"`

	OpenAI struct {
		APIKey       string `yaml:"api_key"`
		APIKeyEnv    string `yaml:"api_key_env"`
		BaseURL      string `yaml:"base_url"`
		Organization string `yaml:"organization"`
		Timeout      int    `yaml:"timeout"`
	} `yaml:"openai"`

	Scope struct {
		Workdir      string   `yaml:"workdir"`
		Instructions []string `yaml:"instructions"`
	} `yaml:"scope"`

	Agent struct {
		Default           string `yaml:"default"`
		MaxToolIterations int    `yaml:"max_tool_iterations"`
		Shell             string `yaml:"shell"`
	} `yaml:"agent"`

	Tools struct {
		Timeout int `yaml:"timeout"`
		Shell   struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"shell"`
		Sudo struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"sudo"`
		AWS struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"aws"`
	} `yaml:"tools"`

	Permission struct {
		Shell       string                 `yaml:"shell"`
		SSH         string                 `yaml:"ssh"`
		Sudo        string                 `yaml:"sudo"`
		AWS         string                 `yaml:"aws"`
		Interactive bool                   `yaml:"interactive"`
		Presets     []string               `yaml:"presets"`
		Rules       []tools.PermissionRule `yaml:"rules"`
	} `yaml:"permission"`

	Models map[string]ModelOverride `yaml:"models"`

	TUI struct {
		Fullscreen bool   `yaml:"fullscreen"`
		Theme      string `yaml:"theme"`
		Glamour    struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"glamour"`
		Nav     TUINavConfig `yaml:"nav"`
		Logging TUILogConfig `yaml:"logging"`
	} `yaml:"tui"`

	Memory struct {
		Enabled     bool   `yaml:"enabled"`
		DBPath      string `yaml:"db_path"`
		MaxEntries  int    `yaml:"max_entries"`
		AutoCapture bool   `yaml:"auto_capture"`
		Embedding   struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
			BaseURL  string `yaml:"base_url"`
		} `yaml:"embedding"`
	} `yaml:"memory"`

	Context ContextConfig `yaml:"context"`
}

// ModelOverride holds user-specified model metadata.
type ModelOverride struct {
	ContextWindow   int    `yaml:"context_window"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	TokenizerModel  string `yaml:"tokenizer_model"`
}

// ContextConfig holds compression and token estimation settings.
type ContextConfig struct {
	TokenEstimator        string  `yaml:"token_estimator"`
	Enabled               bool    `yaml:"enabled"`
	Mode                  string  `yaml:"mode"`
	Threshold             float64 `yaml:"threshold"`
	MaxToolOutputTokens   int     `yaml:"max_tool_output_tokens"`
	MaxToolOutputLines    int     `yaml:"max_tool_output_lines"`
	MaxToolOutputBytes    int     `yaml:"max_tool_output_bytes"`
	SummaryModel          string  `yaml:"summary_model"`
	SummaryProvider       string  `yaml:"summary_provider"`
	OffloadToMemory       bool    `yaml:"offload_to_memory"`
	MinMessagesToCompress int     `yaml:"min_messages_to_compress"`
	FallbackContextWindow int     `yaml:"fallback_context_window"`
}

// Returns a Config populated with standard defaults.
func defaultConfig() Config {
	cfg := Config{
		Provider: "ollama",
	}
	cfg.Ollama.BaseURL = "http://127.0.0.1:11434"
	cfg.OpencodeZen.BaseURL = "https://opencode.ai/zen"

	cfg.OpenAI.Timeout = 120

	cfg.Agent.Default = "operate"
	cfg.Agent.MaxToolIterations = 20
	cfg.Agent.Shell = "bash"

	cfg.Tools.Timeout = 120
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Sudo.Enabled = false
	cfg.Tools.AWS.Enabled = false

	cfg.Permission.Shell = "allow"
	cfg.Permission.SSH = "allow"
	cfg.Permission.Sudo = "ask"
	cfg.Permission.AWS = "ask"

	cfg.TUI.Fullscreen = true
	cfg.TUI.Glamour.Enabled = true
	cfg.TUI.Logging.Enabled = true
	cfg.TUI.Logging.DefaultLevel = "info"
	cfg.TUI.Logging.MaxEntries = 100
	cfg.TUI.Logging.DisplayLimit = 20
	cfg.TUI.Logging.Collapse = true
	cfg.TUI.Logging.CollapseThreshold = 5
	cfg.TUI.Nav.FollowMode = true
	cfg.TUI.Nav.ScrollLinesPerTick = 5
	cfg.TUI.Nav.LeaderTimeout = 2000
	cfg.TUI.Nav.HistorySize = 100
	cfg.TUI.Nav.Keybinds.Leader = "ctrl+x"
	cfg.TUI.Nav.Keybinds.AgentCycle = "tab"
	cfg.TUI.Nav.Keybinds.AgentCycleReverse = "shift+tab"
	cfg.TUI.Nav.Keybinds.CommandList = "ctrl+p"
	cfg.TUI.Nav.Keybinds.HistorySearch = "ctrl+r"
	cfg.TUI.Nav.Keybinds.SessionNew = "<leader>n"
	cfg.TUI.Nav.Keybinds.SessionList = "<leader>l"
	cfg.TUI.Nav.Keybinds.SessionCompact = "<leader>c"
	cfg.TUI.Nav.Keybinds.ModelList = "<leader>m"
	cfg.TUI.Nav.Keybinds.ThemeList = "<leader>t"
	cfg.TUI.Nav.Keybinds.AgentList = "<leader>a"
	cfg.TUI.Nav.Keybinds.SessionUndo = "<leader>u"
	cfg.TUI.Nav.Keybinds.SessionRedo = "<leader>r"
	cfg.TUI.Nav.Keybinds.EditorOpen = "<leader>e"
	cfg.TUI.Nav.Keybinds.SessionExport = "<leader>x"
	cfg.TUI.Nav.Keybinds.AppExit = "<leader>q"
	cfg.TUI.Nav.Keybinds.StatusView = "<leader>s"
	cfg.TUI.Nav.Keybinds.SidebarToggle = "<leader>b"
	cfg.TUI.Nav.Keybinds.TipsToggle = "<leader>h"
	cfg.TUI.Nav.Keybinds.MessagesCopy = "<leader>y"
	cfg.TUI.Nav.Keybinds.SessionChildFirst = "<leader>down"
	cfg.TUI.Nav.Keybinds.SessionParent = "up"
	cfg.TUI.Nav.Keybinds.SessionChildCycle = "right"
	cfg.TUI.Nav.Keybinds.SessionChildRev = "left"

	cfg.Memory.Enabled = true
	cfg.Memory.MaxEntries = 10000
	cfg.Memory.AutoCapture = true
	cfg.Memory.DBPath = "~/.local/share/shmorby/memory.db"

	cfg.Context.TokenEstimator = "heuristic"
	cfg.Context.Enabled = true
	cfg.Context.Mode = "auto"
	cfg.Context.Threshold = 0.8
	cfg.Context.MaxToolOutputTokens = 4096
	cfg.Context.MinMessagesToCompress = 6
	cfg.Context.FallbackContextWindow = 128000

	return cfg
}

// Returns an error if provider is not a known value.
func validateProvider(provider string) error {
	switch provider {
	case "ollama", "openrouter", "opencode_zen", "openai":
		return nil
	default:
		return fmt.Errorf(
			"invalid provider %q (want ollama|openrouter|opencode_zen|openai)",
			provider,
		)
	}
}

// Returns an error if agent is not operate or diagnose.
func validateAgent(agent string) error {
	switch agent {
	case "operate", "diagnose":
		return nil
	default:
		return fmt.Errorf("invalid agent %q (want operate|diagnose)", agent)
	}
}
