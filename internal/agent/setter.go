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
	"shmorby/internal/session"
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
	mu       sync.Mutex
	provider *llm.Provider

	registry   *tools.Registry
	compressor *ctxcomp.Compressor
	session    *session.Session

	themeApplier   func(name string)
	logLevelSetter func(level string)
	memStore       interface{ SetAutoCapture(bool) }
}

// OverriderOption is a functional option for attaching optional references.
type OverriderOption func(*ConfigOverrider)

// WithLogLevelSetter sets the log level callback.
func WithLogLevelSetter(fn func(level string)) OverriderOption {
	return func(co *ConfigOverrider) { co.logLevelSetter = fn }
}

// WithMemoryStore sets the memory store for runtime config propagation.
func WithMemoryStore(store any) OverriderOption {
	return func(co *ConfigOverrider) {
		if s, ok := store.(interface{ SetAutoCapture(bool) }); ok {
			co.memStore = s
		}
	}
}

// NewConfigOverrider creates an overrider with live component references.
func NewConfigOverrider(
	cfg *config.Config,
	provider *llm.Provider,
	registry *tools.Registry,
	compressor *ctxcomp.Compressor,
	session *session.Session,
	opts ...OverriderOption,
) *ConfigOverrider {
	co := &ConfigOverrider{
		cfg:        cfg,
		provider:   provider,
		registry:   registry,
		compressor: compressor,
		session:    session,
	}
	for _, opt := range opts {
		opt(co)
	}
	return co
}

// Set applies a config override at runtime.
// Returns a user-facing message or an error.
func (co *ConfigOverrider) Set(param, value string) (string, error) {
	co.mu.Lock()
	defer co.mu.Unlock()

	switch param {
	// --- Provider ---
	case "provider":
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

	// --- Model ---
	case "model":
		co.cfg.Model = value
		llm.InvalidateModelInfo(value)

		return fmt.Sprintf("model set to %q", value), nil

	// --- Agent ---
	case "agent.default":
		if err := config.ValidateAgent(value); err != nil {
			return "", err
		}
		co.cfg.Agent.Default = value
		return fmt.Sprintf("agent.default set to %q", value), nil

	case "agent.max_tool_iterations":
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

	case "agent.shell":
		co.cfg.Agent.Shell = value
		if co.registry != nil {
			if t, ok := co.registry.Lookup("shell"); ok {
				if s, ok := t.(interface{ SetShell(string) }); ok {
					s.SetShell(value)
				}
			}
		}

		return fmt.Sprintf("agent.shell set to %q", value), nil

	// --- Tools ---
	case "tools.timeout":
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
			for _, name := range []string{"shell", "ssh", "sudo", "aws"} {
				if t, ok := co.registry.Lookup(name); ok {
					if s, ok := t.(interface{ SetDefaultTimeout(int) }); ok {
						s.SetDefaultTimeout(n)
					}
				}
			}
		}

		return fmt.Sprintf("tools.timeout set to %d", n), nil

	case "tools.shell.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"tools.shell.enabled: %s", err,
			)
		}
		co.cfg.Tools.Shell.Enabled = b
		co.syncTool("shell", b, func() tools.Tool {
			return tools.NewShellTool(
				co.cfg.Agent.Shell,
				co.cfg.Scope.Workdir,
				co.cfg.Permission.Shell,
			)
		})
		return fmt.Sprintf(
			"tools.shell.enabled set to %v", b,
		), nil

	case "tools.sudo.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf("tools.sudo.enabled: %s", err)
		}
		co.cfg.Tools.Sudo.Enabled = b
		co.syncTool("sudo", b, func() tools.Tool {
			return tools.NewSudoTool(
				co.cfg.Permission.Sudo, nil,
			)
		})

		return fmt.Sprintf("tools.sudo.enabled set to %v", b), nil

	case "tools.aws.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf("tools.aws.enabled: %s", err)
		}
		co.cfg.Tools.AWS.Enabled = b
		co.syncTool("aws", b, func() tools.Tool {
			return tools.NewAWSTool(
				co.cfg.Permission.AWS, nil,
			)
		})

		return fmt.Sprintf("tools.aws.enabled set to %v", b), nil

	// --- Permissions ---
	case "permission.shell":
		if err := config.ValidatePermissionLevel(
			"permission.shell", value,
		); err != nil {
			return "", err
		}
		co.cfg.Permission.Shell = value
		co.updateToolPerm("shell", value)

		return fmt.Sprintf("permission.shell set to %q", value), nil

	case "permission.ssh":
		if err := config.ValidatePermissionLevel(
			"permission.ssh", value,
		); err != nil {
			return "", err
		}
		co.cfg.Permission.SSH = value
		co.updateToolPerm("ssh", value)

		return fmt.Sprintf("permission.ssh set to %q", value), nil

	case "permission.sudo":
		if err := config.ValidatePermissionLevel(
			"permission.sudo", value,
		); err != nil {
			return "", err
		}
		co.cfg.Permission.Sudo = value
		co.updateToolPerm("sudo", value)

		return fmt.Sprintf("permission.sudo set to %q", value), nil

	case "permission.aws":
		if err := config.ValidatePermissionLevel(
			"permission.aws", value,
		); err != nil {
			return "", err
		}
		co.cfg.Permission.AWS = value
		co.updateToolPerm("aws", value)

		return fmt.Sprintf("permission.aws set to %q", value), nil

	case "permission.interactive":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"permission.interactive: %s", err,
			)
		}
		co.cfg.Permission.Interactive = b
		return fmt.Sprintf(
			"permission.interactive set to %v", b,
		), nil

	// --- TUI ---
	case "tui.fullscreen":
		// NOTE: Bubbletea's alt-screen mode is set at program creation
		// time via tea.WithAltScreen().  The config is updated but the
		// visual change only takes effect on next restart.
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf("tui.fullscreen: %s", err)
		}
		co.cfg.TUI.Fullscreen = b
		return fmt.Sprintf("tui.fullscreen set to %v (restart to apply)", b), nil

	case "tui.theme":
		co.cfg.TUI.Theme = value
		if co.themeApplier != nil {
			co.themeApplier(value)
		}

		return fmt.Sprintf("tui.theme set to %q", value), nil

	case "tui.glamour.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"tui.glamour.enabled: %s", err,
			)
		}
		co.cfg.TUI.Glamour.Enabled = b
		return fmt.Sprintf(
			"tui.glamour.enabled set to %v", b,
		), nil

	case "tui.logging.default_level":
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

	case "tui.logging.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"tui.logging.enabled: %s", err,
			)
		}
		co.cfg.TUI.Logging.Enabled = b
		return fmt.Sprintf(
			"tui.logging.enabled set to %v", b,
		), nil

	// --- Memory ---
	case "memory.auto_capture":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"memory.auto_capture: %s", err,
			)
		}
		co.cfg.Memory.AutoCapture = b
		if co.memStore != nil {
			co.memStore.SetAutoCapture(b)
		}
		return fmt.Sprintf(
			"memory.auto_capture set to %v", b,
		), nil

	// --- Context ---
	case "context.mode":
		if err := config.ValidateContextMode(value); err != nil {
			return "", err
		}
		co.cfg.Context.Mode = value
		if co.compressor != nil {
			co.compressor.SetMode(value)
		}
		return fmt.Sprintf("context.mode set to %q", value), nil

	case "context.enabled":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"context.enabled: %s", err,
			)
		}
		co.cfg.Context.Enabled = b
		return fmt.Sprintf("context.enabled set to %v", b), nil

	case "context.token_estimator":
		if err := config.ValidateTokenEstimator(value); err != nil {
			return "", err
		}
		co.cfg.Context.TokenEstimator = value
		return fmt.Sprintf(
			"context.token_estimator set to %q", value,
		), nil

	case "context.threshold":
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

	case "context.offload_to_memory":
		b, err := parseBool(value)
		if err != nil {
			return "", fmt.Errorf(
				"context.offload_to_memory: %s", err,
			)
		}
		co.cfg.Context.OffloadToMemory = b
		return fmt.Sprintf(
			"context.offload_to_memory set to %v", b,
		), nil

	// --- Requires restart ---
	case "ollama.base_url", "openai.api_key", "openai.base_url",
		"openai.organization", "openai.timeout",
		"openrouter.api_key", "opencode_zen.api_key",
		"opencode_zen.base_url",
		"scope.workdir", "scope.instructions",
		"memory.db_path", "memory.max_entries",
		"memory.embedding.provider", "memory.embedding.model",
		"memory.embedding.base_url",
		"context.summary_model", "context.summary_provider",
		"context.fallback_context_window",
		"context.max_tool_output_lines",
		"context.max_tool_output_bytes",
		"context.min_messages_to_compress",
		"context.max_tool_output_tokens":
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

