package agent

import (
	"testing"

	"shmorby/internal/config"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/session"
	"shmorby/internal/tools"
)

// overriderForTest creates a ConfigOverrider with a minimal config and
// fake components for testing.
func overriderForTest(t *testing.T) *ConfigOverrider {
	t.Helper()
	cfg := config.Config{
		Provider: "ollama",
		Model:    "test-model",
	}
	cfg.Agent.Default = "operate"
	cfg.Agent.MaxToolIterations = 20
	cfg.Tools.Timeout = 120
	cfg.Tools.Shell.Enabled = true
	cfg.Permission.Shell = "ask"
	cfg.Permission.SSH = "ask"
	cfg.Permission.Sudo = "ask"
	cfg.Permission.AWS = "ask"
	cfg.Permission.Interactive = true
	cfg.TUI.Theme = "catppuccin-mocha"
	cfg.TUI.Glamour.Enabled = true
	cfg.TUI.Logging.DefaultLevel = "info"
	cfg.TUI.Logging.Enabled = true
	cfg.Memory.AutoCapture = true
	cfg.Context.Mode = "auto"
	cfg.Context.Enabled = true
	cfg.Context.TokenEstimator = "heuristic"
	cfg.Context.Threshold = 0.8
	cfg.Context.OffloadToMemory = true

	var prov llm.Provider = &fakeProvider{name: "ollama"}
	reg := tools.NewRegistry()
	comp := ctxcomp.NewCompressor(ctxcomp.CompressorConfig{}, nil, nil, nil)
	sess := session.New()

	return NewConfigOverrider(
		&cfg, &prov, reg, comp, sess,
	)
}

// TestSet_Provider_Valid verifies provider change updates config and
// the shared provider pointer.
func TestSet_Provider_Valid(t *testing.T) {
	co := overriderForTest(t)
	// Start with openai, switch to ollama (no API key needed).
	var openAIProv llm.Provider = &fakeProvider{name: "openai"}
	*co.provider = openAIProv
	co.cfg.Provider = "openai"
	co.cfg.OpenAI.APIKey = "sk-test" // minimal setup for initial state

	msg, err := co.Set("provider", "ollama")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Provider != "ollama" {
		t.Errorf("cfg.Provider = %q, want %q",
			co.cfg.Provider, "ollama")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation message")
	}
	if co.Provider().Name() != "ollama" {
		t.Errorf("Provider().Name() = %q, want %q",
			co.Provider().Name(), "ollama")
	}
}

// TestSet_Provider_Invalid verifies invalid provider name is rejected.
func TestSet_Provider_Invalid(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("provider", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid provider")
	}
}

