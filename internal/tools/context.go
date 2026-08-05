package tools

import (
	"context"

	"shmorby/internal/audit"
)

type contextKey string

const (
	sessionIDKey   contextKey = "session_id"
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

// WithAuditLogger stores the audit logger in the context.
func WithAuditLogger(ctx context.Context, l *audit.Logger) context.Context {
	return context.WithValue(ctx, auditLoggerKey, l)
}

// AuditLoggerFrom extracts the audit logger from the context.
func AuditLoggerFrom(ctx context.Context) *audit.Logger {
	l, _ := ctx.Value(auditLoggerKey).(*audit.Logger)
	return l
}
