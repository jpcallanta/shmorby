package agent

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"shmorby/internal/config"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/tools"
)

// ParamInfo describes one overrideable parameter for /help output.
type ParamInfo struct {
	Key             string
	CurrentValue    string
	ValidOptions    string
	Type            string
	RequiresRestart bool
}

// ConfigOverrider applies runtime config changes and propagates them
// to the affected runtime components.
type ConfigOverrider struct {
	cfg      *config.Config
	mu       sync.RWMutex
	provider *llm.Provider

	registry   *tools.Registry
	compressor *ctxcomp.Compressor

	themeApplier   func(name string)
	logLevelSetter func(level string)
	memStore       interface{ SetAutoCapture(bool) }

	// sessionMetaUpdater is called after a successful Set so the
	// session's persisted metadata can be kept in sync with the
	// live config.
	sessionMetaUpdater func()
}

// OverriderOption is a functional option for attaching optional references.
type OverriderOption func(*ConfigOverrider)

// Sets the log level callback.
func WithLogLevelSetter(fn func(level string)) OverriderOption {
	return func(co *ConfigOverrider) { co.logLevelSetter = fn }
}

// Sets a callback invoked after each successful config override.
// It propagates runtime changes (provider, model, agent) to the
// session's persisted metadata so they survive a restart.
func WithSessionMetaUpdater(fn func()) OverriderOption {
	return func(co *ConfigOverrider) { co.sessionMetaUpdater = fn }
}

// Sets the memory store for runtime config propagation.
// Accepts any type to avoid importing memory here; the concrete
// *memory.sqliteStore implements SetAutoCapture.
func WithMemoryStore(store any) OverriderOption {
	return func(co *ConfigOverrider) {
		if s, ok := store.(interface{ SetAutoCapture(bool) }); ok {
			co.memStore = s
		}
	}
}

// Creates an overrider with live component references.
func NewConfigOverrider(
	cfg *config.Config,
	provider *llm.Provider,
	registry *tools.Registry,
	compressor *ctxcomp.Compressor,
	opts ...OverriderOption,
) *ConfigOverrider {
	co := &ConfigOverrider{
		cfg:        cfg,
		provider:   provider,
		registry:   registry,
		compressor: compressor,
	}
	for _, opt := range opts {
		opt(co)
	}
	return co
}

