package llm

import (
	"fmt"
	"os"

	"shmorby/internal/config"
)

// NewProvider returns the provider specified in cfg.
//
// Returns an error if the provider's required API key is not set.
func NewProvider(cfg config.Config) (Provider, error) {
	switch cfg.Provider {
	case "ollama":
		return newOllamaProvider(cfg.Ollama.BaseURL, cfg.Model, cfg), nil
	case "openrouter":
		if cfg.OpenRouter.APIKey == "" {
			return nil, fmt.Errorf(
				"openrouter: OPENROUTER_API_KEY is required",
			)
		}
		return newOpenRouterProvider(
			"https://openrouter.ai/api",
			cfg.OpenRouter.APIKey, cfg.Model, cfg,
		), nil
	case "opencode_zen":
		if cfg.OpencodeZen.APIKey == "" {
			return nil, fmt.Errorf(
				"opencode_zen: OPENCODE_ZEN_API_KEY is required",
			)
		}
		baseURL := cfg.OpencodeZen.BaseURL
		if baseURL == "" {
			baseURL = "https://opencode.ai/zen"
		}
		return newOpencodeZenProvider(
			baseURL, cfg.OpencodeZen.APIKey,
			cfg.Model, cfg,
		), nil
	case "openai":
		apiKey := cfg.OpenAI.APIKey
		if apiKey == "" {
			envName := cfg.OpenAI.APIKeyEnv
			if envName == "" {
				envName = "OPENAI_API_KEY"
			}
			apiKey = os.Getenv(envName)
		}
		if apiKey == "" {
			return nil, fmt.Errorf("openai: API key is required")
		}
		baseURL := cfg.OpenAI.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		org := cfg.OpenAI.Organization
		timeout := cfg.OpenAI.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		return newOpenAIProvider(
			baseURL, apiKey, org, cfg.Model, timeout, cfg,
		), nil

	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
