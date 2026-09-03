package health

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Degraded wraps an external-tool failure with structured metadata
// for surfacing to the operator.
type Degraded struct {
	Tool     string
	Reason   string
	Duration time.Duration
	Err      error
}

// Returns a human-readable degraded message.
func (d *Degraded) Error() string {
	if d.Err == nil {
		return fmt.Sprintf(
			"%s degraded (%s)", d.Tool, d.Reason,
		)
	}

	return fmt.Sprintf(
		"%s degraded (%s): %v", d.Tool, d.Reason, d.Err,
	)
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (d *Degraded) Unwrap() error { return d.Err }

// Wrap classifies err and returns a Degraded error.
// Returns nil when err is nil.
func Wrap(
	tool string, dur time.Duration, err error,
) error {
	if err == nil {
		return nil
	}

	return &Degraded{
		Tool:     tool,
		Reason:   Classify(err),
		Duration: dur,
		Err:      err,
	}
}

// Classify maps an error to a short reason string.
func Classify(err error) string {
	if err == nil {
		return ""
	}

	// Context errors take precedence.
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}

	msg := strings.ToLower(err.Error())

	// Missing binary / command.
	if strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file or directory") {
		return "missing"
	}
	if strings.Contains(msg, "not found") &&
		strings.Contains(msg, "executable") {
		return "missing"
	}

	// Sudo auth.
	if strings.Contains(msg, "a password is required") ||
		strings.Contains(msg, "no tty present") ||
		strings.Contains(msg, "sorry, user") {
		return "auth/creds"
	}

	// Generic auth / creds.
	if strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "api key") ||
		strings.Contains(msg, "apikey") ||
		strings.Contains(msg, "credentials") ||
		strings.Contains(msg, " 401") ||
		strings.Contains(msg, " 403") {
		return "auth/creds"
	}

	// Network / reachability.
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "name resolution") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "unreachable") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "tls") &&
			strings.Contains(msg, "handshake") {
		return "unreachable"
	}

	// Timeout by string (covers wrapped deadline).
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "timed out") {
		return "timeout"
	}

	// Permission.
	if strings.Contains(msg, "permission denied") {
		return "permission"
	}

	return "error"
}

// IsDegraded reports whether err is a Degraded error.
func IsDegraded(err error) bool {
	var d *Degraded
	return errors.As(err, &d)
}

// AsDegraded extracts the Degraded error if present.
func AsDegraded(err error) (*Degraded, bool) {
	var d *Degraded
	if errors.As(err, &d) {
		return d, true
	}

	return nil, false
}

// FormatPrefix returns a markdown note listing degraded tools.
// Returns empty string when no degraded entries.
func FormatPrefix(degraded []*Degraded) string {
	if len(degraded) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(
		"> ⚠ degraded tooling detected " +
			"— run `shmorby doctor` for details\n",
	)
	for _, d := range degraded {
		dur := ""
		if d.Duration > 0 {
			dur = fmt.Sprintf(" after %s",
				d.Duration.Round(time.Millisecond))
		}

		b.WriteString(fmt.Sprintf(
			"> - %s: %s%s\n", d.Tool, d.Reason, dur,
		))
	}
	b.WriteString("\n")

	return b.String()
}
