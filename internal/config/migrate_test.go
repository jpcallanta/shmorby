package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testLegacyConfig = `provider: openai
agent:
  default: diagnose
tools:
  timeout: 60
permission:
  shell: allow
`

const testUpToDateConfig = `provider: ollama
ollama:
  base_url: http://127.0.0.1:11434
openai:
  timeout: 120
opencode_zen:
  base_url: https://opencode.ai/zen
agent:
  default: operate
  max_tool_iterations: 20
tools:
  shell:
    enabled: true
  timeout: 120
  websearch:
    base_url: http://localhost:8888
    engine: searxng
permission:
  aws: ask
  interactive: true
  mcp: ask
  shell: ask
  ssh: ask
  sudo: ask
  task: ask
tui:
  fullscreen: true
  glamour:
    enabled: true
  logging:
    collapse: true
    collapse_threshold: 5
    default_level: info
    display_limit: 20
    enabled: true
    max_entries: 100
  nav:
    follow_mode: true
    history_size: 100
    keybinds:
      agent_cycle: tab
      agent_cycle_reverse: shift+tab
      agent_list: <leader>a
      app_exit: <leader>q
      command_list: ctrl+p
      editor_open: <leader>e
      history_search: ctrl+r
      leader: ctrl+x
      messages_copy: <leader>y
      model_list: <leader>m
      session_child_cycle: right
      session_child_cycle_reverse: left
      session_child_first: <leader>down
      session_compact: <leader>c
      session_export: <leader>x
      session_list: <leader>l
      session_new: <leader>n
      session_parent: up
      session_redo: <leader>r
      session_undo: <leader>u
      sidebar_toggle: <leader>b
      status_view: <leader>s
      theme_list: <leader>t
      tips_toggle: <leader>h
    leader_timeout: 2000
    scroll_lines_per_tick: 5
scope:
  workdir: ""
memory:
  auto_capture: true
  enabled: true
  max_entries: 10000
context:
  enabled: true
  fallback_context_window: 128000
  max_tool_output_tokens: 4096
  min_messages_to_compress: 6
  mode: auto
  threshold: 0.8
audit:
  async_buffer_size: 100
  enabled: true
  output_capture_max_bytes: 65536
  retention_days: 365
`

func TestDryMigrate_ShowsMissingFields(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(testLegacyConfig), 0644); err != nil {
		t.Fatal(err)
	}

	err := DryMigrate(src, dst)
	if err != nil {
		t.Fatalf("DryMigrate failed: %v", err)
	}

	// Source should not be modified
	data, _ := os.ReadFile(src)
	if !strings.Contains(string(data), "provider: openai") {
		t.Error("source was modified")
	}

	// Destination should not be created
	if _, err := os.Stat(dst); err == nil {
		t.Error("destination was created by dry run")
	}
}

func TestDryMigrate_UpToDate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(testUpToDateConfig), 0644); err != nil {
		t.Fatal(err)
	}

	err := DryMigrate(src, dst)
	if err != nil {
		t.Fatalf("DryMigrate failed: %v", err)
	}
}

func TestMigrate_AddsMissingFields(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	minimal := `provider: ollama
`
	if err := os.WriteFile(src, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}

	out := string(data)

	// Migrate should have added missing fields from defaults.
	// These are non-zero defaults that the minimal config didn't have.
	checks := []string{
		"ollama:",
		"base_url: http://127.0.0.1:11434",
		"opencode_zen:",
		"base_url: https://opencode.ai/zen",
		"agent:",
		"default: operate",
		"max_tool_iterations: 20",
		"tools:",
		"shell:",
		"enabled: true",
		"timeout: 120",
		"websearch:",
		"engine: searxng",
		"permission:",
		"aws: ask",
		"interactive: true",
		"mcp: ask",
		"shell: ask",
		"tui:",
		"fullscreen: true",
		"memory:",
		"enabled: true",
		"max_entries: 10000",
		"auto_capture: true",
		"context:",
		"enabled: true",
		"mode: auto",
		"threshold: 0.8",
		"audit:",
		"enabled: true",
		"output_capture_max_bytes: 65536",
		"retention_days: 365",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("missing expected field in output: %q", want)
		}
	}
}

