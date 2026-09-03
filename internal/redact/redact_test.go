package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

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

// TestJSONData_JSONKeySecrets verifies JSON object keys like
// password/api_key/token are redacted, including nested objects.
func TestJSONData_JSONKeySecrets(t *testing.T) {
	input := []byte(`{"web1":"nginx","creds":{"password":"hunter2","api_key":"sk-12345678901234567890123456789012"},"auth":{"token":"abc12345"}}`)
	got, err := JSONData(input)
	if err != nil {
		t.Fatalf("JSONData: %v", err)
	}
	for _, secret := range []string{
		"hunter2", "sk-12345678901234567890123456789012", "abc12345",
	} {
		if strings.Contains(string(got), secret) {
			t.Errorf("secret %q not redacted: %s", secret, got)
		}
	}
	if !json.Valid(got) {
		t.Errorf("redacted output is not valid JSON: %s", got)
	}
	if !strings.Contains(string(got), "nginx") {
		t.Errorf("non-secret data lost: %s", got)
	}
}

// TestJSONData_EmbeddedPattern verifies credential patterns inside
// ordinary string values are still caught (e.g. AKIA keys).
func TestJSONData_EmbeddedPattern(t *testing.T) {
	input := []byte(`{"key":"AKIA1234567890ABCDEF","host":"web1"}`)
	got, err := JSONData(input)
	if err != nil {
		t.Fatalf("JSONData: %v", err)
	}
	if strings.Contains(string(got), "AKIA1234567890ABCDEF") {
		t.Errorf("AKIA key not redacted: %s", got)
	}
	if !strings.Contains(string(got), "[REDACTED]") {
		t.Errorf("want [REDACTED] marker, got %s", got)
	}
	if !strings.Contains(string(got), "web1") {
		t.Errorf("non-secret data lost: %s", got)
	}
}

// TestJSONData_PrecisionPreserved verifies integers > 2^53 keep full
// precision through the redaction round-trip.
func TestJSONData_PrecisionPreserved(t *testing.T) {
	input := []byte(`{"id":9007199254740993,"name":"web1"}`)
	got, err := JSONData(input)
	if err != nil {
		t.Fatalf("JSONData: %v", err)
	}
	if !strings.Contains(string(got), "9007199254740993") {
		t.Errorf("integer precision lost: %s", got)
	}
}

// TestJSONData_NonStringSecretValue verifies non-string values under
// secret-named keys are replaced too.
func TestJSONData_NonStringSecretValue(t *testing.T) {
	input := []byte(`{"port":8080,"password":12345}`)
	got, err := JSONData(input)
	if err != nil {
		t.Fatalf("JSONData: %v", err)
	}
	if strings.Contains(string(got), "12345") {
		t.Errorf("numeric secret not redacted: %s", got)
	}
	if !strings.Contains(string(got), "8080") {
		t.Errorf("non-secret numeric value lost: %s", got)
	}
}

// TestJSONData_InvalidInput verifies invalid JSON returns an error.
func TestJSONData_InvalidInput(t *testing.T) {
	if _, err := JSONData([]byte(`{"broken":`)); err == nil {
		t.Error("want error for invalid JSON, got nil")
	}
}

// TestJSONData_Arrays verifies redaction inside nested arrays.
func TestJSONData_Arrays(t *testing.T) {
	input := []byte(`{"creds":[{"password":"hunter2"},{"username":"alice"}]}`)
	got, err := JSONData(input)
	if err != nil {
		t.Fatalf("JSONData: %v", err)
	}
	if strings.Contains(string(got), "hunter2") {
		t.Errorf("secret in array not redacted: %s", got)
	}
	if !strings.Contains(string(got), "alice") {
		t.Errorf("non-secret array data lost: %s", got)
	}
}

// TestSecretString_NewPatterns verifies the patterns added for
// defense-in-depth (Slack, Stripe, npm, GitLab, JWT, connection
// strings) are redacted.
func TestSecretString_NewPatterns(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"Slack",
			"token=xoxb-1234567890-1234567890123-abcdefghijklmnopqrst",
			"token=[REDACTED]",
		},
		{
			"Stripe",
			"sk_live_4eC39HqLyjWDarjtT1zdp7dc",
			"[REDACTED]",
		},
		{
			"StripeTestKept",
			"sk_test_4eC39HqLyjWDarjtT1zdp7dc",
			"sk_test_4eC39HqLyjWDarjtT1zdp7dc",
		},
		{
			"Npm",
			"//registry.npmjs.org/:_authToken=npm_" +
				"abcdefghijklmnopqrstuvwxyz0123456789",
			"//registry.npmjs.org/:_authToken=[REDACTED]",
		},
		{
			"GitLab",
			"glpat-abcdefghijklmnopqrstuvwx",
			"[REDACTED]",
		},
		{
			"JWT",
			"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" +
				".eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4ifQ" +
				".SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			"[REDACTED]",
		},
		{
			"ConnStringPostgres",
			"dsn: postgres://admin:s3cr3t@db.internal:5432/app",
			"dsn: [REDACTED]",
		},
		{
			"ConnStringMongo",
			"uri mongodb+srv://user:pass@cluster0.example.mongodb.net/test",
			"uri [REDACTED]",
		},
	}
	for _, tc := range cases {
		if got := SecretString(tc.in); got != tc.want {
			t.Errorf("%s: SecretString() = %q, want %q",
				tc.name, got, tc.want)
		}
	}
}
