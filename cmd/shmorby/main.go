package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"shmorby/internal/agent"
	"shmorby/internal/config"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/scope"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	"shmorby/internal/tui"
	tuicl "shmorby/internal/tui/clipboard"
)

var (
	logLevelFlag = "info"
	providerFlag = ""
	modelFlag    = ""
	configFile   = ""
	agentFlag    = ""
	scopeFile    = ""
	systemPrompt = ""
	noTuiFlag    = false
	validateFlag = false
	rootCmd      = &cobra.Command{
		Use:           "shmorby",
		Short:         "AI sysadmin agent harness",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := parseLogLevel(logLevelFlag)

			// Validates the flag and may reject invalid values.
			if err != nil {
				return fmt.Errorf("parse log level: %w", err)
			}

			logger := slog.New(
				slog.NewTextHandler(
					os.Stdout,
					&slog.HandlerOptions{Level: level},
				),
			)
			slog.SetDefault(logger)

			cfg, err := config.Load(config.LoadOptions{
				ConfigFile: configFile,
				Provider:   providerFlag,
				Model:      modelFlag,
				Agent:      agentFlag,
			})
			if err != nil {
				if validateFlag {
					return fmt.Errorf("config invalid:\n%s", err)
				}
				return fmt.Errorf("load config: %w", err)
			}

			if validateFlag {
				cmd.Println("config valid")
				return nil
			}

			scopeResult, err := scope.Load(cfg, scope.Flags{ScopeFile: scopeFile})
			if err != nil {
				return fmt.Errorf("load scope: %w", err)
			}

			// Ensure workdir exists for shell tool.
			if err := os.MkdirAll(cfg.Scope.Workdir, 0o755); err != nil {
				return fmt.Errorf("create workdir: %w", err)
			}

			slog.Info("loaded config", "provider", cfg.Provider, "model", cfg.Model)

			provider, err := llm.NewProvider(cfg)
			if err != nil {
				return fmt.Errorf("init provider: %w", err)
			}

			reg := tools.NewRegistry()
			if cfg.Tools.Shell.Enabled {
				t := tools.NewShellTool(
					cfg.Agent.Shell,
					cfg.Scope.Workdir,
					cfg.Permission.Shell,
				)
				t.SetDefaultTimeout(cfg.Tools.Timeout)
				reg.Register(t)
			}
			tSSH := tools.NewSSHTool(cfg.Permission.SSH, nil)
			tSSH.SetDefaultTimeout(cfg.Tools.Timeout)
			reg.Register(tSSH)
			if cfg.Tools.Sudo.Enabled {
				tSudo := tools.NewSudoTool(cfg.Permission.Sudo, nil)
				tSudo.SetDefaultTimeout(cfg.Tools.Timeout)
				reg.Register(tSudo)
			}
			if cfg.Tools.AWS.Enabled {
				tAWS := tools.NewAWSTool(cfg.Permission.AWS, nil)
				tAWS.SetDefaultTimeout(cfg.Tools.Timeout)
				reg.Register(tAWS)
			}

			// Initialize memory store.
			var memStore memory.Store
			var memRetriever *memory.Retriever
			if cfg.Memory.Enabled {
				// Wire embedder based on provider config.
				var emb memory.Embedder
				switch cfg.Memory.Embedding.Provider {
				case "ollama":
					baseURL := cfg.Ollama.BaseURL
					if cfg.Memory.Embedding.BaseURL != "" {
						baseURL = cfg.Memory.Embedding.BaseURL
					}
					emb = memory.NewOllamaEmbedder(
						baseURL, cfg.Memory.Embedding.Model,
					)
				case "openai":
					if cfg.OpenAI.APIKey != "" {
						emb = memory.NewOpenAIEmbedder(
							cfg.OpenAI.APIKey,
							cfg.Memory.Embedding.BaseURL,
							cfg.Memory.Embedding.Model,
						)
					}
				}

				// Probe embedding endpoint; skip memory if unreachable.
				if emb != nil {
					pCtx, pCancel := context.WithTimeout(
						cmd.Context(), 5*time.Second,
					)
					defer pCancel()

					_, pErr := emb.Embed(pCtx, []string{"ping"})
					if pErr != nil {
						slog.Warn(
							"embedding endpoint unreachable, memory disabled",
							"err", pErr,
						)
					} else {
						memCfg := memory.Config{
							Enabled:     cfg.Memory.Enabled,
							DBPath:      cfg.Memory.DBPath,
							MaxEntries:  cfg.Memory.MaxEntries,
							AutoCapture: cfg.Memory.AutoCapture,
						}

						var mErr error
						memStore, mErr = memory.NewStore(memCfg, emb)
						if mErr != nil {
							slog.Warn("memory store unavailable, continuing without memory",
								"err", mErr)
						} else {
							memRetriever = memory.NewRetriever(memStore, 5)

							// Wire vector search into the retriever.
							vs, vEmb := memory.StoreVectorSearch(memStore)
							if vs != nil && vEmb != nil {
								memRetriever.SetVectorSearch(vs, vEmb)
							}

							// Re-index existing SQLite entries.
							_ = memory.StoreMigrateVectors(
								cmd.Context(), memStore,
							)
						}
					}
				} else {
					memCfg := memory.Config{
						Enabled:     cfg.Memory.Enabled,
						DBPath:      cfg.Memory.DBPath,
						MaxEntries:  cfg.Memory.MaxEntries,
						AutoCapture: cfg.Memory.AutoCapture,
					}

					var mErr error
					memStore, mErr = memory.NewStore(memCfg, nil)
					if mErr != nil {
						slog.Warn("memory store unavailable, continuing without memory",
							"err", mErr)
					} else {
						memRetriever = memory.NewRetriever(memStore, 5)
					}
				}
			}

			// Build per-tool permission rulesets when interactive mode
			// is enabled. Nil rules preserves v1 "ask" = silently allow.
			var toolRules map[string]*tools.RuleSet
			if cfg.Permission.Interactive {
				toolRules = make(map[string]*tools.RuleSet)
				for _, tool := range []string{"shell", "ssh", "sudo", "aws"} {
					rs := tools.MergeRules(cfg.Permission.Presets, cfg.Permission.Rules)
					toolRules[tool] = &rs
				}
			}

			// Apply tool output byte cap from config (0 = unlimited).
			if cfg.Context.MaxToolOutputBytes > 0 {
				tools.MaxOutput = cfg.Context.MaxToolOutputBytes
			} else {
				tools.MaxOutput = 0
			}

			// Build compressor from config.
			var compressor *ctxcomp.Compressor
			var modelInfo llm.ModelInfo
			if cfg.Context.Enabled {
				var estimator ctxcomp.Estimator
				if cfg.Context.TokenEstimator == "tiktoken" {
					estimator = ctxcomp.NewTiktokenEstimator(cfg.Model)
				} else {
					estimator = &ctxcomp.HeuristicEstimator{}
				}

				// Read model override for context window info.
				if mo, ok := cfg.Models[cfg.Model]; ok {
					modelInfo = llm.ModelInfo{
						ContextWindow:   mo.ContextWindow,
						MaxOutputTokens: mo.MaxOutputTokens,
					}
				}

				compressor = ctxcomp.NewCompressor(
					ctxcomp.CompressorConfig{
						Enabled:               cfg.Context.Enabled,
						Mode:                  cfg.Context.Mode,
						Threshold:             cfg.Context.Threshold,
						MaxToolOutputTokens:   cfg.Context.MaxToolOutputTokens,
						MaxToolOutputLines:    cfg.Context.MaxToolOutputLines,
						SummaryModel:          cfg.Context.SummaryModel,
						SummaryProvider:       cfg.Context.SummaryProvider,
						OffloadToMemory:       cfg.Context.OffloadToMemory,
						MinMessagesToCompress: cfg.Context.MinMessagesToCompress,
						FallbackContextWindow: cfg.Context.FallbackContextWindow,
					},
					memStore,
					estimator,
					nil, // summaryFunc: no LLM summarizer wired yet
				)
			}

			// Phase 32: Build runtime config overrider.
			sess := session.New()
			overrider := agent.NewConfigOverrider(
				&cfg,
				&provider,
				reg,
				compressor,
				sess,
				agent.WithLogLevelSetter(func(level string) {
					l, err := parseLogLevel(level)
					if err == nil {
						slog.SetLogLoggerLevel(l)
					}
				}),
				agent.WithMemoryStore(memStore), // propagates auto_capture at runtime
			)

			// Use TUI when terminal and --no-tui not set.
			if !noTuiFlag && isTerminal() {
				if err := tuicl.Init(); err != nil {
					slog.Warn("clipboard unavailable, copy/paste disabled", "err", err)
				}
				scrollLines := cfg.TUI.Nav.ScrollLinesPerTick
				if scrollLines <= 0 {
					scrollLines = 5
				}

				// Wire TUI log handler when logging is enabled.
				var logHandler *tui.TUILogHandler
				var logChan chan tui.LogEntry
				logDefaultLevel := cfg.TUI.Logging.DefaultLevel
				if logDefaultLevel == "" {
					logDefaultLevel = "info"
				}
				if cfg.TUI.Logging.Enabled {
					logChan = make(chan tui.LogEntry, 100)
					logHandler = tui.NewTUILogHandler(
						slog.Default().Handler(), logChan,
					)
					slog.SetDefault(slog.New(logHandler))
				}

				m := tui.NewModel(tui.Config{
					Provider:       provider,
					Session:        sess,
					Mode:           cfg.Agent.Default,
					Scope:          scopeResult.Content,
					Model:          cfg.Model,
					Override:       systemPrompt,
					Registry:       reg,
					MaxToolIter:    cfg.Agent.MaxToolIterations,
					ShellEnabled:   cfg.Tools.Shell.Enabled,
					Fullscreen:     cfg.TUI.Fullscreen,
					ThemeName:      cfg.TUI.Theme,
					GlamourEnabled: cfg.TUI.Glamour.Enabled,
					ScrollLines:    scrollLines,
					FollowMode:     cfg.TUI.Nav.FollowMode,
					ToolTimeout:    cfg.Tools.Timeout,
					ScopeInfo: tui.ScopeInfo{
						PrimaryPath:  scopeResult.PrimaryPath,
						Instructions: scopeResult.Instructions,
						TotalBytes:   scopeResult.TotalBytes,
					},
					MemoryStore:          memStore,
					Retriever:            memRetriever,
					Compressor:           compressor,
					ModelInfo:            modelInfo,
					LogEnabled:           cfg.TUI.Logging.Enabled,
					LogDefaultLevel:      logDefaultLevel,
					LogMaxEntries:        cfg.TUI.Logging.MaxEntries,
					LogDisplayLimit:      cfg.TUI.Logging.DisplayLimit,
					LogCollapse:          cfg.TUI.Logging.Collapse,
					LogCollapseThreshold: cfg.TUI.Logging.CollapseThreshold,
					LogChan:              logChan,
					LogHandler:           logHandler,
					ToolRules:            toolRules,
					ConfigOverrider:      overrider,
				})
				opts := []tea.ProgramOption{}
				if cfg.TUI.Fullscreen {
					opts = append(opts, tea.WithAltScreen())
				}
				p := tea.NewProgram(m, opts...)
				_, err := p.Run()
				return fmt.Errorf("run TUI: %w", err)
			}

			// Fall back to plain REPL.
			repl := &agent.REPL{
				Provider:     provider,
				Session:      sess,
				Mode:         cfg.Agent.Default,
				Model:        cfg.Model,
				Scope:        scopeResult.Content,
				Override:     systemPrompt,
				In:           os.Stdin,
				Out:          os.Stdout,
				Registry:     reg,
				MaxToolIter:  cfg.Agent.MaxToolIterations,
				ShellEnabled: cfg.Tools.Shell.Enabled,
				ScopeInfo: agent.ScopeInfo{
					PrimaryPath:  scopeResult.PrimaryPath,
					Instructions: scopeResult.Instructions,
					TotalBytes:   scopeResult.TotalBytes,
				},
				Store:           memStore,
				Retriever:       memRetriever,
				Compressor:      compressor,
				ModelInfo:       modelInfo,
				ToolRules:       toolRules,
				ConfigOverrider: overrider,
			}

			return repl.Run(cmd.Context())
		},
	}
)