func TestMigrate_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(testLegacyConfig), 0644); err != nil {
		t.Fatal(err)
	}

	err := Migrate(src, dst)
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}

	out := string(data)

	// Original fields preserved
	if !strings.Contains(out, "provider: openai") {
		t.Error("original provider=openai not preserved")
	}
	if !strings.Contains(out, "default: diagnose") {
		t.Error("original agent.default=diagnose not preserved")
	}
	if !strings.Contains(out, "timeout: 60") {
		t.Error("original tools.timeout=60 not preserved")
	}
	if !strings.Contains(out, "shell: allow") {
		t.Error("original permission.shell=allow not preserved")
	}
}

func TestMigrate_PreservesComments(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	srcContent := `# Shmorby configuration
provider: openai
agent:
  default: diagnose
`
	if err := os.WriteFile(src, []byte(srcContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(dst)
	if !strings.Contains(string(data), "# Shmorby configuration") {
		t.Error("comments were stripped")
	}
}

func TestMigrate_NoZeroValuesInjected(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	minimal := `provider: ollama
`
	if err := os.WriteFile(src, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(dst)
	out := string(data)

	// These zero-value fields must NOT appear in migrated output
	badFields := []string{
		"api_key: \"\"",
		"api_key_env: \"\"",
		"organization: \"\"",
		"base_url: \"\"",
		"model: \"\"",
		"models: {}",
		"presets: []",
		"rules: []",
		"instructions: []",
	}

	for _, bad := range badFields {
		if strings.Contains(out, bad) {
			t.Errorf("zero-value field leaked into migrated output: %q", bad)
		}
	}
}

func TestMigrate_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(testLegacyConfig), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("perm: want %o, got %o", 0600, info.Mode().Perm())
	}
}

func TestMigrate_MissingSource(t *testing.T) {
	dir := t.TempDir()
	err := Migrate("/nonexistent.yaml", filepath.Join(dir, "config.yaml"))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestMigrate_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	err := Migrate(src, dst)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestShowDefaults(t *testing.T) {
	out := ShowDefaults()
	if out == "" {
		t.Fatal("ShowDefaults returned empty string")
	}
	if !strings.Contains(out, "provider: ollama") {
		t.Error("ShowDefaults missing provider")
	}
}

func TestShowDefaults_OmitsZeroValues(t *testing.T) {
	out := ShowDefaults()
	if out == "" {
		t.Fatal("ShowDefaults returned empty string")
	}
	// Zero-value fields should not appear
	for _, bad := range []string{
		`api_key: ""`,
		`model: ""`,
		`models: {}`,
		`presets: []`,
		`rules: []`,
		`instructions: []`,
	} {
		if strings.Contains(out, bad) {
			t.Errorf("zero-value field leaked into ShowDefaults: %q", bad)
		}
	}
}

func TestValidateFile_Good(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
agent:
  default: operate
tools:
  timeout: 120
permission:
  shell: allow
context:
  mode: auto
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err != nil {
		t.Fatalf("ValidateFile should pass: %v", err)
	}
}

func TestValidateFile_BadProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: azure
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for bad provider")
	}
}

func TestValidateFile_BadAgent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
agent:
  default: production
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for bad agent")
	}
}

func TestValidateFile_BadPermission(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
permission:
  shell: superuser
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for bad permission level")
	}
}

func TestValidateFile_BadTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
tools:
  timeout: -1
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestValidateFile_BadContextMode(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
context:
  mode: turbo
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for bad context mode")
	}
}

func TestValidateFile_MissingOpenRouterKey(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: openrouter
openrouter:
  api_key: ""
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for missing openrouter api_key")
	}
}

func TestValidateFile_MissingOpenAIKey(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: openai
openai:
  api_key: ""
  api_key_env: ""
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for missing openai api_key")
	}
}

func TestValidateFile_MissingMemoryDBPath(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
memory:
  enabled: true
  db_path: ""
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for missing memory db_path")
	}
}

