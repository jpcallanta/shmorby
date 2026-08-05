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

// cachedModelInfo wraps a resolved ModelInfo. guessed marks values
// from the fallback path so a later config override still applies.
type cachedModelInfo struct {
	info    ModelInfo
	guessed bool
}

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
//
// Fallback results are cached as guesses so repeated lookups of an
// unknown model do not re-hit the network; a cached guess is
// re-checked against config overrides on every call.
func FetchModelInfo(
	ctx context.Context,
	fetcher ModelInfoFetcher,
	model string,
	cfg config.Config,
) (ModelInfo, error) {
	// 1. Check cache.
	if cached, ok := modelInfoCache.Load(model); ok {
		ci := cached.(cachedModelInfo)
		if !ci.guessed {
			return ci.info, nil
		}
		// Cached guess: apply a config override if one exists now,
		// otherwise return the guess as-is.
		if cfg.Models != nil {
			if override, ok := cfg.Models[model]; ok {
				info := ModelInfo{
					ContextWindow:   override.ContextWindow,
					MaxOutputTokens: override.MaxOutputTokens,
				}
				modelInfoCache.Store(model, cachedModelInfo{info: info})
				return info, nil
			}
		}
		return ci.info, ErrModelInfoFallback
	}

	// 2. Try provider-specific API.
	info, err := fetcher.fetchModelInfo(ctx, model)
	if err == nil {
		modelInfoCache.Store(model, cachedModelInfo{info: info})
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
			modelInfoCache.Store(model, cachedModelInfo{info: info})
			return info, nil
		}
	}

	// 4. Fallback — use configured value or sensible default.
	cw := cfg.Context.FallbackContextWindow
	if cw == 0 {
		cw = 8192
	}
	info = ModelInfo{ContextWindow: cw}

	// Cache the guess so repeated lookups of an unknown model do not
	// re-hit the network or repeat the WARN log. Marked guessed so a
	// config override added later still takes precedence.
	modelInfoCache.Store(model, cachedModelInfo{info: info, guessed: true})

	return info, ErrModelInfoFallback
}

// InvalidateModelInfo removes a model from the cache.
// Call on /model switch.
func InvalidateModelInfo(model string) {
	modelInfoCache.Delete(model)
}
