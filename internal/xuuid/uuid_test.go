package xuuid

import (
	"regexp"
	"testing"
)

// Matches the standard UUID v4 layout: 8-4-4-4-12 lowercase hex.
var uuidRe = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestNew_Format(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !uuidRe.MatchString(id) {
		t.Errorf("uuid %q does not match UUID v4 format", id)
	}
}

func TestNew_VersionNibble(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The 14th character (index 14 in "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx")
	// is the version nibble and must be '4'.
	if id[14] != '4' {
		t.Errorf("version nibble = %c, want '4'", id[14])
	}
}

func TestNew_VariantBits(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The 19th character (index 19) carries variant bits and must be
	// one of 8, 9, a, b (binary 10xx).
	c := id[19]
	if c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Errorf("variant character = %c, want one of 8/9/a/b", c)
	}
}

func TestNew_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)

	for i := 0; i < 1000; i++ {
		id, err := New()
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}

		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate UUID generated: %s", id)
		}

		seen[id] = struct{}{}
	}
}