// Applies a config override at runtime.
// Returns a user-facing message or an error.
func (co *ConfigOverrider) Set(param, value string) (msg string, err error) {
	co.mu.Lock()
	defer co.mu.Unlock()

	// Notify the session metadata updater after a successful change
	// so persisted metadata reflects the current runtime values.
	defer func() {
		if err == nil && co.sessionMetaUpdater != nil {
			co.sessionMetaUpdater()
		}
	}()

	switch param {
	// --- Provider & Model ---
	case "provider":
		return co.setProvider(value)
	case "model":
		return co.setModel(value)
	case "apikey":
		return co.setAPIKey(value)

	// --- Agent ---
	case "agent.default":
		return co.setAgentDefault(value)
	case "agent.max_tool_iterations":
		return co.setAgentMaxToolIterations(value)
	case "agent.shell":
		return co.setAgentShell(value)

	// --- Tools ---
	case "tools.timeout":
		return co.setToolsTimeout(value)
	case "tools.shell.enabled":
		return co.setToolEnabled(tools.ToolShell, value,
			func(b bool) { co.cfg.Tools.Shell.Enabled = b },
			func() tools.Tool {
				return tools.NewShellTool(
					co.cfg.Agent.Shell,
					co.cfg.Scope.Workdir,
					co.cfg.Permission.Shell,
				)
			})
	case "tools.sudo.enabled":
		return co.setToolEnabled(tools.ToolSudo, value,
			func(b bool) { co.cfg.Tools.Sudo.Enabled = b },
			func() tools.Tool {
				return tools.NewSudoTool(
					co.cfg.Permission.Sudo, nil,
				)
			})
	case "tools.aws.enabled":
		return co.setToolEnabled(tools.ToolAWS, value,
			func(b bool) { co.cfg.Tools.AWS.Enabled = b },
			func() tools.Tool {
				return tools.NewAWSTool(
					co.cfg.Permission.AWS, nil,
				)
			})

	// --- Permissions ---
	case "permission.shell", "permission.ssh",
		"permission.sudo", "permission.aws",
		"permission.find", "permission.file_read",
		"permission.file_edit", "permission.file_write",
		"permission.grep":
		return co.setPermission(param, value)
	case "permission.interactive":
		return co.setPermissionInteractive(value)

	// --- TUI ---
	case "tui.fullscreen":
		return co.setTUIFullscreen(value)
	case "tui.theme":
		return co.setTUITheme(value)
	case "tui.glamour.enabled":
		return co.setTUIGlamour(value)
	case "tui.logging.default_level":
		return co.setTUILogLevel(value)
	case "tui.logging.enabled":
		return co.setTUILogEnabled(value)

	// --- Memory ---
	case "memory.auto_capture":
		return co.setMemoryAutoCapture(value)

	// --- Context ---
	case "context.mode", "context.enabled",
		"context.threshold", "context.offload_to_memory":
		return co.setContext(param, value)

	// --- Requires restart ---
	case "ollama.base_url", "openai.api_key", "openai.base_url",
		"openai.organization", "openai.timeout",
		"openrouter.api_key", "opencode_zen.api_key",
		"opencode_zen.base_url",
		"scope.workdir", "scope.instructions",
		"memory.db_path", "memory.max_entries",
		"memory.embedding.provider", "memory.embedding.model",
		"memory.embedding.base_url", "memory.embedding.dimensions",
		"context.summary_model", "context.summary_provider",
		"context.fallback_context_window",
		"context.max_tool_output_lines",
		"context.max_tool_output_bytes",
		"context.min_messages_to_compress",
		"context.max_tool_output_tokens",
		"ledger.enabled":
		return "", fmt.Errorf(
			"%s cannot be changed at runtime (requires restart)",
			param,
		)

	default:
		if strings.HasPrefix(param, "models.") ||
			strings.HasPrefix(param, "permission.presets") ||
			strings.HasPrefix(param, "permission.rules") ||
			strings.HasPrefix(param, "tui.nav.") ||
			strings.HasPrefix(param, "tui.nav.keybinds.") {
			return "", fmt.Errorf(
				"%s cannot be changed at runtime (requires restart)",
				param,
			)
		}
		return "", fmt.Errorf(
			"unknown config parameter %q - try /help",
			param,
		)
	}
}

// --- Provider & Model setters ---

// Handles provider changes. Creates a new provider instance
// and updates the shared provider pointer if non-nil.
func (co *ConfigOverrider) setProvider(value string) (string, error) {
	if err := config.ValidateProvider(value); err != nil {
		return "", err
	}
	co.cfg.Provider = value
	newProv, err := llm.NewProvider(*co.cfg)
	if err != nil {
		return "", fmt.Errorf("cannot switch provider: %w", err)
	}
	if co.provider != nil {
		*co.provider = newProv
	}
	return fmt.Sprintf(
		"provider set to %q (provider reconnecting...)", value,
	), nil
}

// Updates the model and invalidates cached model info.
func (co *ConfigOverrider) setModel(value string) (string, error) {
	co.cfg.Model = value
	llm.InvalidateModelInfo(value)

	return fmt.Sprintf("model set to %q", value), nil
}

