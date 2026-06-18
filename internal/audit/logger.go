package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// AuditEntry records a single permission decision.
type AuditEntry struct {
	Timestamp string `json:"timestamp"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Rule      string `json:"rule"`
	Action    string `json:"action"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	Duration  int64  `json:"duration_ms"`
	ExitCode  int    `json:"exit_code,omitempty"`
}

// AuditLogger writes JSON lines to a file.
type AuditLogger struct {
	file *os.File
	mu   sync.Mutex
}

// NewAuditLogger creates or appends to the audit log at path.
func NewAuditLogger(path string) (*AuditLogger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &AuditLogger{file: f}, nil
}

// Close closes the underlying file.
func (a *AuditLogger) Close() error {
	return a.file.Close()
}

// Log writes one JSON line to the audit file.
func (a *AuditLogger) Log(entry AuditEntry) error {
	entry.Command = redact(entry.Command)

	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	_, err = a.file.Write(data)
	return err
}

var (
	akiaPat      = regexp.MustCompile(`(?i)AKIA\S{16,}`)
	bearerPat    = regexp.MustCompile(`(?i)bearer\s+\S+`)
	githubPat    = regexp.MustCompile(`gh[poausr]_[0-9A-Za-z]{36,}`)
	openaiPat    = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)
	sshKeyPat    = regexp.MustCompile(`-----BEGIN[ A-Z]*(?:RSA|EC|DSA|OPENSSH|PRIVATE)? PRIVATE KEY-----`)
	awsSecretPat = regexp.MustCompile(`(?i)aws_secret_access_key[=:]["']?[A-Za-z0-9/+=]{20,}["']?`)
	googlePat    = regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)
	genericKey   = regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|secret|password|token|credential)\s*[:=]\s*\S{8,}`)
	headerKeyPat = regexp.MustCompile(`(?i)(?:x[_-]?api[_-]?key|x-apikey)[:=]\s*\S{8,}`)
	cliKeyPat    = regexp.MustCompile(`--(?:api[_-]?key|token|secret|password|access-key)\s+\S{8,}`)
	redactPairs  = []struct {
		pat  *regexp.Regexp
		repl string
	}{
		{akiaPat, "[REDACTED]"},
		{awsSecretPat, "aws_secret_access_key=[REDACTED]"},
		{bearerPat, "Bearer [REDACTED]"},
		{githubPat, "[REDACTED]"},
		{openaiPat, "[REDACTED]"},
		{googlePat, "[REDACTED]"},
		{sshKeyPat, "-----BEGIN PRIVATE KEY-----[REDACTED]"},
		{genericKey, "${1}=[REDACTED]"},
		{headerKeyPat, "[REDACTED]"},
		{cliKeyPat, "[REDACTED]"},
	}
)

// redact replaces known secret patterns with placeholders.
func redact(s string) string {
	for _, r := range redactPairs {
		s = r.pat.ReplaceAllString(s, r.repl)
	}
	return s
}
