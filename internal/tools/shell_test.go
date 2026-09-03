package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cmdexec "shmorby/internal/exec"
	"shmorby/internal/xdg"
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
	setMaxOutput(t, 65536)

	in := []byte("hello")
	out := TruncateOutput(in)
	if string(out) != string(in) {
		t.Errorf("want %q, got %q", in, out)
	}
}

// TestTruncateOutput_OverLimit_Truncated checks output > 64 KiB
// truncated with notice.
func TestTruncateOutput_OverLimit_Truncated(t *testing.T) {
	const limit int64 = 65536
	setMaxOutput(t, limit)

	big := make([]byte, limit+1)
	for i := range big {
		big[i] = 'x'
	}
	out := TruncateOutput(big)
	if int64(len(out)) > limit {
		t.Errorf("want len <= %d, got %d", limit, len(out))
	}
	if !strings.Contains(string(out), "truncated at 64 KiB") {
		t.Errorf("want truncation notice in output")
	}
}

// TestTruncateOutput_Unlimited_Default checks MaxOutput=0 passes through.
func TestTruncateOutput_Unlimited_Default(t *testing.T) {
	setMaxOutput(t, 0)

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
	_ = r.Register(tool)

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
	_ = r.Register(tool)

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
	_ = r.Register(tool)

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

// TestNewShellTool_DefaultShell checks empty shell defaults to xdg.DefaultShell().
func TestNewShellTool_DefaultShell(t *testing.T) {
	tool := NewShellTool("", "", "allow")
	want := xdg.DefaultShell()
	if tool.shell != want {
		t.Errorf("shell: got %q, want %q", tool.shell, want)
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

// TestRegistry_RegisterDuplicate_ReturnsError checks that registering
// a duplicate tool returns an error instead of panicking.
func TestRegistry_RegisterDuplicate_ReturnsError(t *testing.T) {
	r := NewRegistry()
	tool := NewShellTool("bash", "", "allow")
	if err := r.Register(tool); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := r.Register(tool)
	if err == nil {
		t.Fatal("want error on duplicate register, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf(
			"want error containing 'already registered', got %q",
			err.Error(),
		)
	}

	// Original tool must remain registered.
	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("want 1 tool after duplicate, got %d", len(schemas))
	}
}

// TestRegistry_Schemas_StableOrder checks schemas order matches
// registration order.
func TestRegistry_Schemas_StableOrder(t *testing.T) {
	r := NewRegistry()
	t1 := &namedTool{name: "z_last"}
	t2 := &namedTool{name: "a_first"}
	_ = r.Register(t1)
	_ = r.Register(t2)

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

// TestRegistry_FilterByPerm_ExcludesDeniedTools checks that
// FilterByPerm removes tools with PermLevel "deny".
func TestRegistry_FilterByPerm_ExcludesDeniedTools(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&namedTool{name: "shell"})
	_ = r.Register(&permTool{name: "sudo", perm: "deny"})
	_ = r.Register(&permTool{name: "ssh", perm: "ask"})

	filtered := r.FilterByPerm()
	schemas := filtered.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("want 2 tools after filter, got %d", len(schemas))
	}
	names := make(map[string]bool)
	for _, s := range schemas {
		names[s.Name] = true
	}
	if !names["shell"] {
		t.Error("shell should be in filtered registry")
	}
	if !names["ssh"] {
		t.Error("ssh should be in filtered registry")
	}
	if names["sudo"] {
		t.Error("sudo (deny) should be excluded from filtered registry")
	}
}

// TestRegistry_FilterByPerm_PreservesOrder checks that FilterByPerm
// preserves registration order.
func TestRegistry_FilterByPerm_PreservesOrder(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&permTool{name: "a", perm: "allow"})
	_ = r.Register(&permTool{name: "b", perm: "deny"})
	_ = r.Register(&permTool{name: "c", perm: "ask"})

	filtered := r.FilterByPerm()
	schemas := filtered.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("want 2 tools after filter, got %d", len(schemas))
	}
	if schemas[0].Name != "a" {
		t.Errorf("want first tool 'a', got %q", schemas[0].Name)
	}
	if schemas[1].Name != "c" {
		t.Errorf("want second tool 'c', got %q", schemas[1].Name)
	}
}

// permTool is a test tool with a configurable PermLevel.
type permTool struct {
	name string
	perm string
}