// Sets the API key for the current provider and reconnects.
func (co *ConfigOverrider) setAPIKey(value string) (string, error) {
	switch co.cfg.Provider {
	case "openai":
		co.cfg.OpenAI.APIKey = value
	case "openrouter":
		co.cfg.OpenRouter.APIKey = value
	case "opencode_zen":
		co.cfg.OpencodeZen.APIKey = value
	case "ollama":
		return "", fmt.Errorf(
			"ollama has no API key; set base URL via config",
		)
	default:
		return "", fmt.Errorf("unknown provider %q",
			co.cfg.Provider)
	}
	newProv, err := llm.NewProvider(*co.cfg)
	if err != nil {
		return "", fmt.Errorf(
			"cannot reconnect with new key: %w", err,
		)
	}
	if co.provider != nil {
		*co.provider = newProv
	}
	llm.InvalidateModelInfo(co.cfg.Model)

	return "apikey set (provider reconnecting...)", nil
}

// --- Agent setters ---

// Validates and sets the default agent mode.
func (co *ConfigOverrider) setAgentDefault(value string) (string, error) {
	if err := config.ValidateAgent(value); err != nil {
		return "", err
	}
	co.cfg.Agent.Default = value
	return fmt.Sprintf("agent.default set to %q", value), nil
}

// Parses and validates the max tool
// iterations value, rejecting out-of-range inputs.
func (co *ConfigOverrider) setAgentMaxToolIterations(
	value string,
) (string, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return "", fmt.Errorf(
			"agent.max_tool_iterations must be an integer",
		)
	}
	if n < 1 || n > 100 {
		return "", fmt.Errorf(
			"agent.max_tool_iterations must be between 1 and 100",
		)
	}
	co.cfg.Agent.MaxToolIterations = n
	return fmt.Sprintf(
		"agent.max_tool_iterations set to %d", n,
	), nil
}

// Sets the agent shell path and propagates to the
// shell tool if present in the registry.
func (co *ConfigOverrider) setAgentShell(value string) (string, error) {
	co.cfg.Agent.Shell = value
	if co.registry != nil {
		if t, ok := co.registry.Lookup(tools.ToolShell); ok {
			if s, ok := t.(interface{ SetShell(string) }); ok {
				s.SetShell(value)
			}
		}
	}

	return fmt.Sprintf("agent.shell set to %q", value), nil
}

// --- Tool setters ---

// Parses and validates the timeout value, then
// propagates it to all tools that support SetDefaultTimeout.
func (co *ConfigOverrider) setToolsTimeout(value string) (string, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return "", fmt.Errorf(
			"tools.timeout must be an integer",
		)
	}
	if n < 1 || n > 3600 {
		return "", fmt.Errorf(
			"tools.timeout must be between 1 and 3600",
		)
	}
	co.cfg.Tools.Timeout = n
	if co.registry != nil {
		for _, name := range []string{
			tools.ToolShell, tools.ToolSSH, tools.ToolSudo, tools.ToolAWS,
		} {
			if t, ok := co.registry.Lookup(name); ok {
				if s, ok := t.(interface{ SetDefaultTimeout(int) }); ok {
					s.SetDefaultTimeout(n)
				}
			}
		}
	}

	return fmt.Sprintf("tools.timeout set to %d", n), nil
}

// Parses a boolean value and registers/unregisters
// the specified tool. setter updates the config field; factory
// creates the tool if needed.
func (co *ConfigOverrider) setToolEnabled(
	name, value string,
	setter func(bool),
	factory func() tools.Tool,
) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"tools.%s.enabled: %w", name, err,
		)
	}
	setter(b)
	co.syncTool(name, b, factory)
	return fmt.Sprintf(
		"tools.%s.enabled set to %v", name, b,
	), nil
}

// --- Permission setters ---

