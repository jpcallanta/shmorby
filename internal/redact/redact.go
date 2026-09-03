// Package redact provides secret-pattern redaction for tool output and args.
// Used by both the audit logger and memory store to prevent credential
// leakage via persisted data.
package redact

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var (
	akiaPat    = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	bearerPat  = regexp.MustCompile(`Bearer\s+\S+`)
	githubPat  = regexp.MustCompile(`gh[poausr]_[0-9A-Za-z]{36,}`)
	openaiPat  = regexp.MustCompile(`sk-[A-Za-z0-9]{32,}`)
	sshKeyPat  = regexp.MustCompile(`-----BEGIN[ A-Z]*(?:RSA|EC|DSA|OPENSSH)? PRIVATE KEY-----`)
	awsSecret  = regexp.MustCompile(`(?i)aws_secret_access_key[=:]["']?[A-Za-z0-9/+=]{40}["']?`)
	googlePat  = regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)
	genericKey = regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|secret|password)\s*[:=]\s*\S{8,}`)
	// Slack bot/user/app tokens (xoxb-, xoxp-, xoxa-, xoxr-, xoxs-).
	slackPat = regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`)
	// Stripe live keys. sk_test_ keys are public placeholders, so
	// only live ones are redacted.
	stripePat = regexp.MustCompile(`sk_live_[0-9A-Za-z]{24,}`)
	npmPat    = regexp.MustCompile(`npm_[A-Za-z0-9]{36,}`)
	gitlabPat = regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`)
	// JWT (header.payload.signature); the fixed "eyJ" prefix is
	// base64('{"') for both header and payload. Segment minimums
	// guard against false positives on short base64 blobs.
	jwtPat = regexp.MustCompile(
		`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)
	// Credential-bearing connection strings. The whole URI (host
	// included) is replaced because even the host is sensitive in
	// internal deployments.
	connStrPat = regexp.MustCompile(
		`(?i)(?:mysql|postgres(?:ql)?|mongodb(?:\+srv)?|redis|amqp)` +
			`://[^\s"']+`)
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
	{slackPat, "[REDACTED]"},
	{stripePat, "[REDACTED]"},
	{npmPat, "[REDACTED]"},
	{gitlabPat, "[REDACTED]"},
	{jwtPat, "[REDACTED]"},
	{connStrPat, "[REDACTED]"},
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

// secretKeyRoots are normalized key fragments that mark an object
// value as secret. Matching is case-insensitive and punctuation-free,
// so password, passwd, api_key, api-key, auth token, client_secret,
// private key etc. all match.
var secretKeyRoots = []string{
	"password", "passwd", "apikey", "secret", "token",
	"credential", "privatekey", "authorization",
}

// secretKey reports whether an object key names a secret-bearing
// value. Substring match on the normalized key (lowercase, non
// alphanumeric stripped), e.g. "API_KEY" -> "apikey".
func secretKey(k string) bool {
	norm := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return -1
	}, k)
	for _, root := range secretKeyRoots {
		if strings.Contains(norm, root) {
			return true
		}
	}
	return false
}

// redactTree redacts a decoded JSON tree in place. Values under
// secret-named keys are fully replaced with "[REDACTED]"; remaining
// string values are passed through SecretString so embedded credential
// patterns (AKIA keys, tokens) are still caught.
func redactTree(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if secretKey(k) {
				t[k] = "[REDACTED]"
			} else {
				t[k] = redactTree(val)
			}
		}
	case []interface{}:
		for i, val := range t {
			t[i] = redactTree(val)
		}
	case string:
		return SecretString(t)
	}
	return v
}

// JSONData redacts secrets inside a JSON document by walking the
// decoded tree. Object values under secret-named keys (password,
// api_key, secret, token, ...) are replaced with "[REDACTED]" and
// remaining string values are pattern-redacted. Unlike regex-on-text
// redaction, the output is always valid JSON, so it is safe to store.
// Numbers are decoded as json.Number, preserving integer precision.
func JSONData(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	redactTree(v)
	return json.Marshal(v)
}
