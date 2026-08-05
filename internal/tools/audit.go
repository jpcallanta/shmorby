package tools

import "shmorby/internal/redact"

// RedactArgs applies secret-pattern redaction to tool arguments.
// Delegates to the shared redact package to avoid import cycles.
func RedactArgs(raw []byte) []byte {
	return redact.SecretBytes(raw)
}
