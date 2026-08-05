// Package redact provides secret-pattern redaction for tool output and args.
// Used by both the audit logger and memory store to prevent credential
// leakage via persisted data.
package redact

import "regexp"

var (
	akiaPat    = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	bearerPat  = regexp.MustCompile(`Bearer\s+\S+`)
	githubPat  = regexp.MustCompile(`gh[poausr]_[0-9A-Za-z]{36,}`)
	openaiPat  = regexp.MustCompile(`sk-[A-Za-z0-9]{32,}`)
	sshKeyPat  = regexp.MustCompile(`-----BEGIN[ A-Z]*(?:RSA|EC|DSA|OPENSSH)? PRIVATE KEY-----`)
	awsSecret  = regexp.MustCompile(`(?i)aws_secret_access_key[=:]["']?[A-Za-z0-9/+=]{40}["']?`)
	googlePat  = regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)
	genericKey = regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|secret|password)\s*[:=]\s*\S{8,}`)
)

var patterns = []struct {
	pat  *regexp.Regexp
	repl string
}{
	{akiaPat, "[REDACTED]"},
	{awsSecret, "aws_secret_access_key=[REDACTED]"},
	{bearerPat, "Bearer [REDACTED]"},
	{githubPat, "[REDACTED]"},
	{openaiPat, "[REDACTED]"},
	{googlePat, "[REDACTED]"},
	{sshKeyPat, "-----BEGIN PRIVATE KEY-----[REDACTED]"},
	{genericKey, "${1}=[REDACTED]"},
}

// SecretBytes applies secret-pattern redaction to a byte slice.
// Replaces known patterns (AKIA keys, Bearer tokens, OpenAI keys, etc.)
// with "[REDACTED]".
func SecretBytes(raw []byte) []byte {
	s := string(raw)
	for _, r := range patterns {
		s = r.pat.ReplaceAllString(s, r.repl)
	}
	return []byte(s)
}

// SecretString applies secret-pattern redaction to a string.
// Replaces known patterns (AKIA keys, Bearer tokens, OpenAI keys, etc.)
// with "[REDACTED]".
func SecretString(output string) string {
	for _, r := range patterns {
		output = r.pat.ReplaceAllString(output, r.repl)
	}
	return output
}
