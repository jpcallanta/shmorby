package health

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestClassify_Missing validates missing classification.
func TestClassify_Missing(t *testing.T) {
	cases := []string{
		`executable file not found in $PATH`,
		`no such file or directory`,
		`fork/exec ./shmorby: no such file or directory`,
		`executable file not found: aws`,
	}
	for _, msg := range cases {
		err := fmt.Errorf("%s", msg)
		if got := Classify(err); got != "missing" {
			t.Errorf("Classify(%q) want missing, got %q",
				msg, got)
		}
		if !IsDegraded(Wrap("exec", 0, err)) {
			t.Errorf("Wrap should be degraded for %q", msg)
		}
	}
}

// TestClassify_Timeout checks deadline and string timeout.
func TestClassify_Timeout(t *testing.T) {
	if got := Classify(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("want timeout for DeadlineExceeded, got %q", got)
	}

	cases := []string{"timeout", "deadline exceeded", "timed out after 5s"}
	for _, msg := range cases {
		if got := Classify(fmt.Errorf("%s", msg)); got != "timeout" {
			t.Errorf("Classify(%q) want timeout, got %q", msg, got)
		}
	}
}

// TestClassify_Auth checks auth mapping.
func TestClassify_Auth(t *testing.T) {
	cases := []string{
		`sudo: a password is required`,
		`no tty present`,
		`unauthorized`,
		`api key invalid 401`,
	}
	for _, msg := range cases {
		if got := Classify(fmt.Errorf("%s", msg)); got != "auth/creds" {
			t.Errorf("Classify(%q) want auth/creds, got %q", msg, got)
		}
	}
}

// TestClassify_Unreachable checks network mapping.
func TestClassify_Unreachable(t *testing.T) {
	msg := "dial tcp 127.0.0.1:11434: connection refused"
	if got := Classify(fmt.Errorf("%s", msg)); got != "unreachable" {
		t.Errorf("want unreachable, got %q", got)
	}
}

// TestWrap_PreservesUnwrap validates errors.Is/As.
func TestWrap_PreservesUnwrap(t *testing.T) {
	base := context.DeadlineExceeded
	w := Wrap("exec", 10*time.Millisecond, base)
	if !errors.Is(w, context.DeadlineExceeded) {
		t.Errorf("Wrap should preserve Is for DeadlineExceeded")
	}
	if d, ok := AsDegraded(w); !ok || d.Reason != "timeout" {
		t.Errorf("AsDegraded want timeout, got %+v ok=%v", d, ok)
	}
	if !IsDegraded(w) {
		t.Error("IsDegraded want true")
	}
}

// TestWrap_Nil validates nil passthrough.
func TestWrap_Nil(t *testing.T) {
	if got := Wrap("exec", 0, nil); got != nil {
		t.Errorf("Wrap nil want nil, got %v", got)
	}
	if got := Classify(nil); got != "" {
		t.Errorf("Classify nil want empty, got %q", got)
	}
}

// TestFormatPrefix_Empty validates empty returns empty.
func TestFormatPrefix_Empty(t *testing.T) {
	if got := FormatPrefix(nil); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// TestFormatPrefix_Multiple checks prefix contains tools.
func TestFormatPrefix_Multiple(t *testing.T) {
	d1 := &Degraded{Tool: "exec", Reason: "timeout", Duration: time.Second}
	d2 := &Degraded{Tool: "sudo", Reason: "auth/creds"}
	pfx := FormatPrefix([]*Degraded{d1, d2})
	if !strings.Contains(pfx, "exec: timeout") {
		t.Errorf("want exec timeout in %q", pfx)
	}
	if !strings.Contains(pfx, "sudo: auth/creds") {
		t.Errorf("want sudo in %q", pfx)
	}
	if !strings.Contains(pfx, "shmorby doctor") {
		t.Errorf("want doctor hint in %q", pfx)
	}
}