// Registers CLI flags.
func init() {
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprint(cmd.OutOrStdout(), `shmorby — AI sysadmin agent harness

Shmorby is an AI sysadmin agent that operates infrastructure via shell,
SSH, sudo, and AWS CLI commands. Use it for deployment, configuration,
monitoring, and diagnostics tasks.

Usage:
  shmorby [flags]

Flags:
  --validate              Validate config and exit
  --provider string       LLM provider: openrouter, opencode_zen,
                          openai, ollama (default "ollama")
  --model string          Model name (default "llama3.2")
  --config string         Config file path
  --scope-file string     Operational context markdown (SCOPE.md)
  --agent string          Agent mode: operate, diagnose (default "operate")
  --system-prompt-file    Override path to system prompt txt
  --no-tui                Disable TUI, use plain stdin/stdout REPL
  --log-level string      Log level: debug, info, warn, error (default "info")
  --version               Print version and exit

Config file (shmorby.yaml):
  Loaded from (first match wins):
    1. /etc/shmorby/config.yaml (Unix) /
       %ProgramData%\shmorby\config.yaml (Windows)
    2. ~/.config/shmorby/config.yaml or
       $XDG_CONFIG_HOME/shmorby/config.yaml (Unix) /
       %APPDATA%\shmorby\config.yaml (Windows)
    3. --config flag
    4. ./shmorby.yaml in cwd
  See examples/shmorby.yaml for full reference.

Slash commands (in TUI or stdin REPL):
  /help       Show this help
  /quit       Exit shmorby
  /reset      Clear conversation history
  /model      Switch LLM model
  /agent      Switch agent mode (operate, diagnose)
  /scope      Show loaded scope context
  /memory     Memory management
  /context    Token usage and compression stats
  /log        Set log verbosity (debug, info, warn, error)
  /tui        Toggle fullscreen mode

Quick start:
  1. Install and run Ollama: ollama pull llama3.2
  2. Run: shmorby
  3. Type a sysadmin task: "check nginx status on all hosts"

  Or with an API provider:
    shmorby --provider openai --model gpt-4o
`)
	})

	rootCmd.Flags().StringVar(
		&logLevelFlag,
		"log-level",
		"info",
		"debug|info|warn|error",
	)
	rootCmd.Flags().StringVar(
		&providerFlag, "provider", "", "ollama|openrouter|opencode_zen|openai")
	rootCmd.Flags().StringVar(
		&modelFlag, "model", "", "LLM model id")
	rootCmd.Flags().StringVar(
		&configFile, "config", "", "config yaml path")
	rootCmd.Flags().StringVar(
		&agentFlag, "agent", "", "operate|diagnose")
	rootCmd.Flags().StringVar(
		&scopeFile, "scope-file", "", "operational context markdown")
	rootCmd.Flags().StringVar(
		&systemPrompt, "system-prompt-file", "", "system prompt override file")
	rootCmd.Flags().BoolVar(
		&noTuiFlag, "no-tui", false, "disable TUI, use plain REPL")
	rootCmd.Flags().BoolVar(
		&validateFlag, "validate", false, "validate config and exit")
}

// Runs the root command.
func main() {
	// Exits non-zero on command execution error.
	if err := execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

// Runs the root Cobra command.
func execute() error {

	return rootCmd.Execute()
}

// Converts a string log level into a slog level.
func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":

		return slog.LevelDebug, nil
	case "info":

		return slog.LevelInfo, nil
	case "warn":

		return slog.LevelWarn, nil
	case "error":

		return slog.LevelError, nil
	default:

		return 0, fmt.Errorf(
			"invalid --log-level %q (want debug|info|warn|error)",
			s,
		)
	}
}

// isTerminal checks if stdin is a terminal device.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
