package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
	"shmorby/internal/fileread"
)

// Migrate reads a legacy config file, merges in any missing default fields
// and writes it back. Comments from the original file are preserved.
// Empty files are treated as a blank config (filled with defaults).
// Uses size-limited reads to prevent OOM from oversized files (issue #46).
func Migrate(src, dst string) error {
	data, err := fileread.ReadFileLimited(src, 0)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	// Unmarshal original into yaml.Node to preserve comments and structure.
	// Empty files produce no yaml.Node content — treat as blank config.
	var original yaml.Node
	if len(bytes.TrimSpace(data)) == 0 {
		// Initialize as a blank mapping so merge has a target
		original.Kind = yaml.DocumentNode
		original.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	} else {
		if err := yaml.Unmarshal(data, &original); err != nil {
			return fmt.Errorf("parse source YAML: %w", err)
		}
	}

	// Get canonical defaults, filtering zero-values so empty api_key etc.
	// are not injected into the migrated output.
	defaults := DefaultConfig()
	defaultData, err := yaml.Marshal(filterZeroValues(reflect.ValueOf(defaults)))
	if err != nil {
		return fmt.Errorf("marshal defaults: %w", err)
	}
	var defaultNode yaml.Node
	if err := yaml.Unmarshal(defaultData, &defaultNode); err != nil {
		return fmt.Errorf("parse defaults YAML: %w", err)
	}

	// Resolve past DocumentNode and merge recursively
	origMap := resolveMapping(&original)
	defMap := resolveMapping(&defaultNode)
	if origMap != nil && defMap != nil {
		mergeNodes(origMap, defMap)
	}

	// Marshal back, preserving comments from the original
	out, err := yaml.Marshal(&original)
	if err != nil {
		return fmt.Errorf("marshal merged config: %w", err)
	}

	// Preserve original file permissions (or use restrictive default)
	perm := os.FileMode(0600)
	if info, err := os.Stat(src); err == nil {
		perm = info.Mode().Perm()
	}

	// Ensure destination directory exists
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Write to temp file first, then rename for atomicity
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, out, perm); err != nil {
		os.Remove(tmp) // cleanup on failure
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename config: %w", err)
	}

	return nil
}

// DryMigrate reports which fields (including nested) would be added but
// writes nothing.
// Uses size-limited reads to prevent OOM from oversized files (issue #46).
func DryMigrate(src, dst string) error {
	data, err := fileread.ReadFileLimited(src, 0)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	var original yaml.Node
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &original); err != nil {
			return fmt.Errorf("parse source YAML: %w", err)
		}
	} else {
		original.Kind = yaml.DocumentNode
		original.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}

	defaults := DefaultConfig()
	defaultData, err := yaml.Marshal(filterZeroValues(reflect.ValueOf(defaults)))
	if err != nil {
		return fmt.Errorf("marshal defaults: %w", err)
	}
	var defaultNode yaml.Node
	if err := yaml.Unmarshal(defaultData, &defaultNode); err != nil {
		return fmt.Errorf("parse defaults YAML: %w", err)
	}

	origMap := resolveMapping(&original)
	defMap := resolveMapping(&defaultNode)
	missing := findMissingKeys(origMap, defMap)
	if len(missing) > 0 {
		fmt.Println("Missing fields that would be added:")
		for _, k := range missing {
			fmt.Printf("  + %s\n", k)
		}
	} else {
		fmt.Println("No missing fields — config is up to date.")
	}

	return nil
}