// Validates and sets a permission level for the
// specified tool, then propagates it to the tool via the registry.
func (co *ConfigOverrider) setPermission(param, value string) (string, error) {
	if err := config.ValidatePermissionLevel(param, value); err != nil {
		return "", err
	}
	// Extract tool name from param (e.g., "permission.shell" -> "shell").
	toolName := strings.TrimPrefix(param, "permission.")
	switch toolName {
	case tools.ToolShell:
		co.cfg.Permission.Shell = value
	case tools.ToolSSH:
		co.cfg.Permission.SSH = value
	case tools.ToolSudo:
		co.cfg.Permission.Sudo = value
	case tools.ToolAWS:
		co.cfg.Permission.AWS = value
	case tools.ToolFind:
		co.cfg.Permission.Find = value
	case tools.ToolFileRead:
		co.cfg.Permission.FileRead = value
	case tools.ToolFileEdit:
		co.cfg.Permission.FileEdit = value
	case tools.ToolFileWrite:
		co.cfg.Permission.FileWrite = value
	case tools.ToolGrep:
		co.cfg.Permission.Grep = value
	default:
		return "", fmt.Errorf(
			"unknown permission field %q", toolName,
		)
	}
	co.updateToolPerm(toolName, value)

	return fmt.Sprintf("%s set to %q", param, value), nil
}

// Parses and sets the interactive permission.
func (co *ConfigOverrider) setPermissionInteractive(
	value string,
) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"permission.interactive: %w", err,
		)
	}
	co.cfg.Permission.Interactive = b
	return fmt.Sprintf(
		"permission.interactive set to %v", b,
	), nil
}

// --- TUI setters ---

// Sets the fullscreen mode. Note: visual change
// only takes effect on next program restart (Bubbletea's
// alt-screen mode is set at program creation via tea.WithAltScreen).
func (co *ConfigOverrider) setTUIFullscreen(value string) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf("tui.fullscreen: %w", err)
	}
	co.cfg.TUI.Fullscreen = b
	return fmt.Sprintf("tui.fullscreen set to %v (restart to apply)", b), nil
}

// Sets the theme and triggers the theme applier callback.
func (co *ConfigOverrider) setTUITheme(value string) (string, error) {
	co.cfg.TUI.Theme = value
	if co.themeApplier != nil {
		co.themeApplier(value)
	}

	return fmt.Sprintf("tui.theme set to %q", value), nil
}

// Parses and sets the glamour rendering mode.
func (co *ConfigOverrider) setTUIGlamour(value string) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"tui.glamour.enabled: %w", err,
		)
	}
	co.cfg.TUI.Glamour.Enabled = b
	return fmt.Sprintf(
		"tui.glamour.enabled set to %v", b,
	), nil
}

// Validates and sets the log level, propagating it
// to the log level setter callback if set.
func (co *ConfigOverrider) setTUILogLevel(value string) (string, error) {
	switch value {
	case "debug", "info", "warn", "error":
		co.cfg.TUI.Logging.DefaultLevel = value
		if co.logLevelSetter != nil {
			co.logLevelSetter(value)
		}
		return fmt.Sprintf(
			"tui.logging.default_level set to %q", value,
		), nil
	default:
		return "", fmt.Errorf(
			"tui.logging.default_level: invalid level %q "+
				"(want debug|info|warn|error)",
			value,
		)
	}
}

// Parses and sets the logging enabled flag.
func (co *ConfigOverrider) setTUILogEnabled(value string) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"tui.logging.enabled: %w", err,
		)
	}
	co.cfg.TUI.Logging.Enabled = b
	return fmt.Sprintf(
		"tui.logging.enabled set to %v", b,
	), nil
}

// --- Memory setter ---

// Parses and sets the auto capture flag,
// propagating it to the memory store if set.
func (co *ConfigOverrider) setMemoryAutoCapture(value string) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"memory.auto_capture: %w", err,
		)
	}
	co.cfg.Memory.AutoCapture = b
	if co.memStore != nil {
		co.memStore.SetAutoCapture(b)
	}
	return fmt.Sprintf(
		"memory.auto_capture set to %v", b,
	), nil
}

// --- Context setters ---

