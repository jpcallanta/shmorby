package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
			return resolveVision(model, ci.info, cfg), nil
		}
		// Cached guess: apply a config override if one exists now,
		// otherwise return the guess as-is.
		if cfg.Models != nil {
			if override, ok := cfg.Models[model]; ok {
				info := ModelInfo{
					ContextWindow:   override.ContextWindow,
					MaxOutputTokens: override.MaxOutputTokens,
				}
				info = resolveVision(model, info, cfg)
				modelInfoCache.Store(model, cachedModelInfo{info: info})

				return info, nil
			}
		}
		return resolveVision(model, ci.info, cfg), ErrModelInfoFallback
	}

	// 2. Try provider-specific API.
	info, err := fetcher.fetchModelInfo(ctx, model)
	if err == nil {
		info = resolveVision(model, info, cfg)
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
			info = resolveVision(model, info, cfg)
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
	info = resolveVision(model, info, cfg)

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

// visionModelPrefixes lists model families known to accept image
// input, used when no API surface or config override reports vision
// capability. Matching is case-insensitive on the model name prefix
// (after any vendor prefix). Unknown models default to false — the
// feature is refused, not broken.
var visionModelPrefixes = []string{
	// OpenAI
	"gpt-4o", "gpt-4.1", "gpt-5", "o3", "o4-mini",
	// Anthropic / Google
	"claude-", "gemini-",
	// Popular Ollama vision models
	"llava", "qwen2.5vl", "qwen3vl", "minicpm-v",
}

// Reports whether the model name matches a known vision-capable
// family. Vendor-prefixed IDs (e.g. "openai/gpt-4o") are stripped
// before matching.
func supportsVisionHeuristic(model string) bool {
	if i := strings.IndexByte(model, '/'); i >= 0 {
		model = model[i+1:]
	}
	lower := strings.ToLower(model)
	for _, p := range visionModelPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	return false
}

// Resolves vision capability: an explicit config override always
// wins; otherwise the provider-reported value is kept, and when the
// provider reported nothing the known-model heuristic decides.
// Overrides may be keyed by the bare model name even when the
// runtime ID carries a vendor prefix (e.g. "openai/gpt-4o"), so
// both keys are tried.
func resolveVision(model string, info ModelInfo, cfg config.Config) ModelInfo {
	if cfg.Models != nil {
		keys := []string{model}

		if i := strings.IndexByte(model, '/'); i >= 0 {
			keys = append(keys, model[i+1:])
		}
		for _, key := range keys {
			if ov, ok := cfg.Models[key]; ok && ov.SupportsVision != nil {
				info.SupportsVision = *ov.SupportsVision

				return info
			}
		}
	}

	if !info.SupportsVision && supportsVisionHeuristic(model) {
		info.SupportsVision = true
	}

	return info
}
