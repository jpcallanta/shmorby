package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewAuditLogger_CreatesFile checks logger creates the audit file.
func TestNewAuditLogger_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("audit log file was not created")
	}
}

// TestAuditLogger_LogWritesJSONLine checks Log writes a valid JSON line.
func TestAuditLogger_LogWritesJSONLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "echo hello",
		Rule:      "allow",
		Action:    "allow",
		Decision:  "allow",
		Reason:    "test",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !strings.HasSuffix(strings.TrimSpace(string(data)), "}") {
		t.Error("log entry does not end with JSON object")
	}
	if !strings.Contains(string(data), "echo hello") {
		t.Error("log entry should contain the command")
	}
	if !strings.Contains(string(data), `"tool":"shell"`) {
		t.Error("log entry should contain tool field")
	}
}

// TestAuditLogger_LogRedactsAKIA checks AKIA keys are redacted.
func TestAuditLogger_LogRedactsAKIA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "aws configure set aws_access_key_id AKIA1234567890123456",
		Action:    "allow",
		Decision:  "allow",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(data), "AKIA1234567890123456") {
		t.Error("AKIA key should be redacted")
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Error("AKIA key should be replaced with [REDACTED]")
	}
}

// TestAuditLogger_LogRedactsBearer checks bearer tokens are redacted.
func TestAuditLogger_LogRedactsBearer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "curl -H 'Authorization: Bearer sk-abc123def456' https://api.example.com",
		Action:    "allow",
		Decision:  "allow",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(data), "Bearer sk-abc123def456") {
		t.Error("Bearer token should be redacted")
	}
	if !strings.Contains(string(data), "Bearer [REDACTED]") {
		t.Error("Bearer should be replaced with 'Bearer [REDACTED]'")
	}
}

// TestAuditLogger_LogRedactsGitHubToken checks GitHub tokens are redacted.
func TestAuditLogger_LogRedactsGitHubToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "git push https://ghp_123456789012345678901234567890123456@github.com/user/repo.git",
		Action:    "allow",
		Decision:  "allow",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(data), "ghp_123456789012345678901234567890123456") {
		t.Error("GitHub token should be redacted")
	}
}

// TestAuditLogger_LogRedactsOpenAIKey checks OpenAI keys are redacted.
func TestAuditLogger_LogRedactsOpenAIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "export OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz1234567890",
		Action:    "allow",
		Decision:  "allow",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(data), "sk-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Error("OpenAI key should be redacted")
	}
}

// TestAuditLogger_LogExitCode checks exit_code is present when set.
func TestAuditLogger_LogExitCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "false",
		Action:    "allow",
		Decision:  "allow",
		ExitCode:  1,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !strings.Contains(string(data), `"exit_code":1`) {
		t.Error("log entry should contain exit_code")
	}
}

// TestAuditLogger_LogDuration checks duration_ms is present.
func TestAuditLogger_LogDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	entry := AuditEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Tool:      "shell",
		Command:   "echo hello",
		Action:    "allow",
		Decision:  "allow",
		Duration:  1500,
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !strings.Contains(string(data), `"duration_ms":1500`) {
		t.Error("log entry should contain duration_ms")
	}
}

// TestAuditLogger_Close checks close does not error.
func TestAuditLogger_Close(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestRedact_NoSensitiveData_Unchanged checks no-op for clean input.
func TestRedact_NoSensitiveData_Unchanged(t *testing.T) {
	input := "echo hello world"
	got := redact(input)
	if got != input {
		t.Errorf("want unchanged, got %q", got)
	}
}

// TestRedact_AKIA_Redacted checks AKIA pattern.
func TestRedact_AKIA_Redacted(t *testing.T) {
	input := "using key AKIA1234567890123456"
	got := redact(input)
	if strings.Contains(got, "AKIA1234567890123456") {
		t.Error("AKIA not redacted")
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Error("expected [REDACTED]")
	}
}

// TestRedact_Bearer_Redacted checks Bearer token.
func TestRedact_Bearer_Redacted(t *testing.T) {
	input := "Authorization: Bearer some-token-here"
	got := redact(input)
	want := "Authorization: Bearer [REDACTED]"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestRedact_SSHKey_Redacted checks SSH private key header is redacted and
// that the alternate BEGIN OPENSSH PRIVATE KEY form is also caught.
func TestRedact_SSHKey_Redacted(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----base64data-----END RSA PRIVATE KEY-----"
	got := redact(input)
	if strings.Contains(got, "BEGIN RSA PRIVATE KEY") {
		t.Error("SSH key header not redacted")
	}
	if !strings.Contains(got, "BEGIN PRIVATE KEY-----[REDACTED]") {
		t.Error("expected BEGIN header replaced with [REDACTED]")
	}

	input2 := "-----BEGIN OPENSSH PRIVATE KEY-----base64-----END OPENSSH PRIVATE KEY-----"
	got2 := redact(input2)
	if strings.Contains(got2, "BEGIN OPENSSH PRIVATE KEY") {
		t.Error("OPENSSH key header not redacted")
	}
}

// TestRedact_Bearer_Lowercase checks case-insensitive bearer matching.
func TestRedact_Bearer_Lowercase(t *testing.T) {
	input := "Authorization: bearer my-token-value"
	got := redact(input)
	if strings.Contains(got, "bearer my-token-value") {
		t.Error("lowercase bearer token not redacted")
	}
	if !strings.Contains(got, "Bearer [REDACTED]") {
		t.Error("expected 'Bearer [REDACTED]'")
	}
}

// TestRedact_AKIA_ShortKey checks shorter AKIA keys are also caught.
func TestRedact_AKIA_ShortKey(t *testing.T) {
	input := "key=AKIA1234567890123456"
	got := redact(input)
	if strings.Contains(got, "AKIA1234567890123456") {
		t.Error("16-char AKIA not redacted")
	}
}

// TestRedact_AKIA_LongKey checks longer AKIA keys are caught.
func TestRedact_AKIA_LongKey(t *testing.T) {
	input := "key=AKIA12345678901234567890"
	got := redact(input)
	if strings.Contains(got, "AKIA12345678901234567890") {
		t.Error("20-char AKIA not redacted")
	}
}

// TestRedact_CLIKey checks --api-key flag is redacted.
func TestRedact_CLIKey(t *testing.T) {
	input := "cmd --api-key 0123456789abcdef01234567"
	got := redact(input)
	if strings.Contains(got, "0123456789abcdef01234567") {
		t.Error("CLI api-key value not redacted")
	}
}

// TestRedact_CLIKey_Token checks --token flag is redacted.
func TestRedact_CLIKey_Token(t *testing.T) {
	input := "cmd --token ghp_123456789012345678901234567890123456"
	got := redact(input)
	if strings.Contains(got, "ghp_123456789012345678901234567890123456") {
		t.Error("CLI token value not redacted")
	}
}

// TestRedact_HeaderKey checks x-api-key header is redacted.
func TestRedact_HeaderKey(t *testing.T) {
	input := `curl -H "x-api-key: abcdef123456"`
	got := redact(input)
	if strings.Contains(got, "abcdef123456") {
		t.Error("x-api-key value not redacted")
	}
}

// TestRedact_HeaderKey_XApikey checks x-apikey header variant.
func TestRedact_HeaderKey_XApikey(t *testing.T) {
	input := `curl -H "x-apikey: abcdef123456"`
	got := redact(input)
	if strings.Contains(got, "abcdef123456") {
		t.Error("x-apikey value not redacted")
	}
}
