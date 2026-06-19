package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// LoadOptions controls config source paths and CLI overrides.
type LoadOptions struct {
	// ConfigFile is the --config flag value (error if set but missing).
	ConfigFile string

	// Provider, Model, Agent are CLI flag overrides.
	Provider string
	Model    string
	Agent    string

	// SystemConfig overrides the system-level config path for testing.
	// Empty means /etc/shmorby/config.yaml.
	SystemConfig string

	// UserConfigDir overrides the XDG config directory for testing.
	// Empty means $XDG_CONFIG_HOME/shmorby then ~/.config/shmorby.
	UserConfigDir string
}

// Load loads configuration with layered precedence (later overrides earlier).
//
// Order (later wins):
//  1. /etc/shmorby/config.yaml             (skip if missing)
//  2. $XDG_CONFIG_HOME/shmorby/config.yaml (skip if missing)
//  3. --config file                         (error if set but missing)
//  4. ./shmorby.yaml in cwd                (skip if missing)
//  5. Env vars: SHMORBY_PROVIDER, SHMORBY_MODEL, OLLAMA_BASE_URL,
//     OPENROUTER_API_KEY, OPENCODE_ZEN_API_KEY, OPENCODE_ZEN_BASE_URL
//  6. CLI flags (Provider, Model, Agent)   (always win)
func Load(opts LoadOptions) (Config, error) {
	cfg := defaultConfig()

	// 1: System config.
	sysConfig := "/etc/shmorby/config.yaml"
	if opts.SystemConfig != "" {
		sysConfig = opts.SystemConfig
	}
	if err := loadYAMLIfExists(&cfg, sysConfig); err != nil {
		return Config{}, fmt.Errorf("load system config: %w", err)
	}

	// 2: User config.
	if err := loadYAMLIfExists(&cfg, userConfigPath(opts.UserConfigDir)); err != nil {
		return Config{}, fmt.Errorf("load user config: %w", err)
	}

	// 3: --config file.
	if opts.ConfigFile != "" {
		if err := loadYAML(&cfg, opts.ConfigFile); err != nil {
			return Config{}, fmt.Errorf("load --config %q: %w", opts.ConfigFile, err)
		}
	}

	// 4: ./shmorby.yaml in cwd.
	if err := loadYAMLIfExists(&cfg, "./shmorby.yaml"); err != nil {
		return Config{}, fmt.Errorf("load ./shmorby.yaml: %w", err)
	}

	// 5: Env overrides.
	applyEnvOverrides(&cfg)

	// 6: CLI overrides.
	if opts.Provider != "" {
		cfg.Provider = opts.Provider
	}
	if opts.Model != "" {
		cfg.Model = opts.Model
	}
	if opts.Agent != "" {
		cfg.Agent.Default = opts.Agent
	}

	if err := validateProvider(cfg.Provider); err != nil {
		return Config{}, fmt.Errorf("validate provider: %w", err)
	}
	if err := validateAgent(cfg.Agent.Default); err != nil {
		return Config{}, fmt.Errorf("validate agent: %w", err)
	}

	return cfg, nil
}

// loadYAMLIfExists reads path into cfg if it exists; skips silently when missing.
func loadYAMLIfExists(cfg *Config, path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat config: %w", err)
	}
	return loadYAML(cfg, path)
}

// loadYAML reads a single YAML file into cfg. Unknown fields are ignored.
func loadYAML(cfg *Config, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	return yaml.Unmarshal(b, cfg)
}

// userConfigPath returns the XDG user config path, or a dir override if non-empty.
func userConfigPath(dirOverride string) string {
	if dirOverride != "" {
		return filepath.Join(dirOverride, "shmorby", "config.yaml")
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg != "" {
		return filepath.Join(xdg, "shmorby", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "shmorby", "config.yaml")
	}
	return filepath.Join(home, ".config", "shmorby", "config.yaml")
}

// applyEnvOverrides reads known environment variables into cfg.
//
//nolint:funlen
func applyEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("SHMORBY_PROVIDER"); ok {
		cfg.Provider = v
	}
	if v, ok := os.LookupEnv("SHMORBY_MODEL"); ok {
		cfg.Model = v
	}
	if v, ok := os.LookupEnv("OLLAMA_BASE_URL"); ok {
		cfg.Ollama.BaseURL = v
	}
	if v, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
		cfg.OpenRouter.APIKey = v
	}
	if v, ok := os.LookupEnv("OPENCODE_ZEN_API_KEY"); ok {
		cfg.OpencodeZen.APIKey = v
	}
	if v, ok := os.LookupEnv("OPENCODE_ZEN_BASE_URL"); ok {
		cfg.OpencodeZen.BaseURL = v
	}
	if v, ok := os.LookupEnv("OPENAI_API_KEY"); ok {
		cfg.OpenAI.APIKey = v
	}
	if v, ok := os.LookupEnv("OPENAI_ORG_ID"); ok {
		cfg.OpenAI.Organization = v
	}
	if v, ok := os.LookupEnv("OPENAI_BASE_URL"); ok {
		cfg.OpenAI.BaseURL = v
	}
	if v, ok := os.LookupEnv("SHMORBY_TOOLS_TIMEOUT"); ok {
		if t, err := strconv.Atoi(v); err == nil {
			cfg.Tools.Timeout = t
		}
	}
	if v, ok := os.LookupEnv("OPENAI_TIMEOUT"); ok {
		if t, err := strconv.Atoi(v); err == nil {
			cfg.OpenAI.Timeout = t
		}
	}
	if v, ok := os.LookupEnv("SHMORBY_TOOL_OUTPUT_MAX_LINES"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Context.MaxToolOutputLines = n
		}
	}
	if v, ok := os.LookupEnv("SHMORBY_TOOL_OUTPUT_MAX_BYTES"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Context.MaxToolOutputBytes = n
		}
	}
}