// updateToolPerm propagates a permission level change to a tool.
func (co *ConfigOverrider) updateToolPerm(name, level string) {
	if co.registry != nil {
		if t, ok := co.registry.Lookup(name); ok {
			if s, ok := t.(interface{ SetPerm(string) }); ok {
				s.SetPerm(level)
			}
		}
	}
}

// syncTool registers or unregisters a tool by name.
func (co *ConfigOverrider) syncTool(
	name string, enabled bool, factory func() tools.Tool,
) {
	if co.registry == nil {
		return
	}
	if enabled {
		// Only register if not already present.
		if _, ok := co.registry.Lookup(name); !ok {
			t := factory()
			if s, ok := t.(interface{ SetDefaultTimeout(int) }); ok {
				s.SetDefaultTimeout(co.cfg.Tools.Timeout)
			}
			co.registry.Register(t)
		}
	} else {
		co.registry.Unregister(name)
	}
}

// parseBool parses common boolean string representations.
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

// OverrideableParams returns all parameters that can be set via /set.
func (co *ConfigOverrider) OverrideableParams() []ParamInfo {
	// Keep this roughly in the order shown in the spec for readability.
	permVal := func(v string) string { return v }
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
			ValidOptions: "operate|diagnose", Type: "string",
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
			CurrentValue: permVal(co.cfg.Permission.Shell),
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.ssh",
			CurrentValue: permVal(co.cfg.Permission.SSH),
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.sudo",
			CurrentValue: permVal(co.cfg.Permission.Sudo),
			ValidOptions: "allow|ask|deny", Type: "string",
		},
		{
			Key:          "permission.aws",
			CurrentValue: permVal(co.cfg.Permission.AWS),
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
			Key:          "context.token_estimator",
			CurrentValue: strVal(co.cfg.Context.TokenEstimator),
			ValidOptions: "heuristic|tiktoken", Type: "string",
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

// Provider returns the current live provider.
func (co *ConfigOverrider) Provider() llm.Provider {
	if co.provider != nil {
		return *co.provider
	}
	return nil
}

// Export for compiler check of interface conformance.
var _ = slog.Default()
