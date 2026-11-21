package tools

import (
	"context"

	"shmorby/internal/audit"
)

type contextKey string

const (
	sessionIDKey   contextKey = "session_id"
	inputIDKey     contextKey = "input_id"
	auditLoggerKey contextKey = "audit_logger"
)

// WithSessionID stores the session ID in the context.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey, id)
}

// SessionIDFrom extracts the session ID from the context.
func SessionIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDKey).(string)
	return v
}

// WithInputID stores the client input idempotency key in the
// context.
func WithInputID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, inputIDKey, id)
}

// InputIDFrom extracts the input idempotency key from the context.
func InputIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(inputIDKey).(string)

	return id
}

// WithAuditLogger stores the audit logger in the context.
func WithAuditLogger(ctx context.Context, l *audit.Logger) context.Context {
	return context.WithValue(ctx, auditLoggerKey, l)
}

// AuditLoggerFrom extracts the audit logger from the context.
func AuditLoggerFrom(ctx context.Context) *audit.Logger {
	l, _ := ctx.Value(auditLoggerKey).(*audit.Logger)
	return l
}
