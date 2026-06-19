package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewShellTool_EchoHello checks echo hello via shell tool.
func TestNewShellTool_EchoHello(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"echo hello"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "hello"
	if !strings.Contains(out, want) {
		t.Errorf("want output containing %q, got %q", want, out)
	}
}

// TestShellTool_Deny_NowExecutes checks that Run() no longer checks
// permission (enforced at agent loop level).
func TestShellTool_Deny_NowExecutes(t *testing.T) {
	tool := NewShellTool("bash", "", "deny")
	args := []byte(`{"command":"echo should-run"}`)

	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want no error (permission checked by agent loop), got %v", err)
	}
	if !strings.Contains(result, "should-run") {
		t.Errorf("want command output, got %q", result)
	}
}

// TestTruncateOutput_UnderLimit_Unchanged checks small output passes.
func TestTruncateOutput_UnderLimit_Unchanged(t *testing.T) {
	old := MaxOutput
	MaxOutput = 65536
	defer func() { MaxOutput = old }()

	in := []byte("hello")
	out := TruncateOutput(in)
	if string(out) != string(in) {
		t.Errorf("want %q, got %q", in, out)
	}
}

// TestTruncateOutput_OverLimit_Truncated checks output > 64 KiB
// truncated with notice.
func TestTruncateOutput_OverLimit_Truncated(t *testing.T) {
	old := MaxOutput
	MaxOutput = 65536
	defer func() { MaxOutput = old }()

	big := make([]byte, MaxOutput+1)
	for i := range big {
		big[i] = 'x'
	}
	out := TruncateOutput(big)
	if len(out) > MaxOutput {
		t.Errorf("want len <= %d, got %d", MaxOutput, len(out))
	}
	if !strings.Contains(string(out), "truncated at 64 KiB") {
		t.Errorf("want truncation notice in output")
	}
}

// TestTruncateOutput_Unlimited_Default checks MaxOutput=0 passes through.
func TestTruncateOutput_Unlimited_Default(t *testing.T) {
	old := MaxOutput
	MaxOutput = 0
	defer func() { MaxOutput = old }()

	big := make([]byte, 100000)
	for i := range big {
		big[i] = 'z'
	}
	out := TruncateOutput(big)
	if len(out) != len(big) {
		t.Errorf("want len %d, got %d", len(big), len(out))
	}
}