// Handles all context-related config changes by
// dispatching to the appropriate handler based on the key.
func (co *ConfigOverrider) setContext(param, value string) (string, error) {
	switch param {
	case "context.mode":
		return co.setContextMode(value)
	case "context.enabled":
		return co.setContextEnabled(value)
	case "context.threshold":
		return co.setContextThreshold(value)
	case "context.offload_to_memory":
		return co.setContextOffload(value)
	default:
		return "", fmt.Errorf("unknown context param %q", param)
	}
}

// Validates and sets the context mode, propagating
// it to the compressor if set.
func (co *ConfigOverrider) setContextMode(value string) (string, error) {
	if err := config.ValidateContextMode(value); err != nil {
		return "", err
	}
	co.cfg.Context.Mode = value
	if co.compressor != nil {
		co.compressor.SetMode(value)
	}
	return fmt.Sprintf("context.mode set to %q", value), nil
}

// Parses and sets the context enabled flag.
func (co *ConfigOverrider) setContextEnabled(value string) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"context.enabled: %w", err,
		)
	}
	co.cfg.Context.Enabled = b
	return fmt.Sprintf("context.enabled set to %v", b), nil
}

// Parses and validates the threshold value,
// then propagates it to the compressor if set.
func (co *ConfigOverrider) setContextThreshold(value string) (string, error) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return "", fmt.Errorf(
			"context.threshold must be a float",
		)
	}
	if f < 0.0 || f > 1.0 {
		return "", fmt.Errorf(
			"context.threshold must be between 0.0 and 1.0",
		)
	}
	co.cfg.Context.Threshold = f
	if co.compressor != nil {
		co.compressor.SetThreshold(f)
	}

	return fmt.Sprintf("context.threshold set to %.1f", f), nil
}

// Parses and sets the offload to memory flag.
func (co *ConfigOverrider) setContextOffload(value string) (string, error) {
	b, err := parseBool(value)
	if err != nil {
		return "", fmt.Errorf(
			"context.offload_to_memory: %w", err,
		)
	}
	co.cfg.Context.OffloadToMemory = b
	return fmt.Sprintf(
		"context.offload_to_memory set to %v", b,
	), nil
}

// --- Helper methods ---

// Propagates a permission level change to a tool.
func (co *ConfigOverrider) updateToolPerm(name, level string) {
	if co.registry != nil {
		if t, ok := co.registry.Lookup(name); ok {
			if s, ok := t.(interface{ SetPerm(string) }); ok {
				s.SetPerm(level)
			}
		}
	}
}

// Registers or unregisters a tool by name.
func (co *ConfigOverrider) syncTool(
	name string, enabled bool, factory func() tools.Tool,
) {
	if co.registry == nil {
		return
	}
	if enabled {
		if _, ok := co.registry.Lookup(name); !ok {
			t := factory()
			if s, ok := t.(interface{ SetDefaultTimeout(int) }); ok {
				s.SetDefaultTimeout(co.cfg.Tools.Timeout)
			}
			// Non-fatal: duplicate names are logged by the
			// registry and the existing tool is kept.
			if err := co.registry.Register(t); err != nil {
				slog.Warn("tool registration skipped",
					"name", name, "err", err)
			}
		}
	} else {
		co.registry.Unregister(name)
	}
}

// Parses common boolean string representations.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "yes", "1":
		return true, nil
	case "false", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf(
			"cannot parse %q as bool (want true|false|yes|no|1|0)", s,
		)
	}
}