// TestSet_Model_String verifies model string is updated.
func TestSet_Model_String(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("model", "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Model != "gpt-4o" {
		t.Errorf("cfg.Model = %q, want %q", co.cfg.Model, "gpt-4o")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_AgentDefault_Valid verifies agent.default is updated.
func TestSet_AgentDefault_Valid(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("agent.default", "diagnose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Agent.Default != "diagnose" {
		t.Errorf("Agent.Default = %q, want %q",
			co.cfg.Agent.Default, "diagnose")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_AgentDefault_Invalid verifies invalid agent mode is rejected.
func TestSet_AgentDefault_Invalid(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("agent.default", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid agent mode")
	}
}

// TestSet_MaxToolIterations_Int verifies max_tool_iterations is updated.
func TestSet_MaxToolIterations_Int(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("agent.max_tool_iterations", "50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Agent.MaxToolIterations != 50 {
		t.Errorf("MaxToolIterations = %d, want 50",
			co.cfg.Agent.MaxToolIterations)
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_MaxToolIterations_OutOfRange verifies out-of-range value.
func TestSet_MaxToolIterations_OutOfRange(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("agent.max_tool_iterations", "200")
	if err == nil {
		t.Fatal("expected error for out-of-range value")
	}
}

// TestSet_ToolsTimeout_Int verifies tools.timeout update.
func TestSet_ToolsTimeout_Int(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("tools.timeout", "300")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Tools.Timeout != 300 {
		t.Errorf("Tools.Timeout = %d, want 300",
			co.cfg.Tools.Timeout)
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_ToolsTimeout_OutOfRange verifies out-of-range timeout.
func TestSet_ToolsTimeout_OutOfRange(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("tools.timeout", "0")
	if err == nil {
		t.Fatal("expected error for out-of-range timeout")
	}
}

// TestSet_ToolEnabled_Bool verifies tool enabled/disabled toggling.
func TestSet_ToolEnabled_Bool(t *testing.T) {
	t.Run("shell_disable", func(t *testing.T) {
		co := overriderForTest(t)
		co.cfg.Tools.Shell.Enabled = true
		msg, err := co.Set("tools.shell.enabled", "false")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if co.cfg.Tools.Shell.Enabled {
			t.Error("expected Shell.Enabled = false")
		}
		if msg == "" {
			t.Error("expected non-empty confirmation")
		}
	})
	t.Run("shell_enable", func(t *testing.T) {
		co := overriderForTest(t)
		co.cfg.Tools.Shell.Enabled = false
		msg, err := co.Set("tools.shell.enabled", "true")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !co.cfg.Tools.Shell.Enabled {
			t.Error("expected Shell.Enabled = true")
		}
		if msg == "" {
			t.Error("expected non-empty confirmation")
		}
	})
}

// TestSet_PermissionLevel_Valid verifies permission level update.
func TestSet_PermissionLevel_Valid(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("permission.shell", "allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Permission.Shell != "allow" {
		t.Errorf("Permission.Shell = %q, want %q",
			co.cfg.Permission.Shell, "allow")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_PermissionLevel_Invalid verifies invalid level is rejected.
func TestSet_PermissionLevel_Invalid(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("permission.shell", "maybe")
	if err == nil {
		t.Fatal("expected error for invalid permission level")
	}
}

// TestSet_Interactive_Bool verifies permission.interactive toggle.
func TestSet_Interactive_Bool(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("permission.interactive", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Permission.Interactive {
		t.Error("expected Permission.Interactive = false")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_ContextMode_Valid verifies context.mode update.
func TestSet_ContextMode_Valid(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("context.mode", "conservative")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Context.Mode != "conservative" {
		t.Errorf("Context.Mode = %q, want %q",
			co.cfg.Context.Mode, "conservative")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_ContextMode_Invalid verifies invalid mode is rejected.
func TestSet_ContextMode_Invalid(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("context.mode", "ultra")
	if err == nil {
		t.Fatal("expected error for invalid context mode")
	}
}

// TestSet_TUITheme_Valid verifies tui.theme triggers the theme applier.
func TestSet_TUITheme_Valid(t *testing.T) {
	var applied string
	co := overriderForTest(t)
	co.themeApplier = func(name string) { applied = name }

	msg, err := co.Set("tui.theme", "catppuccin-latte")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.TUI.Theme != "catppuccin-latte" {
		t.Errorf("TUI.Theme = %q, want %q",
			co.cfg.TUI.Theme, "catppuccin-latte")
	}
	if applied != "catppuccin-latte" {
		t.Errorf("themeApplier called with %q, want %q",
			applied, "catppuccin-latte")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_LogLevel_Valid verifies tui.logging.default_level update.
func TestSet_LogLevel_Valid(t *testing.T) {
	var setLevel string
	co := overriderForTest(t)
	co.logLevelSetter = func(level string) { setLevel = level }

	msg, err := co.Set("tui.logging.default_level", "debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.TUI.Logging.DefaultLevel != "debug" {
		t.Errorf("Logging.DefaultLevel = %q, want %q",
			co.cfg.TUI.Logging.DefaultLevel, "debug")
	}
	if setLevel != "debug" {
		t.Errorf("logLevelSetter called with %q, want %q",
			setLevel, "debug")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_LogLevel_Invalid verifies invalid log level is rejected.
func TestSet_LogLevel_Invalid(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("tui.logging.default_level", "verbose")
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

// TestSet_Threshold_Float verifies context.threshold update.
func TestSet_Threshold_Float(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("context.threshold", "0.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Context.Threshold != 0.5 {
		t.Errorf("Context.Threshold = %f, want 0.5",
			co.cfg.Context.Threshold)
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_Threshold_OutOfRange verifies out-of-range threshold.
func TestSet_Threshold_OutOfRange(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("context.threshold", "1.5")
	if err == nil {
		t.Fatal("expected error for out-of-range threshold")
	}
}

// TestSet_UnknownParam_Error verifies unknown param is rejected.
func TestSet_UnknownParam_Error(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("bogus.value", "1")
	if err == nil {
		t.Fatal("expected error for unknown param")
	}
}

// TestSet_RequiresRestart_Error verifies requires-restart param is rejected.
func TestSet_RequiresRestart_Error(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("ollama.base_url", "http://new-url:11434")
	if err == nil {
		t.Fatal("expected error for requires-restart param")
	}
}

// TestSet_TokenEstimator_Valid verifies context.token_estimator update.
func TestSet_TokenEstimator_Valid(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("context.token_estimator", "tiktoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Context.TokenEstimator != "tiktoken" {
		t.Errorf("TokenEstimator = %q, want %q",
			co.cfg.Context.TokenEstimator, "tiktoken")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_TokenEstimator_Invalid verifies invalid estimator is rejected.
func TestSet_TokenEstimator_Invalid(t *testing.T) {
	co := overriderForTest(t)
	_, err := co.Set("context.token_estimator", "gpt3")
	if err == nil {
		t.Fatal("expected error for invalid token estimator")
	}
}

// TestSet_OffloadToMemory_Bool verifies offload_to_memory toggle.
func TestSet_OffloadToMemory_Bool(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("context.offload_to_memory", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Context.OffloadToMemory {
		t.Error("expected OffloadToMemory = false")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_GlamourEnabled_Bool verifies tui.glamour.enabled toggle.
func TestSet_GlamourEnabled_Bool(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("tui.glamour.enabled", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.TUI.Glamour.Enabled {
		t.Error("expected Glamour.Enabled = false")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_LogEnabled_Bool verifies tui.logging.enabled toggle.
func TestSet_LogEnabled_Bool(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("tui.logging.enabled", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.TUI.Logging.Enabled {
		t.Error("expected Logging.Enabled = false")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_AutoCapture_Bool verifies memory.auto_capture toggle.
func TestSet_AutoCapture_Bool(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("memory.auto_capture", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Memory.AutoCapture {
		t.Error("expected AutoCapture = false")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_ContextEnabled_Bool verifies context.enabled toggle.
func TestSet_ContextEnabled_Bool(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("context.enabled", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Context.Enabled {
		t.Error("expected Context.Enabled = false")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_AgentShell_String verifies agent.shell update.
func TestSet_AgentShell_String(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("agent.shell", "/bin/zsh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if co.cfg.Agent.Shell != "/bin/zsh" {
		t.Errorf("Agent.Shell = %q, want %q",
			co.cfg.Agent.Shell, "/bin/zsh")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_RequiresRestart_Nested verifies more requires-restart params.
func TestSet_RequiresRestart_Nested(t *testing.T) {
	tests := []struct {
		param string
		value string
	}{
		{"openai.api_key", "sk-test"},
		{"openai.base_url", "http://localhost"},
		{"openai.organization", "org-test"},
		{"openai.timeout", "30"},
		{"openrouter.api_key", "sk-test"},
		{"opencode_zen.api_key", "sk-test"},
		{"opencode_zen.base_url", "http://localhost"},
		{"scope.workdir", "/tmp"},
		{"scope.instructions", "test"},
		{"memory.db_path", "/tmp/db"},
		{"memory.max_entries", "500"},
		{"memory.embedding.provider", "openai"},
		{"context.summary_model", "gpt-4"},
		{"context.summary_provider", "openai"},
		{"context.fallback_context_window", "16000"},
		{"context.max_tool_output_lines", "100"},
		{"context.max_tool_output_bytes", "10000"},
		{"context.min_messages_to_compress", "10"},
		{"context.max_tool_output_tokens", "2048"},
	}
	co := overriderForTest(t)
	for _, tc := range tests {
		t.Run(tc.param, func(t *testing.T) {
			_, err := co.Set(tc.param, tc.value)
			if err == nil {
				t.Errorf("expected error for %q", tc.param)
			}
		})
	}
}

// TestOverrideableParams_ReturnsAll verifies all 26 params are returned.
func TestOverrideableParams_ReturnsAll(t *testing.T) {
	co := overriderForTest(t)
	params := co.OverrideableParams()
	if len(params) == 0 {
		t.Fatal("OverrideableParams returned empty slice")
	}
	if len(params) < 25 {
		t.Errorf("expected at least 25 params, got %d", len(params))
	}
	// Verify each param has key, current, and options.
	for _, p := range params {
		if p.Key == "" {
			t.Error("found param with empty Key")
		}
		if p.ValidOptions == "" {
			t.Errorf("param %q has empty ValidOptions", p.Key)
		}
	}
	// Spot-check specific params.
	checks := map[string]bool{
		"provider": false, "model": false,
		"tools.timeout":    false,
		"permission.shell": false,
		"tui.theme":        false,
		"context.mode":     false,
	}
	for _, p := range params {
		if _, ok := checks[p.Key]; ok {
			checks[p.Key] = true
		}
	}
	for key, found := range checks {
		if !found {
			t.Errorf("missing param %q", key)
		}
	}
}

// TestSet_Permission_AllLevels verifies all permission keys work.
func TestSet_Permission_AllLevels(t *testing.T) {
	keys := []string{
		"permission.shell",
		"permission.ssh",
		"permission.sudo",
		"permission.aws",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			co := overriderForTest(t)
			msg, err := co.Set(key, "deny")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msg == "" {
				t.Error("expected non-empty confirmation")
			}
		})
	}
}

// TestParseBool verifies all bool representations.
func TestParseBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true}, {"false", false},
		{"yes", true}, {"no", false},
		{"1", true}, {"0", false},
		{"TRUE", true}, {"FALSE", false},
		{"Yes", true}, {"No", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseBool(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseBool(%q) = %v, want %v",
					tc.input, got, tc.want)
			}
		})
	}
}

// TestParseBool_Invalid verifies invalid bool returns error.
func TestParseBool_Invalid(t *testing.T) {
	_, err := parseBool("maybe")
	if err == nil {
		t.Fatal("expected error for invalid bool")
	}
}

// TestSet_TUIFullscreen verifies tui.fullscreen toggle.
func TestSet_TUIFullscreen(t *testing.T) {
	co := overriderForTest(t)
	msg, err := co.Set("tui.fullscreen", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !co.cfg.TUI.Fullscreen {
		t.Error("expected TUI.Fullscreen = true")
	}
	if msg == "" {
		t.Error("expected non-empty confirmation")
	}
}

// TestSet_RequiresRestart_PrefixMatch verifies prefix-based rejections.
func TestSet_RequiresRestart_PrefixMatch(t *testing.T) {
	co := overriderForTest(t)
	tests := []string{
		"permission.presets",
		"permission.rules",
		"tui.nav.follow_mode",
		"tui.nav.keybinds.leader",
	}
	for _, param := range tests {
		t.Run(param, func(t *testing.T) {
			_, err := co.Set(param, "test")
			if err == nil {
				t.Errorf("expected error for %q", param)
			}
		})
	}
}