// ValidateFile validates a config file for common issues:
// provider, agent, permission levels, context mode, timeout values,
// required paths, and unknown top-level keys.
// Uses size-limited reads to prevent OOM from oversized files (issue #46).
func ValidateFile(path string) error {
	data, err := fileread.ReadFileLimited(path, 0)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	var errs []string

	// Provider
	if err := ValidateProvider(cfg.Provider); err != nil {
		errs = append(errs, err.Error())
	}

	// Agent
	if cfg.Agent.Default != "" {
		if err := ValidateAgent(cfg.Agent.Default); err != nil {
			errs = append(errs, err.Error())
		}
	}

	// Permission levels
	for _, field := range []struct{ name, value string }{
		{"permission.shell", cfg.Permission.Shell},
		{"permission.ssh", cfg.Permission.SSH},
		{"permission.sudo", cfg.Permission.Sudo},
		{"permission.aws", cfg.Permission.AWS},
		{"permission.mcp", cfg.Permission.MCP},
		{"permission.task", cfg.Permission.Task},
	} {
		if field.value != "" {
			if err := ValidatePermissionLevel(field.name, field.value); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	// Context mode
	if cfg.Context.Mode != "" {
		if err := ValidateContextMode(cfg.Context.Mode); err != nil {
			errs = append(errs, err.Error())
		}
	}

	// Timeouts — must be non-negative
	if cfg.Tools.Timeout < 0 {
		errs = append(errs, fmt.Sprintf("tools.timeout must be >= 0, got %d", cfg.Tools.Timeout))
	}
	if cfg.OpenAI.Timeout < 0 {
		errs = append(errs, fmt.Sprintf("openai.timeout must be >= 0, got %d", cfg.OpenAI.Timeout))
	}

	// Remote providers need an API key
	switch cfg.Provider {
	case "openrouter":
		if cfg.OpenRouter.APIKey == "" {
			errs = append(errs, "openrouter.api_key is empty (required for openrouter provider)")
		}
	case "opencode_zen":
		if cfg.OpencodeZen.APIKey == "" {
			errs = append(errs, "opencode_zen.api_key is empty (required for opencode_zen provider)")
		}
	case "openai":
		if cfg.OpenAI.APIKey == "" && cfg.OpenAI.APIKeyEnv == "" {
			errs = append(errs, "openai.api_key and openai.api_key_env are both empty (need one for openai provider)")
		}
	}

	// Memory DB path
	if cfg.Memory.Enabled && cfg.Memory.DBPath == "" {
		errs = append(errs, "memory.db_path is empty but memory is enabled")
	}

	// Audit DB path
	if cfg.Audit.Enabled && cfg.Audit.DBPath == "" {
		errs = append(errs, "audit.db_path is empty but audit is enabled")
	}

	// Unknown top-level keys — spec §339-348
	knownKeys := knownTopLevelKeys()
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err == nil {
		for k := range parsed {
			if !knownKeys[k] {
				errs = append(errs, fmt.Sprintf("unknown key: %q", k))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// ShowDefaults returns the default config as YAML with zero-value fields
// omitted.
func ShowDefaults() string {
	defaults := DefaultConfig()
	out, _ := yaml.Marshal(filterZeroValues(reflect.ValueOf(defaults)))
	return string(out)
}

// knownTopLevelKeys returns the set of YAML tag names from the Config struct.
func knownTopLevelKeys() map[string]bool {
	keys := make(map[string]bool)
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		keys[name] = true
	}
	return keys
}

// resolveMapping drills past any DocumentNodes to find the first MappingNode.
// Returns nil if no MappingNode is found.
func resolveMapping(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

// mergeNodes adds missing keys from src into dst (both MappingNodes).
// Existing keys in dst are left untouched. Recurses into nested mappings.
func mergeNodes(dst, src *yaml.Node) {
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		return
	}

	// Build index of existing keys in dst and their value positions
	type entry struct {
		val *yaml.Node
		idx int
	}
	dstIndex := make(map[string]entry)
	for i := 0; i < len(dst.Content)-1; i += 2 {
		if dst.Content[i].Kind == yaml.ScalarNode {
			dstIndex[dst.Content[i].Value] = entry{dst.Content[i+1], i + 1}
		}
	}

	// Add missing key-value pairs and recurse into matching mappings
	for i := 0; i < len(src.Content)-1; i += 2 {
		key := src.Content[i]
		val := src.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		if existing, ok := dstIndex[key.Value]; !ok {
			dst.Content = append(dst.Content, key, val)
		} else if existing.val.Kind == yaml.MappingNode && val.Kind == yaml.MappingNode {
			mergeNodes(existing.val, val)
		}
	}
}

// findMissingKeys returns a list of dotted-path keys present in defaults
// but absent from the user's config. Handles nested mappings recursively.
func findMissingKeys(user, defaults *yaml.Node) []string {
	return findMissingKeysPrefix(user, defaults, "")
}

func findMissingKeysPrefix(user, defaults *yaml.Node, prefix string) []string {
	user = resolveMapping(user)
	defaults = resolveMapping(defaults)
	if user == nil || defaults == nil {
		return nil
	}

	userKeys := make(map[string]*yaml.Node)
	for i := 0; i < len(user.Content)-1; i += 2 {
		if user.Content[i].Kind == yaml.ScalarNode {
			userKeys[user.Content[i].Value] = user.Content[i+1]
		}
	}

	var missing []string
	for i := 0; i < len(defaults.Content)-1; i += 2 {
		dk := defaults.Content[i]
		dv := defaults.Content[i+1]
		if dk.Kind != yaml.ScalarNode {
			continue
		}
		name := dk.Value
		if prefix != "" {
			name = prefix + "." + name
		}

		if uv, ok := userKeys[dk.Value]; !ok {
			missing = append(missing, name)
		} else if uv.Kind == yaml.MappingNode && dv.Kind == yaml.MappingNode {
			missing = append(missing, findMissingKeysPrefix(uv, dv, name)...)
		}
	}
	return missing
}

// filterZeroValues returns a copy of v with zero-value struct fields removed.
// Works recursively for nested structs. Non-struct types are returned as-is.
func filterZeroValues(v reflect.Value) interface{} {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Struct:
		result := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fieldType := t.Field(i)

			if !fieldType.IsExported() {
				continue
			}

			yamlTag := fieldType.Tag.Get("yaml")
			if yamlTag == "" || yamlTag == "-" {
				continue
			}
			name := strings.Split(yamlTag, ",")[0]

			if field.IsZero() {
				continue
			}

			result[name] = filterZeroValues(field)
		}
		return result

	case reflect.Map:
		result := make(map[string]interface{})
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			if !val.IsNil() && !val.IsZero() {
				result[fmt.Sprintf("%v", key.Interface())] = filterZeroValues(val)
			}
		}
		return result

	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		result := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = filterZeroValues(v.Index(i))
		}
		return result

	default:
		return v.Interface()
	}
}