// Returns all parameters that can be set via /set.
func (co *ConfigOverrider) OverrideableParams() []ParamInfo {
	boolVal := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	strVal := func(v string) string {
		if v == "" {
			return "\"\""
		}
		return v
	}

	params := []ParamInfo{
		{
			Key: "provider", CurrentValue: strVal(co.cfg.Provider),
			ValidOptions: "ollama|openai|openrouter|opencode_zen",
			Type:         "string",
		},
		{
			Key: "model", CurrentValue: strVal(co.cfg.Model),
			ValidOptions: "any string", Type: "string",
		},
		{
			Key:          "agent.default",
			CurrentValue: strVal(co.cfg.Agent.Default),
			ValidOptions: "operate|diagnose|chat|code", Type: "string",
		},
		{
			Key: "agent.max_tool_iterations",
			CurrentValue: fmt.Sprintf(
				"%d", co.cfg.Agent.MaxToolIterations,
			),
			ValidOptions: "1–100", Type: "int",
		},
		{
			Key:          "agent.shell",
			CurrentValue: strVal(co.cfg.Agent.Shell),
			ValidOptions: "any shell path", Type: "string",
		},
		{
			Key:          "tools.timeout",
			CurrentValue: fmt.Sprintf("%d", co.cfg.Tools.Timeout),
			ValidOptions: "1–3600 (seconds)", Type: "int",
		},
		{
			Key:          "tools.shell.enabled",
			CurrentValue: boolVal(co.cfg.Tools.Shell.Enabled),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "tools.sudo.enabled",
			CurrentValue: boolVal(co.cfg.Tools.Sudo.Enabled),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "tools.aws.enabled",
			CurrentValue: boolVal(co.cfg.Tools.AWS.Enabled),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "permission.shell",
			CurrentValue: co.cfg.Permission.Shell,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.ssh",
			CurrentValue: co.cfg.Permission.SSH,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.sudo",
			CurrentValue: co.cfg.Permission.Sudo,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.aws",
			CurrentValue: co.cfg.Permission.AWS,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.find",
			CurrentValue: co.cfg.Permission.Find,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.file_read",
			CurrentValue: co.cfg.Permission.FileRead,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.file_edit",
			CurrentValue: co.cfg.Permission.FileEdit,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.file_write",
			CurrentValue: co.cfg.Permission.FileWrite,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.grep",
			CurrentValue: co.cfg.Permission.Grep,
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.interactive",
			CurrentValue: boolVal(co.cfg.Permission.Interactive),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "tui.fullscreen",
			CurrentValue: boolVal(co.cfg.TUI.Fullscreen),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "tui.theme",
			CurrentValue: strVal(co.cfg.TUI.Theme),
			ValidOptions: "any theme name", Type: "string",
		},
		{
			Key:          "tui.glamour.enabled",
			CurrentValue: boolVal(co.cfg.TUI.Glamour.Enabled),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "tui.logging.default_level",
			CurrentValue: strVal(co.cfg.TUI.Logging.DefaultLevel),
			ValidOptions: "debug|info|warn|error", Type: "string",
		},
		{
			Key:          "tui.logging.enabled",
			CurrentValue: boolVal(co.cfg.TUI.Logging.Enabled),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "memory.auto_capture",
			CurrentValue: boolVal(co.cfg.Memory.AutoCapture),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key:          "context.mode",
			CurrentValue: strVal(co.cfg.Context.Mode),
			ValidOptions: "auto|aggressive|conservative|off",
			Type:         "string",
		},
		{
			Key:          "context.enabled",
			CurrentValue: boolVal(co.cfg.Context.Enabled),
			ValidOptions: "true|false", Type: "bool",
		},
		{
			Key: "context.threshold",
			CurrentValue: fmt.Sprintf(
				"%.1f", co.cfg.Context.Threshold,
			),
			ValidOptions: "0.0–1.0", Type: "float",
		},
		{
			Key:          "context.offload_to_memory",
			CurrentValue: boolVal(co.cfg.Context.OffloadToMemory),
			ValidOptions: "true|false", Type: "bool",
		},
	}

	return params
}

// Returns the current live provider.
func (co *ConfigOverrider) Provider() llm.Provider {
	co.mu.RLock()
	defer co.mu.RUnlock()

	if co.provider != nil {
		return *co.provider
	}
	return nil
}

// Returns a copy of the current config.
func (co *ConfigOverrider) Config() config.Config {
	return *co.cfg
}
