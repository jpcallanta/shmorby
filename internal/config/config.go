package config

import (
	"fmt"
	"path/filepath"

	"shmorby/internal/tools"
	"shmorby/internal/xdg"
)

// MCPConfig holds MCP server configurations.
type MCPConfig struct {
	Servers map[string]tools.MCPServerConfig `yaml:"servers"`
}

// Config holds application configuration.
//
// Merge behavior: later sources override earlier keys.
// Secrets are set in YAML api_key fields.
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
		// SubtaskTimeout is the maximum seconds a single subtask
		// can run before being cancelled (0 = derived from timeout
		// × max_tool_iterations × 2). Prevents subagent hangs.
		SubtaskTimeout int `yaml:"subtask_timeout"`
		Shell          struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"shell"`
		Sudo struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"sudo"`
		AWS struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"aws"`
		WebSearch struct {
			Enabled   bool   `yaml:"enabled"`
			Engine    string `yaml:"engine"`
			BaseURL   string `yaml:"base_url"`
			ExaAPIKey string `yaml:"exa_api_key"`
		} `yaml:"websearch"`
		WebFetch struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"webfetch"`
	} `yaml:"tools"`

	Permission struct {
		Shell       string                 `yaml:"shell"`
		SSH         string                 `yaml:"ssh"`
		Sudo        string                 `yaml:"sudo"`
		AWS         string                 `yaml:"aws"`
		MCP         string                 `yaml:"mcp"`
		Task        string                 `yaml:"task"`
		Find        string                 `yaml:"find"`
		FileRead    string                 `yaml:"file_read"`
		FileEdit    string                 `yaml:"file_edit"`
		FileWrite   string                 `yaml:"file_write"`
		Grep        string                 `yaml:"grep"`
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

		// StatusModel / StatusProvider control inference-generated
		// spinner descriptions. Empty = random flavor text (default).
		StatusModel    string `yaml:"status_model"`
		StatusProvider string `yaml:"status_provider"`
	} `yaml:"tui"`

	Memory struct {
		Enabled     bool   `yaml:"enabled"`
		DBPath      string `yaml:"db_path"`
		MaxEntries  int    `yaml:"max_entries"`
		AutoCapture bool   `yaml:"auto_capture"`
		// ContextBudget caps tokens for injected memory context
		// (0 = unlimited).
		ContextBudget int `yaml:"context_budget"`
		Embedding     struct {
			Provider   string `yaml:"provider"`
			Model      string `yaml:"model"`
			BaseURL    string `yaml:"base_url"`
			Dimensions int    `yaml:"dimensions"`
		} `yaml:"embedding"`
	} `yaml:"memory"`

	Context ContextConfig `yaml:"context"`

	// Session holds conversation persistence settings.
	Session SessionConfig `yaml:"session"`

	MCP MCPConfig `yaml:"mcp"`

	Audit AuditConfig `yaml:"audit"`

	// Ledger holds encrypted environment ledger settings.
	// When enabled, the agent exposes ledger_get/ledger_set tools
	// and injects ledger context into the system prompt.
	Ledger LedgerConfig `yaml:"ledger"`

	// Code holds settings for the code agent mode. The project root
	// is anchored to the launch CWD (or code.workdir when set) and
	// all file-oriented tools resolve paths relative to it.
	Code CodeConfig `yaml:"code"`
}