// TestRedactArgs_AKIA_Redacted checks AKIA key patterns redacted.
func TestRedactArgs_AKIA_Redacted(t *testing.T) {
	in := []byte(`{"command":"aws","key":"AKIAIOSFODNN7EXAMPLE"}`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("want AKIA key redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_Bearer_Redacted checks Bearer token patterns redacted.
func TestRedactArgs_Bearer_Redacted(t *testing.T) {
	in := []byte(`{"command":"curl","header":"Authorization: Bearer my-secret-token"}`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "my-secret-token") {
		t.Errorf("want Bearer token redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_GitHubToken_Redacted checks GitHub token patterns.
func TestRedactArgs_GitHubToken_Redacted(t *testing.T) {
	in := []byte(`{"command":"git","token":"ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "ghp_") {
		t.Errorf("want GitHub PAT redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_OpenAIKey_Redacted checks OpenAI-style keys.
func TestRedactArgs_OpenAIKey_Redacted(t *testing.T) {
	in := []byte(`{"command":"curl","key":"sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "sk-xxxxxxxx") {
		t.Errorf("want OpenAI key redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_SSHKey_Redacted checks SSH private key headers.
func TestRedactArgs_SSHKey_Redacted(t *testing.T) {
	in := []byte(`data: "-----BEGIN OPENSSH PRIVATE KEY-----\n..."`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "BEGIN OPENSSH PRIVATE KEY") {
		t.Errorf("want SSH key header redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_AWSSecret_Redacted checks AWS secret access key.
func TestRedactArgs_AWSSecret_Redacted(t *testing.T) {
	in := []byte(`aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "wJalrXUtnFEMI") {
		t.Errorf("want AWS secret redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_GenericKey_Redacted checks generic api_key fields.
func TestRedactArgs_GenericKey_Redacted(t *testing.T) {
	in := []byte(`api_key=my-super-secret-value-here`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "my-super-secret") {
		t.Errorf("want api_key redacted, got %s", out)
	}
}

// TestRedactArgs_GoogleAPIKey_Redacted checks Google API key patterns.
func TestRedactArgs_GoogleAPIKey_Redacted(t *testing.T) {
	in := []byte(`key=AIzaSyDf09s9f8sdf09s8df09s8df09s8df09s8df0`)
	out := string(RedactArgs(in))
	if strings.Contains(out, "AIzaSyD") {
		t.Errorf("want Google API key redacted, got %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("want [REDACTED] in output, got %s", out)
	}
}

// TestRedactArgs_NoMatch_Unchanged checks non-sensitive data passes through.
func TestRedactArgs_NoMatch_Unchanged(t *testing.T) {
	in := []byte(`{"command":"echo hello","args":["world"]}`)
	out := string(RedactArgs(in))
	if out != string(in) {
		t.Errorf("want unchanged output, got %s", out)
	}
}

// TestRegistry_Schemas_NonEmpty checks schemas returns registered tools.
func TestRegistry_Schemas_NonEmpty(t *testing.T) {
	r := NewRegistry()
	tool := NewShellTool("bash", "", "allow")
	r.Register(tool)

	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("want 1 schema, got %d", len(schemas))
	}
	if schemas[0].Name != "shell" {
		t.Errorf("want name 'shell', got %q", schemas[0].Name)
	}
}

// TestRegistry_Run_UnknownTool_Error checks unknown tool returns error.
func TestRegistry_Run_UnknownTool_Error(t *testing.T) {
	r := NewRegistry()
	_, err := r.Run(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("want error for unknown tool, got nil")
	}
}

// TestRegistry_Run_EchoHello checks echo hello via registry returns
// output.
func TestRegistry_Run_EchoHello(t *testing.T) {
	r := NewRegistry()
	tool := NewShellTool("bash", "", "allow")
	r.Register(tool)

	out, err := r.Run(
		context.Background(),
		"shell",
		[]byte(`{"command":"echo hello"}`),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("want output containing 'hello', got %q", out)
	}
}

// TestRegistry_Run_Deny_NowExecutes checks Registry.Run() no longer
// checks permission (enforced at agent loop level).
func TestRegistry_Run_Deny_NowExecutes(t *testing.T) {
	r := NewRegistry()
	tool := NewShellTool("bash", "", "deny")
	r.Register(tool)

	result, err := r.Run(
		context.Background(),
		"shell",
		[]byte(`{"command":"echo should-run"}`),
	)
	if err != nil {
		t.Fatalf("want no error (permission checked by agent loop), got %v", err)
	}
	if !strings.Contains(result, "should-run") {
		t.Errorf("want command output, got %q", result)
	}
}

// TestShellTool_InvalidJSON_ReturnsError checks invalid args error.
func TestShellTool_InvalidJSON_ReturnsError(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid shell args") {
		t.Errorf("want error containing 'invalid shell args', got %v", err)
	}
}

// TestShellTool_MissingCommand_ReturnsError checks missing command
// field.
func TestShellTool_MissingCommand_ReturnsError(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	_, err := tool.Run(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("want error for missing command, got nil")
	}
	if !strings.Contains(err.Error(), "missing required") {
		t.Errorf("want error about missing command, got %v", err)
	}
}

// TestNewShellTool_DefaultShell checks empty shell defaults to bash.
func TestNewShellTool_DefaultShell(t *testing.T) {
	tool := NewShellTool("", "", "allow")
	if tool.shell == "" {
		t.Error("want non-empty shell, got empty")
	}
}

// TestNewShellTool_DefaultWorkdir checks empty workdir defaults to
// non-empty.
func TestNewShellTool_DefaultWorkdir(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	if tool.workdir == "" {
		t.Error("want non-empty workdir, got empty")
	}
}

// TestShellTool_CustomCwd checks cwd override changes command directory.
func TestShellTool_CustomCwd(t *testing.T) {
	tmp := t.TempDir()
	tool := NewShellTool("bash", "", "allow")
	args := []byte(
		fmt.Sprintf(`{"command":"pwd","cwd":"%s"}`, tmp),
	)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, filepath.Base(tmp)) {
		t.Errorf("want output containing %q, got %q", tmp, out)
	}
}

// TestShellTool_Timeout_ReturnsError checks timeout kills the command.
func TestShellTool_Timeout_ReturnsError(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	// sleep longer than the 1-second timeout.
	args := []byte(`{"command":"sleep 10","timeout_seconds":1}`)

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
}

// TestShellTool_DefaultTimeout_Configurable checks the configured default
// timeout is used when timeout_seconds is not provided.
func TestShellTool_DefaultTimeout_Configurable(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	tool.SetDefaultTimeout(1)

	args := []byte(`{"command":"sleep 10"}`)

	start := time.Now()
	_, err := tool.Run(context.Background(), args)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
	if elapsed > 3*time.Second {
		t.Errorf("expected quick timeout from default, took %v", elapsed)
	}
}

// TestShellTool_Timeout_ShortDuration checks tight timeout triggers
// cancellation.
func TestShellTool_Timeout_ShortDuration(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"sleep 5","timeout_seconds":1}`)

	start := time.Now()
	_, err := tool.Run(context.Background(), args)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
	if elapsed > 3*time.Second {
		t.Errorf("expected quick timeout, took %v", elapsed)
	}
}

// TestShellTool_NonZeroExit_ReturnsOutputAndNoError checks that
// non-zero exit returns the output with exit code in text, not a Go
// error.
func TestShellTool_NonZeroExit_ReturnsOutputAndNoError(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"echo fail && false"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want nil error for non-zero exit, got %v", err)
	}
	if !strings.Contains(out, "fail") {
		t.Errorf("want output containing 'fail', got %q", out)
	}
	if !strings.Contains(out, "exit code: 1") {
		t.Errorf("want output containing 'exit code: 1', got %q", out)
	}
}

// TestShellTool_NonZeroExit_Exit2 checks exit code appears for code 2.
func TestShellTool_NonZeroExit_Exit2(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"exit 2"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want nil error for exit 2, got %v", err)
	}
	if !strings.Contains(out, "exit code: 2") {
		t.Errorf("want output containing 'exit code: 2', got %q", out)
	}
}

// TestRegistry_RegisterDuplicate_Panics checks duplicate registration.
func TestRegistry_RegisterDuplicate_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on duplicate register, got nil")
		}
	}()
	r := NewRegistry()
	tool := NewShellTool("bash", "", "allow")
	r.Register(tool)
	r.Register(tool)
}

// TestRegistry_Schemas_StableOrder checks schemas order matches
// registration order.
func TestRegistry_Schemas_StableOrder(t *testing.T) {
	r := NewRegistry()
	t1 := &namedTool{name: "z_last"}
	t2 := &namedTool{name: "a_first"}
	r.Register(t1)
	r.Register(t2)

	schemas := r.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("want 2 schemas, got %d", len(schemas))
	}
	if schemas[0].Name != "z_last" {
		t.Errorf("want schemas[0]='z_last', got %q", schemas[0].Name)
	}
	if schemas[1].Name != "a_first" {
		t.Errorf("want schemas[1]='a_first', got %q", schemas[1].Name)
	}
}

// namedTool is a test double implementing Tool with a configurable
// name.
type namedTool struct {
	name string
}

func (n *namedTool) Name() string        { return n.name }
func (n *namedTool) Description() string { return "test tool" }
func (n *namedTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (n *namedTool) PermLevel() string { return "allow" }
func (n *namedTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return "", nil
}
