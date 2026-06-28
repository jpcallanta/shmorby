package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"shmorby/internal/xdg"
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
	// Empty means xdg.SystemConfigDir()/config.yaml.
	SystemConfig string

	// UserConfigDir overrides the user config directory for testing.
	// Empty means xdg.UserConfigDir().
	UserConfigDir string
}

// Load loads configuration with layered precedence (later overrides earlier).
//
// Order (later wins):
//  1. /etc/shmorby/config.yaml (Unix) or %ProgramData%\shmorby\config.yaml
//     (Windows) — skipped if missing
//  2. $XDG_CONFIG_HOME/shmorby/config.yaml (Unix) or %APPDATA%\shmorby\config.yaml
//     (Windows) — skipped if missing
//  3. --config file                         (error if set but missing)
//  4. ./shmorby.yaml in cwd                (skip if missing)
//  5. CLI flags (Provider, Model, Agent)   (always win)
func Load(opts LoadOptions) (Config, error) {
	cfg := defaultConfig()

	// 1: System config.
	sysConfig := filepath.Join(xdg.SystemConfigDir(), "config.yaml")
	if opts.SystemConfig != "" {
		sysConfig = opts.SystemConfig
	}
	b, err := loadYAMLIfExists(&cfg, sysConfig)
	if err != nil {
		return Config{}, fmt.Errorf("load system config: %w", err)
	}
	if b != nil {
		if err := validateConfigFile(b, sysConfig); err != nil {
			return Config{}, fmt.Errorf("load system config: %w", err)
		}
	}

	// 2: User config.
	userPath := userConfigPath(opts.UserConfigDir)
	b, err = loadYAMLIfExists(&cfg, userPath)
	if err != nil {
		return Config{}, fmt.Errorf("load user config: %w", err)
	}
	if b != nil {
		if err := validateConfigFile(b, userPath); err != nil {
			return Config{}, fmt.Errorf("load user config: %w", err)
		}
	}

	// 3: --config file.
	if opts.ConfigFile != "" {
		b, err := os.ReadFile(opts.ConfigFile)
		if err != nil {
			return Config{}, fmt.Errorf("load --config %q: read %w", opts.ConfigFile, err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("load --config %q: %w", opts.ConfigFile, err)
		}
		if err := validateConfigFile(b, opts.ConfigFile); err != nil {
			return Config{}, fmt.Errorf("load --config %q: %w", opts.ConfigFile, err)
		}
	}

	// 4: ./shmorby.yaml in cwd.
	b, err = loadYAMLIfExists(&cfg, "./shmorby.yaml")
	if err != nil {
		return Config{}, fmt.Errorf("load ./shmorby.yaml: %w", err)
	}
	if b != nil {
		if err := validateConfigFile(b, "./shmorby.yaml"); err != nil {
			return Config{}, fmt.Errorf("load ./shmorby.yaml: %w", err)
		}
	}

	// 5: CLI overrides.
	if opts.Provider != "" {
		cfg.Provider = opts.Provider
	}
	if opts.Model != "" {
		cfg.Model = opts.Model
	}
	if opts.Agent != "" {
		cfg.Agent.Default = opts.Agent
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}

	return cfg, nil
}

// Reads path into cfg if it exists; skips silently when missing.
// Returns the raw bytes for line-level validation.
func loadYAMLIfExists(cfg *Config, path string) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat config: %w", err)
	}
	return loadYAML(cfg, path)
}

// Reads a single YAML file into cfg. Unknown fields are ignored.
func loadYAML(cfg *Config, path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return b, yaml.Unmarshal(b, cfg)
}

// Validates provider and agent values in a YAML config file,
// returning errors annotated with the YAML line number on failure.
func validateConfigFile(b []byte, path string) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil
	}

	var fileCfg Config
	if err := doc.Decode(&fileCfg); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	root := doc.Content[0]

	if fileCfg.Provider != "" {
		if err := ValidateProvider(fileCfg.Provider); err != nil {
			line := findMappingKeyLine(root, "provider")
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	if fileCfg.Agent.Default != "" {
		if err := ValidateAgent(fileCfg.Agent.Default); err != nil {
			line := findNestedKeyLine(root, "agent", "default")
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}

	return nil
}

// Returns the line number of key in a YAML mapping node.
// Returns 0 if key is not found.
func findMappingKeyLine(node *yaml.Node, key string) int {
	if node.Kind != yaml.MappingNode {
		return 0
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i].Line
		}
	}
	return 0
}

// Traverses nested YAML mapping nodes following keys and
// returns the line number of the final key. Returns 0 if the path is not found.
func findNestedKeyLine(node *yaml.Node, keys ...string) int {
	if len(keys) == 0 {
		return 0
	}
	if node.Kind != yaml.MappingNode {
		return 0
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == keys[0] {
			if len(keys) == 1 {
				return node.Content[i].Line
			}
			return findNestedKeyLine(node.Content[i+1], keys[1:]...)
		}
	}
	return 0
}

// Returns the XDG user config path, or a dir override if non-empty.
func userConfigPath(dirOverride string) string {
	if dirOverride != "" {
		return filepath.Join(dirOverride, "shmorby", "config.yaml")
	}
	return filepath.Join(xdg.UserConfigDir(), "config.yaml")
}

// Validates the merged config including cross-field checks.
func validateConfig(cfg Config) error {
	if err := ValidateProvider(cfg.Provider); err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	if err := ValidateAgent(cfg.Agent.Default); err != nil {
		return fmt.Errorf("agent.default: %w", err)
	}
	if err := ValidatePermissionLevel(
		"permission.shell", cfg.Permission.Shell,
	); err != nil {
		return err
	}
	if err := ValidatePermissionLevel(
		"permission.ssh", cfg.Permission.SSH,
	); err != nil {
		return err
	}
	if err := ValidatePermissionLevel(
		"permission.sudo", cfg.Permission.Sudo,
	); err != nil {
		return err
	}
	if err := ValidatePermissionLevel(
		"permission.aws", cfg.Permission.AWS,
	); err != nil {
		return err
	}
	if err := ValidateTokenEstimator(cfg.Context.TokenEstimator); err != nil {
		return fmt.Errorf("context.token_estimator: %w", err)
	}
	if err := ValidateContextMode(cfg.Context.Mode); err != nil {
		return fmt.Errorf("context.mode: %w", err)
	}
	if cfg.Tools.Timeout < 0 {
		return fmt.Errorf(
			"tools.timeout: must be >= 0, got %d",
			cfg.Tools.Timeout,
		)
	}
	if cfg.Agent.MaxToolIterations <= 0 {
		return fmt.Errorf(
			"agent.max_tool_iterations: must be > 0, got %d",
			cfg.Agent.MaxToolIterations,
		)
	}

	switch cfg.Provider {
	case "openai":
		if cfg.OpenAI.APIKey == "" {
			return fmt.Errorf(
				"provider is %q but openai.api_key is not set; "+
					"set it in your shmorby.yaml", cfg.Provider,
			)
		}
	case "openrouter":
		if cfg.OpenRouter.APIKey == "" {
			return fmt.Errorf(
				"provider is %q but openrouter.api_key is not "+
					"set; set it in your shmorby.yaml",
				cfg.Provider,
			)
		}
	case "opencode_zen":
		if cfg.OpencodeZen.APIKey == "" {
			return fmt.Errorf(
				"provider is %q but opencode_zen.api_key is not "+
					"set; set it in your shmorby.yaml",
				cfg.Provider,
			)
		}
	}

	return nil
}