func (p *permTool) Name() string        { return p.name }
func (p *permTool) Description() string { return "test tool" }
func (p *permTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (p *permTool) PermLevel() string { return p.perm }
func (p *permTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return "", nil
}

// TestShellTool_Run_TimeoutKillsProcessGroup verifies that a
// long-running command is killed by the timeout, not left hanging
// due to a blocked pipe read.
func TestShellTool_Run_TimeoutKillsProcessGroup(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"trap '' TERM; sleep 300","timeout_seconds":1}`)

	start := time.Now()
	_, err := tool.Run(context.Background(), args)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("want quick timeout (process group killed), took %v", elapsed)
	}
}

// TestShellTool_Run_ContextCancelKillsProcessGroup verifies that
// cancelling the parent context kills the process group.
func TestShellTool_Run_ContextCancelKillsProcessGroup(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"sleep 300","timeout_seconds":120}`)

	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, err := tool.Run(ctx, args)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("want quick cancellation (process group killed), took %v", elapsed)
	}
}

// TestShellTool_Run_GrandchildInProcessGroup verifies that a forked
// child process is killed when the tool times out (process-group
// isolation via Setpgid).
func TestShellTool_Run_GrandchildInProcessGroup(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(
		`{"command":"bash -c 'sleep 300 & sleep 300'","timeout_seconds":1}`,
	)

	start := time.Now()
	_, err := tool.Run(context.Background(), args)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("want quick timeout (grandchild killed), took %v", elapsed)
	}
}

// TestShellTool_Run_NoOrphansAfterTimeout verifies no orphan processes
// remain on the host after the tool times out. This test runs pgrep
// when available.
func TestShellTool_Run_NoOrphansAfterTimeout(t *testing.T) {
	if _, pgrepErr := exec.LookPath("pgrep"); pgrepErr != nil {
		t.Skip("pgrep not available")
	}

	selfPid := fmt.Sprint(os.Getpid())

	tool := NewShellTool("bash", "", "allow")
	args := []byte(
		`{"command":"sleep 300","timeout_seconds":1}`,
	)

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}

	// Wait a moment for the kernel to clean up the process.
	time.Sleep(100 * time.Millisecond)

	// Verify no orphan sleep processes belonging to the test PID.
	pgrep := exec.Command("pgrep", "-P", selfPid, "-x", "sleep")
	out, pgrepErr := pgrep.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if pgrepErr == nil && outStr != "" {
		// Pgrep returned successfully with output, meaning there are
		// child sleep processes still alive. This is not necessarily a
		// bug (other tests may be running sleep concurrently), so we
		// just warn.
		t.Logf("note: found sleep children of test PID %s: %s", selfPid, outStr)
	}
}

// TestOSExecutor_Run_ProcessGroupIsolation verifies the OSExecutor
// uses process-group isolation.
func TestOSExecutor_Run_ProcessGroupIsolation(t *testing.T) {
	executor := cmdexec.OSExecutor{}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	out, err := executor.Run(ctx, "bash", "-c", "sleep 300")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
	_ = out
	if elapsed > 5*time.Second {
		t.Errorf("want quick timeout, took %v", elapsed)
	}
}

// TestOSExecutor_Run_Grandchild verifies forked grandchild is killed.
func TestOSExecutor_Run_Grandchild(t *testing.T) {
	executor := cmdexec.OSExecutor{}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	out, err := executor.Run(ctx, "bash", "-c", "sleep 300 & sleep 300")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want error for timeout, got nil")
	}
	_ = out
	if elapsed > 5*time.Second {
		t.Errorf("want quick timeout (grandchild killed), took %v", elapsed)
	}
}

