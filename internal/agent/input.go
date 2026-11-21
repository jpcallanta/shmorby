package agent

import (
	"context"
	"log/slog"

	"shmorby/internal/tools"
	"shmorby/internal/xuuid"
)

// WithInputID returns a context carrying a client-generated input
// idempotency key.
func WithInputID(
	ctx context.Context, id string,
) context.Context {
	return tools.WithInputID(ctx, id)
}

// InputIDFrom returns the input idempotency key in ctx, or "".
func InputIDFrom(ctx context.Context) string {
	return tools.InputIDFrom(ctx)
}

// ensureInputID returns ctx with an input key, generating one when
// the client did not supply it.
func ensureInputID(
	ctx context.Context,
) (context.Context, string) {
	if id := InputIDFrom(ctx); id != "" {
		return ctx, id
	}

	id, err := xuuid.New()
	if err != nil {
		slog.Warn("generate input id failed", "err", err)

		return ctx, ""
	}

	return WithInputID(ctx, id), id
}
