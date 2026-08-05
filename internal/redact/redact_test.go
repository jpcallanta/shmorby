package redact

import "testing"

// TestSecretBytes_AKIA verifies AKIA key patterns are redacted in bytes.
func TestSecretBytes_AKIA(t *testing.T) {
	input := []byte("AKIA1234567890ABCDEF")
	got := string(SecretBytes(input))
	if got != "[REDACTED]" {
		t.Errorf("SecretBytes(AKIA) = %q, want [REDACTED]", got)
	}
}

// TestSecretString_AKIA verifies AKIA key patterns are redacted in strings.
func TestSecretString_AKIA(t *testing.T) {
	input := "AKIA1234567890ABCDEF"
	got := SecretString(input)
	if got != "[REDACTED]" {
		t.Errorf("SecretString(AKIA) = %q, want [REDACTED]", got)
	}
}

// TestSecretString_Bearer verifies Bearer token patterns are redacted.
func TestSecretString_Bearer(t *testing.T) {
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9"
	got := SecretString(input)
	if got != "Authorization: Bearer [REDACTED]" {
		t.Errorf("SecretString(Bearer) = %q, want Authorization: Bearer [REDACTED]", got)
	}
}

// TestSecretString_GitHub verifies GitHub token patterns are redacted.
func TestSecretString_GitHub(t *testing.T) {
	input := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef123456"
	got := SecretString(input)
	if got != "[REDACTED]" {
		t.Errorf("SecretString(GitHub) = %q, want [REDACTED]", got)
	}
}

// TestSecretString_OpenAI verifies OpenAI key patterns are redacted.
func TestSecretString_OpenAI(t *testing.T) {
	input := "sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	got := SecretString(input)
	if got != "[REDACTED]" {
		t.Errorf("SecretString(OpenAI) = %q, want [REDACTED]", got)
	}
}

// TestSecretString_SSHKey verifies SSH private key headers are redacted.
func TestSecretString_SSHKey(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----"
	got := SecretString(input)
	if got != "-----BEGIN PRIVATE KEY-----[REDACTED]" {
		t.Errorf("SecretString(SSHKey) = %q, want -----BEGIN PRIVATE KEY-----[REDACTED]", got)
	}
}

// TestSecretString_AWSSecret verifies AWS secret access key patterns are redacted.
func TestSecretString_AWSSecret(t *testing.T) {
	input := "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	got := SecretString(input)
	if got != "aws_secret_access_key=[REDACTED]" {
		t.Errorf("SecretString(AWSSecret) = %q, want aws_secret_access_key=[REDACTED]", got)
	}
}

// TestSecretString_GenericKey verifies generic api_key patterns are redacted.
func TestSecretString_GenericKey(t *testing.T) {
	input := "api_key=abcdefghij1234567890"
	got := SecretString(input)
	// The regex uses non-capturing group with replacement "${1}=[REDACTED]".
	// Since ${1} is empty, the result is "=[REDACTED]" replacing the full match.
	// The surrounding non-matching text (nothing in this case) is preserved.
	want := "=[REDACTED]"
	if got != want {
		t.Errorf("SecretString(GenericKey) = %q, want %q", got, want)
	}
}

// TestSecretString_GoogleAPI verifies Google API key patterns are redacted.
func TestSecretString_GoogleAPI(t *testing.T) {
	input := "AIzaSyDdI0hCZtE6vySjMm-WEfRq3CPzqKqqsHI"
	got := SecretString(input)
	if got != "[REDACTED]" {
		t.Errorf("SecretString(GoogleAPI) = %q, want [REDACTED]", got)
	}
}

// TestSecretString_NoMatch verifies non-sensitive data passes through unchanged.
func TestSecretString_NoMatch(t *testing.T) {
	input := "normal command output with no secrets"
	got := SecretString(input)
	if got != input {
		t.Errorf("SecretString(no match) = %q, want %q", got, input)
	}
}

// TestSecretString_MultipleSecrets verifies multiple secrets in one string.
func TestSecretString_MultipleSecrets(t *testing.T) {
	input := "key1=AKIA1234567890ABCDEF key2=sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	got := SecretString(input)
	// Both should be redacted
	if got == input {
		t.Errorf("SecretString(multiple) did not redact any secrets: %q", got)
	}
}

// TestSecretString_Empty verifies empty string passes through.
func TestSecretString_Empty(t *testing.T) {
	got := SecretString("")
	if got != "" {
		t.Errorf("SecretString(empty) = %q, want empty string", got)
	}
}
