package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"shmorby/internal/config"
)

// ModelInfoFetcher retrieves model metadata from a provider API.
type ModelInfoFetcher interface {
	fetchModelInfo(ctx context.Context, model string) (ModelInfo, error)
}

var modelInfoCache sync.Map

// ErrModelInfoFallback indicates the returned ModelInfo is a guess
// (8192 context window) because neither the API nor config override
// provided real data.
var ErrModelInfoFallback = fmt.Errorf(
	"model info: using fallback context window",
)

// FetchModelInfo resolves model metadata using the resolution order:
// 1. Cache
// 2. Provider API (live fetch)
// 3. Config override
// 4. Fallback (8192 context window) — returns ErrModelInfoFallback
func FetchModelInfo(
	ctx context.Context,
	fetcher ModelInfoFetcher,
	model string,
	cfg config.Config,
) (ModelInfo, error) {
	// 1. Check cache.
	if cached, ok := modelInfoCache.Load(model); ok {
		return cached.(ModelInfo), nil
	}

	// 2. Try provider-specific API.
	info, err := fetcher.fetchModelInfo(ctx, model)
	if err == nil {
		modelInfoCache.Store(model, info)
		return info, nil
	}
	slog.Warn(
		"failed to fetch model info from API",
		"model", model, "err", err,
	)

	// 3. Check config overrides.
	if cfg.Models != nil {
		if override, ok := cfg.Models[model]; ok {
			info = ModelInfo{
				ContextWindow:   override.ContextWindow,
				MaxOutputTokens: override.MaxOutputTokens,
			}
			modelInfoCache.Store(model, info)
			return info, nil
		}
	}

	// 4. Fallback — use configured value or sensible default.
	cw := cfg.Context.FallbackContextWindow
	if cw == 0 {
		cw = 8192
	}
	return ModelInfo{ContextWindow: cw}, ErrModelInfoFallback
}

// InvalidateModelInfo removes a model from the cache.
// Call on /model switch.
func InvalidateModelInfo(model string) {
	modelInfoCache.Delete(model)
}
