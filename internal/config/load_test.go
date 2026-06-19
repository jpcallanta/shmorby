package config

import (
	"os"
	"path/filepath"
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
	if cfg.Agent.Shell != "bash" {
		t.Fatalf("Agent.Shell: got %q want %q", cfg.Agent.Shell, "bash")
	}
	if !cfg.Tools.Shell.Enabled {
		t.Fatal("Tools.Shell.Enabled: want true")
	}
	if cfg.Permission.Shell != "allow" {
		t.Fatalf("Permission.Shell: got %q want %q", cfg.Permission.Shell, "allow")
	}
	if cfg.Permission.Sudo != "ask" {
		t.Fatalf("Permission.Sudo: got %q want %q", cfg.Permission.Sudo, "ask")
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

// TestLoad_EnvProviderOverridesFile checks that SHMORBY_PROVIDER overrides the YAML value.
func TestLoad_EnvProviderOverridesFile(t *testing.T) {
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
	if cfg.Provider != "openrouter" {
		t.Fatalf("Provider: got %q want %q", cfg.Provider, "openrouter")
	}
}

// TestLoad_EnvInvalidProvider checks that an invalid provider from env returns error.
func TestLoad_EnvInvalidProvider(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	t.Setenv("SHMORBY_PROVIDER", "invalid")
	_, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for invalid provider from env, got nil")
	}
}

// TestLoad_CLIModelOverridesEnv checks that --model CLI flag overrides env.
func TestLoad_CLIModelOverridesEnv(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	t.Setenv("SHMORBY_MODEL", "env-model")

	cfg, err := Load(LoadOptions{
		Model:         "cli-model",
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "cli-model" {
		t.Fatalf("Model: got %q want %q", cfg.Model, "cli-model")
	}
}

// TestLoad_EnvOllamaBaseURL checks OLLAMA_BASE_URL overrides the default.
func TestLoad_EnvOllamaBaseURL(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	t.Setenv("OLLAMA_BASE_URL", "http://ollama.internal:11434")

	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Ollama.BaseURL != "http://ollama.internal:11434" {
		t.Fatalf("Ollama.BaseURL: got %q want %q", cfg.Ollama.BaseURL, "http://ollama.internal:11434")
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

// TestLoad_EmptyProviderEnv checks that SHMORBY_PROVIDER="" is rejected.
func TestLoad_EmptyProviderEnv(t *testing.T) {
	ucd := filepath.Join(t.TempDir(), "shmorby")
	t.Setenv("SHMORBY_PROVIDER", "")

	_, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent-sys-config",
		UserConfigDir: ucd,
	})
	if err == nil {
		t.Fatal("expected error for empty SHMORBY_PROVIDER, got nil")
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

// TestLoad_ToolsTimeoutFromEnv checks SHMORBY_TOOLS_TIMEOUT env var works.
func TestLoad_ToolsTimeoutFromEnv(t *testing.T) {
	t.Setenv("SHMORBY_TOOLS_TIMEOUT", "60")
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tools.Timeout != 60 {
		t.Errorf("Tools.Timeout: want 60, got %d", cfg.Tools.Timeout)
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

// TestLoad_MaxToolOutputLinesFromEnv checks SHMORBY_TOOL_OUTPUT_MAX_LINES env var.
func TestLoad_MaxToolOutputLinesFromEnv(t *testing.T) {
	t.Setenv("SHMORBY_TOOL_OUTPUT_MAX_LINES", "100")
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context.MaxToolOutputLines != 100 {
		t.Errorf("MaxToolOutputLines: want 100, got %d", cfg.Context.MaxToolOutputLines)
	}
}

// TestLoad_MaxToolOutputLinesEnvOverridesYAML checks env overrides YAML.
func TestLoad_MaxToolOutputLinesEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shmorby.yaml")
	writeConfig(t, cfgPath, `
context:
  max_tool_output_lines: 50
`)
	t.Setenv("SHMORBY_TOOL_OUTPUT_MAX_LINES", "300")
	cfg, err := Load(LoadOptions{
		ConfigFile:    cfgPath,
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context.MaxToolOutputLines != 300 {
		t.Errorf("MaxToolOutputLines: want 300, got %d", cfg.Context.MaxToolOutputLines)
	}
}

// TestLoad_MaxToolOutputLinesFromEnvInvalid checks invalid env value is ignored.
func TestLoad_MaxToolOutputLinesFromEnvInvalid(t *testing.T) {
	t.Setenv("SHMORBY_TOOL_OUTPUT_MAX_LINES", "not-a-number")
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

// TestPermissionDefaults_InteractiveFalse checks that permission
// interactive defaults to false when not specified in config.
func TestPermissionDefaults_InteractiveFalse(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemConfig:  "/nonexistent",
		UserConfigDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Permission.Interactive {
		t.Error("Permission.Interactive: want false by default")
	}
	if cfg.Permission.Presets != nil {
		t.Error("Permission.Presets: want nil by default")
	}
	if cfg.Permission.Rules != nil {
		t.Error("Permission.Rules: want nil by default")
	}
}
