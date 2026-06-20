package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	for _, e := range []string{
		"SHMORBY_PROVIDER",
		"SHMORBY_MODEL",
		"SHMORBY_TOOLS_TIMEOUT",
		"SHMORBY_TOOL_OUTPUT_MAX_LINES",
		"SHMORBY_TOOL_OUTPUT_MAX_BYTES",
		"OLLAMA_BASE_URL",
		"OPENROUTER_API_KEY",
		"OPENCODE_ZEN_API_KEY",
		"OPENCODE_ZEN_BASE_URL",
		"OPENAI_API_KEY",
		"OPENAI_ORG_ID",
		"OPENAI_BASE_URL",
		"OPENAI_TIMEOUT",
	} {
		os.Unsetenv(e)
	}
	os.Exit(m.Run())
}

// writeConfig writes content to path, failing the test on error.
func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLoad_DefaultsNoFiles verifies Load succeeds with defaults when no config files exist.
func TestLoad_DefaultsNoFiles(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "ollama" {
		t.Fatalf("Provider: got %q want %q", cfg.Provider, "ollama")
	}
	if cfg.Agent.Default != "operate" {
		t.Fatalf("Agent.Default: got %q want %q", cfg.Agent.Default, "operate")
	}
	if cfg.Agent.MaxToolIterations != 20 {
		t.Fatalf("Agent.MaxToolIterations: got %d want 20", cfg.Agent.MaxToolIterations)
	}
	if cfg.Agent.Shell != "" {
		t.Fatalf("Agent.Shell: got %q want %q", cfg.Agent.Shell, "")
	}
	if !cfg.Tools.Shell.Enabled {
		t.Fatal("Tools.Shell.Enabled: want true")
	}
	if cfg.Permission.Shell != "ask" {
		t.Fatalf("Permission.Shell: got %q want %q", cfg.Permission.Shell, "ask")
	}
	if cfg.Permission.Sudo != "ask" {
		t.Fatalf("Permission.Sudo: got %q want %q", cfg.Permission.Sudo, "ask")
	}
	if !cfg.Permission.Interactive {
		t.Fatal("Permission.Interactive: want true")
	}
}