// ModelOverride holds user-specified model metadata.
type ModelOverride struct {
	ContextWindow   int    `yaml:"context_window"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	TokenizerModel  string `yaml:"tokenizer_model"`
}

// ContextConfig holds compression settings.
type ContextConfig struct {
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

// SessionConfig holds conversation persistence settings.
// Enabled=false disables all session disk writes; resume flags then
// fail cleanly. Retention controls the `session prune` sweep:
// archived or inactive sessions older than RetentionDays are
// deleted, and the store is capped to MaxSessions newest-first.
type SessionConfig struct {
	Enabled       bool   `yaml:"enabled"`
	DBPath        string `yaml:"db_path"`
	RetentionDays int    `yaml:"retention_days"`
	MaxSessions   int    `yaml:"max_sessions"`
}

// AuditConfig holds audit subsystem settings.
type AuditConfig struct {
	Enabled               bool   `yaml:"enabled"`
	DBPath                string `yaml:"db_path"`
	OutputCaptureMaxBytes int    `yaml:"output_capture_max_bytes"`
	RetentionDays         int    `yaml:"retention_days"`
	AsyncBufferSize       int    `yaml:"async_buffer_size"`
}

// LedgerConfig holds encrypted environment ledger settings.
// Enabled controls whether the agent exposes ledger tools and
// injects ledger context. ContextBudget caps the total bytes of
// ledger data injected into the system prompt (0 = unlimited).
type LedgerConfig struct {
	Enabled       bool `yaml:"enabled"`
	ContextBudget int  `yaml:"context_budget"`
}

// CodeConfig holds settings for the code agent mode. The project root
// anchors all file-oriented tools (file_read, file_edit, file_write,
// find, grep) so they cannot access paths outside the project.
type CodeConfig struct {
	// Workdir is the project root for file tools. "." (default)
	// resolves to the process CWD at startup.
	Workdir string `yaml:"workdir"`

	// AllowedPatterns are extra glob patterns permitted even when
	// they fall outside the project root.
	AllowedPatterns []string `yaml:"allowed_patterns"`

	// BlockedPatterns are extra glob patterns always rejected.
	// Built-in blocked directory names (.git/, vendor/, etc.) are
	// always enforced regardless of this list.
	BlockedPatterns []string `yaml:"blocked_patterns"`
}

// DefaultConfig returns a Config populated with standard defaults
// including xdg-based paths. Exported for use by the config migrate
// subcommand and tests.
func DefaultConfig() Config {
	return defaultConfig()
}

// defaultConfig returns a Config populated with standard defaults including xdg-based paths.
func defaultConfig() Config {
	cfg := Config{
		Provider: "ollama",
	}
	cfg.Ollama.BaseURL = "http://127.0.0.1:11434"
	cfg.OpencodeZen.BaseURL = "https://opencode.ai/zen"

	cfg.OpenAI.Timeout = 120

	cfg.Agent.Default = "operate"
	cfg.Agent.MaxToolIterations = 20
	cfg.Agent.Shell = ""

	cfg.Tools.Timeout = 120
	cfg.Tools.Shell.Enabled = true
	cfg.Tools.Sudo.Enabled = false
	cfg.Tools.AWS.Enabled = false
	cfg.Tools.WebSearch.Enabled = false
	cfg.Tools.WebSearch.Engine = "searxng"
	cfg.Tools.WebSearch.BaseURL = "http://localhost:8888"
	cfg.Tools.WebSearch.ExaAPIKey = ""
	cfg.Tools.WebFetch.Enabled = false

	cfg.Permission.Shell = "ask"
	cfg.Permission.SSH = "ask"
	cfg.Permission.Sudo = "ask"
	cfg.Permission.AWS = "ask"
	cfg.Permission.MCP = "ask"
	cfg.Permission.Task = "ask"
	cfg.Permission.Find = "allow"
	cfg.Permission.FileRead = "allow"
	cfg.Permission.FileEdit = "ask"
	cfg.Permission.FileWrite = "ask"
	cfg.Permission.Grep = "allow"
	cfg.Permission.Interactive = true

	cfg.TUI.Fullscreen = true
	cfg.Scope.Workdir = xdg.DefaultWorkDir()
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
	cfg.Memory.DBPath = filepath.Join(xdg.UserDataDir(), "memory.db")

	cfg.Context.Enabled = true
	cfg.Context.Mode = "auto"
	cfg.Context.Threshold = 0.8
	cfg.Context.MaxToolOutputTokens = 4096
	cfg.Context.MinMessagesToCompress = 6
	cfg.Context.FallbackContextWindow = 128000

	cfg.Audit.Enabled = true
	cfg.Audit.DBPath = filepath.Join(xdg.UserDataDir(), "audit.db")
	cfg.Audit.OutputCaptureMaxBytes = 65536
	cfg.Audit.RetentionDays = 365
	cfg.Audit.AsyncBufferSize = 100

	cfg.Session.Enabled = true
	cfg.Session.DBPath = filepath.Join(xdg.UserDataDir(), "sessions.db")
	cfg.Session.RetentionDays = 90
	cfg.Session.MaxSessions = 200

	cfg.Ledger.Enabled = true
	cfg.Ledger.ContextBudget = 2048

	cfg.Code.Workdir = "."
	cfg.Code.BlockedPatterns = []string{
		"**/.git/**",
		"**/node_modules/**",
		"**/vendor/**",
		"**/.idea/**",
		"**/.vscode/**",
	}

	return cfg
}

// ValidateProvider returns an error if provider is not a known value.
func ValidateProvider(provider string) error {
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

// ValidateAgent returns an error if agent is not a known mode.
func ValidateAgent(agent string) error {
	switch agent {
	case "operate", "diagnose", "chat", "code":
		return nil
	default:
		return fmt.Errorf("invalid agent %q (want operate|diagnose|chat|code)", agent)
	}
}

// ValidatePermissionLevel returns an error if permission level is not
// allow, ask, or deny.
func ValidatePermissionLevel(field, level string) error {
	switch level {
	case "allow", "ask", "deny":
		return nil
	default:
		return fmt.Errorf(
			"%s: invalid level %q (want allow|ask|deny)",
			field, level,
		)
	}
}

// ValidateContextMode returns an error if context mode is not known.
func ValidateContextMode(mode string) error {
	switch mode {
	case "auto", "aggressive", "conservative", "off":
		return nil
	default:
		return fmt.Errorf(
			"invalid context.mode %q (want auto|aggressive|"+
				"conservative|off)",
			mode,
		)
	}
}