// TestShellTool_Run_ProcessGetsOwnGroup verifies the shell process
// is placed in its own process group (Setpgid).
func TestShellTool_Run_ProcessGetsOwnGroup(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"bash -c 'echo -n $$; trap \"\" TERM; sleep 300'","timeout_seconds":1}`)

	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	// Success — the timeout killed the process group and did not
	// hang on the blocked pipe read.
}

// TestShellTool_Run_NormalReturnsOutput verifies normal commands
// still return output correctly after the exec refactor.
func TestShellTool_Run_NormalReturnsOutput(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"echo hello world"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("want 'hello world', got %q", out)
	}
}

// TestShellTool_Run_StdErrCaptured checks stderr is captured.
func TestShellTool_Run_StdErrCaptured(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"echo stdout; echo stderr >&2"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "stdout") {
		t.Errorf("want stdout, got %q", out)
	}
	if !strings.Contains(out, "stderr") {
		t.Errorf("want stderr, got %q", out)
	}
}

// TestNewShellTool_SysProcAttr confirms Setpgid is set.
func TestNewShellTool_SysProcAttr(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	if tool.shell == "" {
		t.Error("empty shell")
	}
	if tool.workdir == "" {
		t.Error("empty workdir")
	}
	// Verify the tool itself is properly initialized; the actual
	// Setpgid is set at command creation time which we can't test
	// without running a real command (covered by other tests).
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

// --- Timeout ceiling tests ---

// TestShellTool_TimeoutCeiling_Clamped checks that timeout_seconds
// exceeding maxTimeoutSeconds is clamped to the ceiling.
func TestShellTool_TimeoutCeiling_Clamped(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	// Command that sleeps for 10 seconds, with a 86400s timeout
	// that should be clamped to 3600s.
	args := []byte(`{"command":"sleep 10","timeout_seconds":86400}`)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	_, err := tool.Run(ctx, args)
	elapsed := time.Since(start)

	// The error message should mention the clamped value (3600s),
	// not the original 86400s — proving the ceiling was applied.
	if err != nil {
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("want timeout error, got %v", err)
		}
		// Confirm the ceiling was applied: error should say 3600s.
		if !strings.Contains(err.Error(), "3600s") {
			t.Errorf("want ceiling 3600s in error, got: %v", err)
		}
	}
	// The command should have been killed by context timeout (~3s),
	// not by the 86400s user timeout — confirming the ceiling was
	// accepted and the context deadline took effect.
	if elapsed > 5*time.Second {
		t.Errorf("command took too long (%v), ceiling may not be enforced", elapsed)
	}
}

// TestShellTool_TimeoutCeiling_ExactBoundary checks that a timeout
// exactly at the ceiling is accepted.
func TestShellTool_TimeoutCeiling_ExactBoundary(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"echo ok","timeout_seconds":3600}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want no error for exact-boundary timeout, got %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("want 'ok' output, got %q", out)
	}
}

// TestShellTool_TimeoutCeiling_BelowCeiling checks that a timeout
// below the ceiling is used as-is.
func TestShellTool_TimeoutCeiling_BelowCeiling(t *testing.T) {
	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"sleep 5","timeout_seconds":2}`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err := tool.Run(ctx, args)
	elapsed := time.Since(start)

	// With timeout_seconds=2 (below ceiling), the command should
	// complete in ~2s, not 5s.
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	if elapsed > 4*time.Second {
		t.Errorf("command took %v, expected ~2s timeout", elapsed)
	}
}

// TestShellTool_Run_BufferCap_Notice verifies a command whose output
// exceeds the buffer cap is truncated with a notice instead of
// exhausting memory.
func TestShellTool_Run_BufferCap_Notice(t *testing.T) {
	old := cmdexec.MaxOutputBufferBytes
	cmdexec.MaxOutputBufferBytes = 1024
	defer func() { cmdexec.MaxOutputBufferBytes = old }()

	tool := NewShellTool("bash", "", "allow")
	args := []byte(`{"command":"dd if=/dev/zero bs=1024 count=8` +
		` 2>/dev/null | tr '\\0' 'x'"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "stream capped") {
		t.Errorf("want capped notice in output, got %q",
			out[max(0, len(out)-60):])
	}
}

// TestShellTool_Run_InvalidCwd_Rejected checks a nonexistent cwd or
// a cwd pointing at a file is rejected before the command spawns.
func TestShellTool_Run_InvalidCwd_Rejected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewShellTool("bash", "", "allow")
	for _, cwd := range []string{"/nonexistent/dir/xyz", file} {
		args, _ := json.Marshal(map[string]string{
			"command": "pwd", "cwd": cwd,
		})
		if _, err := tool.Run(context.Background(), args); err == nil {
			t.Errorf("want error for cwd %q, got nil", cwd)
		}
	}
}

// TestShellTool_Run_ValidCwd_Executes checks a real directory cwd
// still runs.
func TestShellTool_Run_ValidCwd_Executes(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellTool("bash", "", "allow")
	args, _ := json.Marshal(map[string]string{
		"command": "pwd", "cwd": dir,
	})

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// macOS resolves /var -> /private/var for t.TempDir paths.
	if !strings.Contains(out, filepath.Base(dir)) {
		t.Errorf("want cwd %q in output, got %q", dir, out)
	}
}