// TestLoad_MergeLaterWins checks that a later YAML file (cwd shmorby.yaml)
// overrides an earlier one (--config file).
func TestLoad_MergeLaterWins(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	cfgPath := filepath.Join(ucd, "override.yaml")
	writeConfig(t, cfgPath, `
provider: ollama
model: model1
scope:
  workdir: /a
  instructions:
    - ./SCOPE-a.md
`)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwd := filepath.Join(ucd, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	writeConfig(t, filepath.Join(cwd, "shmorby.yaml"), `
model: model2
scope:
  workdir: /b
  instructions:
    - ./SCOPE-b.md
`)
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cfg, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "ollama" {
		t.Fatalf("Provider: got %q want %q", cfg.Provider, "ollama")
	}
	if cfg.Model != "model2" {
		t.Fatalf("Model: got %q want %q", cfg.Model, "model2")
	}
	if cfg.Scope.Workdir != "/b" {
		t.Fatalf("Scope.Workdir: got %q want %q", cfg.Scope.Workdir, "/b")
	}
	if len(cfg.Scope.Instructions) != 1 || cfg.Scope.Instructions[0] != "./SCOPE-b.md" {
		t.Fatalf("Scope.Instructions: got %#v", cfg.Scope.Instructions)
	}
}

// TestLoad_CLIProviderOverridesFile checks that --provider openrouter beats a file value.
func TestLoad_CLIProviderOverridesFile(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwd := filepath.Join(ucd, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	writeConfig(t, filepath.Join(cwd, "shmorby.yaml"), `
provider: ollama
model: file-model
openrouter:
  api_key: sk-or-test
`)
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cfg, err := Load(LoadOptions{
		Provider:      "openrouter",
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Fatalf("Provider: got %q want %q", cfg.Provider, "openrouter")
	}
	if cfg.Model != "file-model" {
		t.Fatalf("Model: got %q want %q", cfg.Model, "file-model")
	}
}

// TestLoad_ConfigMissingError checks that --config pointing at a nonexistent file returns an error.
func TestLoad_ConfigMissingError(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	_, err := Load(LoadOptions{
		ConfigFile:    "/nonexistent/does-not-exist.yaml",
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for missing --config file, got nil")
	}
}

// TestLoad_InvalidProviderFromYAML checks that an invalid provider in YAML returns error.
func TestLoad_InvalidProviderFromYAML(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwd := filepath.Join(ucd, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	writeConfig(t, filepath.Join(cwd, "shmorby.yaml"), `
provider: bogus
`)
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	_, err = Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
}

// TestLoad_EnvVarNoLongerAffectsConfig checks that env vars do not override config values.
func TestLoad_EnvVarNoLongerAffectsConfig(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwd := filepath.Join(ucd, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	writeConfig(t, filepath.Join(cwd, "shmorby.yaml"), `
provider: ollama
`)
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	t.Setenv("SHMORBY_PROVIDER", "openrouter")

	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Env var should NOT override YAML value.
	if cfg.Provider != "ollama" {
		t.Fatalf("Provider: want %q, got %q (env should not affect config)", "ollama", cfg.Provider)
	}
}

// TestLoad_CLIModelOverridesDefaults checks that --model CLI flag overrides defaults.
func TestLoad_CLIModelOverridesDefaults(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	cfg, err := Load(LoadOptions{
		Model:         "cli-model",
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "cli-model" {
		t.Fatalf("Model: want %q, got %q", "cli-model", cfg.Model)
	}
}

// TestLoad_InvalidAgent checks that an invalid --agent value returns an error.
func TestLoad_InvalidAgent(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	_, err := Load(LoadOptions{
		Agent:         "invalid-agent",
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for invalid --agent, got nil")
	}
}

// TestLoad_InvalidAgentFromYAML checks that an invalid agent.default in YAML returns error.
func TestLoad_InvalidAgentFromYAML(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwd := filepath.Join(ucd, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	writeConfig(t, filepath.Join(cwd, "shmorby.yaml"), `
agent:
  default: invalid-agent
`)
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	_, err = Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for invalid agent.default from YAML, got nil")
	}
}

// TestLoad_ValidAgent checks that valid --agent values are accepted.
func TestLoad_ValidAgent(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	for _, agent := range []string{"operate", "diagnose"} {
		cfg, err := Load(LoadOptions{
			Agent:         agent,
			SystemConfig:  "/nonexistent-sys-config",
			UserConfigDir: ucd,
		})
		if err != nil {
			t.Fatalf("Load(Agent=%q): %v", agent, err)
		}
		if cfg.Agent.Default != agent {
			t.Fatalf("Agent.Default: got %q want %q", cfg.Agent.Default, agent)
		}
	}
}

// TestLoad_UserConfigInMergeChain checks that user config overrides system
// defaults and cwd yaml overrides user config.
func TestLoad_UserConfigInMergeChain(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")

	sysCfg := filepath.Join(ucd, "sys", "config.yaml")
	writeConfig(t, sysCfg, `
provider: openrouter
model: sys-model
ollama:
  base_url: http://sys:11434
`)

	userCfgDir := filepath.Join(ucd, "user")
	writeConfig(t, filepath.Join(userCfgDir, "shmorby", "config.yaml"), `
model: user-model
`)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwd := filepath.Join(ucd, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	writeConfig(t, filepath.Join(cwd, "shmorby.yaml"), `
provider: ollama
`)
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	cfg, err := Load(LoadOptions{
		SystemConfig:  sysCfg,
		UserConfigDir: userCfgDir,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// cwd yaml overrides user config and system config.
	if cfg.Provider != "ollama" {
		t.Fatalf("Provider: got %q want %q", cfg.Provider, "ollama")
	}
	// user config overrides system config.
	if cfg.Model != "user-model" {
		t.Fatalf("Model: got %q want %q", cfg.Model, "user-model")
	}
	// system config values preserved when not overridden.
	if cfg.Ollama.BaseURL != "http://sys:11434" {
		t.Fatalf("Ollama.BaseURL: got %q want %q", cfg.Ollama.BaseURL, "http://sys:11434")
	}
}

// TestLoad_ConfigInvalidProviderWithLine checks that an invalid provider in
// --config reports the YAML line number.
func TestLoad_ConfigInvalidProviderWithLine(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "myconfig.yaml")
	writeConfig(t, cfgPath, `
provider: bogus
`)
	_, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
	if !strings.Contains(err.Error(), cfgPath+":"+strconv.Itoa(2)) {
		t.Errorf("want error with line 2, got:\n%s", err)
	}
}

// TestLoad_ConfigInvalidAgentWithLine tests that an invalid agent.default in
// the --config file reports the YAML line number.
func TestLoad_ConfigInvalidAgentWithLine(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "myconfig.yaml")
	writeConfig(t, cfgPath, `
agent:
  default: invalid-agent
`)
	_, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for invalid agent, got nil")
	}
	if !strings.Contains(err.Error(), cfgPath+":"+strconv.Itoa(3)) {
		t.Errorf("want error with line 3, got:\n%s", err)
	}
}

// TestLoad_ConfigValidFileNoEarlyError checks that a --config file without
// provider/agent keys does not trigger early validation errors.
func TestLoad_ConfigValidFileNoEarlyError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "myconfig.yaml")
	writeConfig(t, cfgPath, `
ollama:
  base_url: http://custom:11434
`)
	cfg, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Ollama.BaseURL != "http://custom:11434" {
		t.Fatalf("Ollama.BaseURL: got %q want %q",
			cfg.Ollama.BaseURL, "http://custom:11434")
	}
}

// TestLoad_MalformedYAML checks that invalid YAML in --config returns a wrapped error.
func TestLoad_MalformedYAML(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	badPath := filepath.Join(ucd, "bad.yaml")
	if err := os.MkdirAll(ucd, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(badPath, []byte(`
provider: ollama
  model: broken-indent
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(LoadOptions{
		ConfigFile:    badPath,
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

// TestLoad_MemoryEmbeddingParsed checks embedding config is parsed.
func TestLoad_MemoryEmbeddingParsed(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shmorby.yaml")
	writeConfig(t, cfgPath, `
memory:
  embedding:
    provider: ollama
    model: nomic-embed-text
    base_url: http://custom:11434
`)

	cfg, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Memory.Embedding.Provider != "ollama" {
		t.Errorf("Provider: want ollama, got %s",
			cfg.Memory.Embedding.Provider)
	}
	if cfg.Memory.Embedding.Model != "nomic-embed-text" {
		t.Errorf("Model: want nomic-embed-text, got %s",
			cfg.Memory.Embedding.Model)
	}
	if cfg.Memory.Embedding.BaseURL != "http://custom:11434" {
		t.Errorf("BaseURL: want http://custom:11434, got %s",
			cfg.Memory.Embedding.BaseURL)
	}
}

// TestLoad_ToolsTimeoutDefault checks Tools.Timeout defaults to 120.
func TestLoad_ToolsTimeoutDefault(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tools.Timeout != 120 {
		t.Errorf("Tools.Timeout: want 120, got %d", cfg.Tools.Timeout)
	}
}

// TestLoad_ToolsTimeoutFromYAML checks Tools.Timeout can be set via YAML.
func TestLoad_ToolsTimeoutFromYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shmorby.yaml")
	writeConfig(t, cfgPath, `
tools:
  timeout: 300
`)
	cfg, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tools.Timeout != 300 {
		t.Errorf("Tools.Timeout: want 300, got %d", cfg.Tools.Timeout)
	}
}

// TestLoad_MaxToolOutputLinesDefault checks MaxToolOutputLines defaults to 0.
func TestLoad_MaxToolOutputLinesDefault(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context.MaxToolOutputLines != 0 {
		t.Errorf("MaxToolOutputLines: want 0, got %d", cfg.Context.MaxToolOutputLines)
	}
}

// TestLoad_MaxToolOutputLinesFromYAML checks max_tool_output_lines from YAML.
func TestLoad_MaxToolOutputLinesFromYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shmorby.yaml")
	writeConfig(t, cfgPath, `
context:
  max_tool_output_lines: 200
`)
	cfg, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context.MaxToolOutputLines != 200 {
		t.Errorf("MaxToolOutputLines: want 200, got %d", cfg.Context.MaxToolOutputLines)
	}
}

// TestLoad_MemoryEmbeddingDefaults checks empty embedding config.
func TestLoad_MemoryEmbeddingDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Memory.Embedding.Provider != "" {
		t.Errorf("Provider: want empty, got %s",
			cfg.Memory.Embedding.Provider)
	}
}

// TestPermissionDefaults_InteractiveTrue checks that permission
// interactive defaults to true when not specified in config.
func TestPermissionDefaults_InteractiveTrue(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Permission.Interactive {
		t.Error("Permission.Interactive: want true by default")
	}
	if cfg.Permission.Presets != nil {
		t.Error("Permission.Presets: want nil by default")
	}
	if cfg.Permission.Rules != nil {
		t.Error("Permission.Rules: want nil by default")
	}
}

// TestLoad_DefaultWorkdir checks Scope.Workdir defaults to xdg.DefaultWorkDir().
func TestLoad_DefaultWorkdir(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Scope.Workdir == "" {
		t.Error("Scope.Workdir: want non-empty default")
	}
}

// TestLoad_DefaultDBPath checks Memory.DBPath uses xdg.UserDataDir().
func TestLoad_DefaultDBPath(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Memory.DBPath == "" {
		t.Error("Memory.DBPath: want non-empty default")
	}
	if cfg.Memory.DBPath == "~/.local/share/shmorby/memory.db" {
		t.Error("Memory.DBPath: want xdg-based path, not hardcoded tilde path")
	}
}

// TestLoad_EachLayerValidatesWithLine checks that every YAML layer validates
// with file:line reporting.
func TestLoad_EachLayerValidatesWithLine(t *testing.T) {
	t.Run("system layer", func(t *testing.T) {
		dir := t.TempDir()
		sysPath := filepath.Join(dir, "sys", "config.yaml")
		writeConfig(t, sysPath, `provider: bogus
`)
		_, err := Load(LoadOptions{
			SystemConfig:  sysPath,
			UserConfigDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for invalid provider in system config")
		}
		if !strings.Contains(err.Error(), "sys/config.yaml:1") {
			t.Errorf("want error with file:line, got:\n%s", err)
		}
	})

	t.Run("user layer", func(t *testing.T) {
		dir := t.TempDir()
		userDir := filepath.Join(dir, "user")
		writeConfig(t, filepath.Join(userDir, "shmorby", "config.yaml"), `provider: bogus
`)
		_, err := Load(LoadOptions{
			SystemConfig:  "/nonexistent",
			UserConfigDir: userDir,
		})
		if err == nil {
			t.Fatal("expected error for invalid provider in user config")
		}
		if !strings.Contains(err.Error(), "config.yaml:1") {
			t.Errorf("want error with file:line, got:\n%s", err)
		}
	})

	t.Run("cwd layer", func(t *testing.T) {
		dir := t.TempDir()
		cwdDir := filepath.Join(dir, "cwd")
		if err := os.MkdirAll(cwdDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeConfig(t, filepath.Join(cwdDir, "shmorby.yaml"), `provider: bogus
`)
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(cwdDir); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(oldWd) }()

		_, err = Load(LoadOptions{
			SystemConfig:  "/nonexistent",
			UserConfigDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for invalid provider in cwd config")
		}
		if !strings.Contains(err.Error(), "shmorby.yaml:1") {
			t.Errorf("want error with file:line, got:\n%s", err)
		}
	})
}

// TestLoad_MissingAPIKeyForProvider_ReturnsError checks that missing API key
// for the chosen provider returns an error.
func TestLoad_MissingAPIKeyForProvider_ReturnsError(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"openai", "openai.api_key is not set"},
		{"openrouter", "openrouter.api_key is not set"},
		{"opencode_zen", "opencode_zen.api_key is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			_, err := Load(LoadOptions{
				Provider:      tt.provider,
				SystemConfig:  "/nonexistent",
				UserConfigDir: t.TempDir(),
			})
			if err == nil {
				t.Fatal("expected error for missing API key, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("want error containing %q, got:\n%s", tt.want, err)
			}
		})
	}
}

// TestValidateConfig_InvalidPermission_ReturnsError checks permission validation.
func TestValidateConfig_InvalidPermission_ReturnsError(t *testing.T) {
	cfg := defaultConfig()
	cfg.Permission.Shell = "maybe"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid permission level, got nil")
	}
}

// TestValidateConfig_InvalidTokenEstimator_ReturnsError checks token estimator validation.
func TestValidateConfig_InvalidTokenEstimator_ReturnsError(t *testing.T) {
	cfg := defaultConfig()
	cfg.Context.TokenEstimator = "gpt3"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid token_estimator, got nil")
	}
}

// TestValidateConfig_InvalidContextMode_ReturnsError checks context mode validation.
func TestValidateConfig_InvalidContextMode_ReturnsError(t *testing.T) {
	cfg := defaultConfig()
	cfg.Context.Mode = "ultra"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for invalid context.mode, got nil")
	}
}

// TestValidateConfig_NegativeTimeout_ReturnsError checks negative timeout validation.
func TestValidateConfig_NegativeTimeout_ReturnsError(t *testing.T) {
	cfg := defaultConfig()
	cfg.Tools.Timeout = -1
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for negative timeout, got nil")
	}
}

// TestValidateConfig_ValidConfig_ReturnsNil checks valid config passes validation.
func TestValidateConfig_ValidConfig_ReturnsNil(t *testing.T) {
	cfg := defaultConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected nil error for valid config, got: %v", err)
	}
}