func TestValidateFile_MissingSource(t *testing.T) {
	err := ValidateFile("/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidateFile_UnknownKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
typo_key: 123
wrang_key: hello
`
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(cfg)
	if err == nil {
		t.Fatal("expected error for unknown keys")
	}
	if !strings.Contains(err.Error(), "unknown key:") {
		t.Errorf("error should mention unknown keys, got: %v", err)
	}
}

func TestMigrate_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(testLegacyConfig), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	// Verify no .tmp file left behind
	tmp := dst + ".tmp"
	if _, err := os.Stat(tmp); err == nil {
		t.Error("temp file left behind after successful write")
	}
}

func TestMigrate_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "subdir", "config.yaml")

	if err := os.WriteFile(src, []byte(testLegacyConfig), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("destination not created: %v", err)
	}
}

// Verifies that nested fields from defaults are added when the parent
// key exists but the child is missing.
func TestMigrate_NestedMerge(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	// agent exists but without max_tool_iterations
	content := `provider: ollama
agent:
  default: chat
`
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(dst)
	out := string(data)

	if !strings.Contains(out, "max_tool_iterations: 20") {
		t.Error("nested default max_tool_iterations was not added")
	}
	if !strings.Contains(out, "default: chat") {
		t.Error("existing agent.default was overwritten")
	}
}

// Verifies migrate works with arbitrary --file paths.
func TestMigrate_ArbitraryPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "my.yaml")
	dst := filepath.Join(dir, "sub", "deep", "out.yaml")

	minimal := `provider: ollama
`
	if err := os.WriteFile(src, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	// Must not panic on short relative paths either
	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("destination not created: %v", err)
	}

	data, _ := os.ReadFile(dst)
	if !strings.Contains(string(data), "ollama:") {
		t.Error("migrated output missing defaults")
	}
}

// Verifies an empty file gets filled with all defaults rather than
// writing literal "null".
func TestMigrate_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(dst)
	out := string(data)

	// Must not be "null"
	if strings.TrimSpace(out) == "null" {
		t.Error("empty file migrated to literal 'null'")
	}
	// Must contain defaults
	if !strings.Contains(out, "provider: ollama") {
		t.Error("empty file migration missing defaults")
	}
}

// Verifies unknown user keys are preserved.
func TestMigrate_UnrelatedKeys(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.yaml")
	dst := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
my_custom_key: keepme
`
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(dst)
	out := string(data)

	if !strings.Contains(out, "my_custom_key: keepme") {
		t.Error("migrate dropped unknown user key")
	}
}

// Verifies exhaustive backfill: enabled=false booleans
// are injected when absent, while empty api_key / models / presets
// remain omitted. Starts from an empty file so every non-empty
// default should appear.
func TestMigrate_ExhaustiveDefaults(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.yaml")
	dst := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(src, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}

	out := string(data)

	// Non-zero defaults must appear
	for _, want := range []string{
		"provider: ollama",
		"base_url: http://127.0.0.1:11434",
		"max_tool_iterations: 20",
		"timeout: 120",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing expected default %q", want)
		}
	}

	// Exhaustive: enabled=false must be injected
	for _, want := range []string{
		"sudo:",
		"aws:",
		"webfetch:",
		"websearch:",
		"enabled: false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exhaustive missing enabled false %q", want)
		}
	}

	// Explicitly omitted zero-values must NOT appear
	for _, bad := range []string{
		`api_key: ""`,
		`models: {}`,
		`presets: []`,
		`rules: []`,
	} {
		if strings.Contains(out, bad) {
			t.Errorf("zero-value leaked %q", bad)
		}
	}
}

// Verifies that a scalar where a mapping is expected is preserved and
// surfaced via DryMigrate.
func TestMigrate_TypeMismatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.yaml")
	dst := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
tools: "oops"
`
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(src, dst); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	data, _ := os.ReadFile(dst)
	out := string(data)

	// Original scalar must be preserved, not overwritten
	if !strings.Contains(out, `tools: "oops"`) &&
		!strings.Contains(out, "tools: oops") {
		t.Error("migrate overwrote type-mismatched scalar")
	}

	// DryMigrate must surface the mismatch instead of silent no-op
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := DryMigrate(src, dst); err != nil {
		t.Fatalf("DryMigrate failed: %v", err)
	}

	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()

	got := string(buf[:n])

	if !strings.Contains(got, "type mismatch") {
		t.Errorf("DryMigrate should report type mismatch, got %q", got)
	}
}

// Ensures DryMigrate output is stable sorted.
func TestFindMissingKeys_Sorted(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "partial.yaml")
	dst := filepath.Join(dir, "config.yaml")

	content := `provider: ollama
tools:
  timeout: 120
`
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := DryMigrate(src, dst); err != nil {
		t.Fatalf("DryMigrate failed: %v", err)
	}

	w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	r.Close()

	got := string(buf[:n])

	// Extract "+ <path>" lines and verify lexicographic order
	var keys []string

	for _, line := range strings.Split(got, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "+ ") {
			keys = append(keys, strings.TrimPrefix(line, "+ "))
		}
	}

	if len(keys) < 2 {
		t.Fatalf("expected multiple missing keys, got %v", keys)
	}

	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Errorf("DryMigrate output not sorted: want %q <= %q in %v",
				keys[i-1], keys[i], keys)
		}
	}
}
