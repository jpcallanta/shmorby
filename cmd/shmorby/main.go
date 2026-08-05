package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"shmorby/internal/agent"
	"shmorby/internal/audit"
	"shmorby/internal/config"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/scope"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	"shmorby/internal/tui"
	tuicl "shmorby/internal/tui/clipboard"
	"shmorby/internal/xdg"
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

			// Log to file so REPL output on stdout stays clean.
			logPath := filepath.Join(xdg.UserDataDir(), "shmorby.log")
			logFile, fErr := os.OpenFile(logPath,
				os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if fErr != nil {
				return fmt.Errorf("open log file: %w", fErr)
			}
			defer logFile.Close()
			logger := slog.New(
				slog.NewTextHandler(
					logFile,
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
			workdir := cfg.Scope.Workdir

			if strings.HasPrefix(workdir, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("resolve workdir tilde: %w", err)
				}
				workdir = filepath.Join(home, workdir[2:])
			}
			if err := os.MkdirAll(workdir, 0o755); err != nil {
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
					workdir,
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

			tFind := tools.NewFindTool(cfg.Permission.Shell)
			reg.Register(tFind)
			if cfg.Tools.WebSearch.Enabled {
				tWS := tools.NewWebSearchTool(cfg.Permission.Shell, nil)
				tWS.SetDefaultTimeout(cfg.Tools.Timeout)
				tWS.SetBaseURL(cfg.Tools.WebSearch.BaseURL)
				tWS.SetEngine(cfg.Tools.WebSearch.Engine)
				if cfg.Tools.WebSearch.ExaAPIKey != "" {
					tWS.SetExaAPIKey(cfg.Tools.WebSearch.ExaAPIKey)
				}
				reg.Register(tWS)
			}
			if cfg.Tools.WebFetch.Enabled {
				tWF := tools.NewWebFetchTool(cfg.Permission.Shell, nil)
				tWF.SetDefaultTimeout(cfg.Tools.Timeout)
				reg.Register(tWF)
			}

			// Initialize audit store and logger.
			var auditLogger *audit.Logger
			if cfg.Audit.Enabled {
				auditStore, aErr := audit.NewAuditStore(cfg.Audit.DBPath)
				if aErr != nil {
					slog.Warn("audit store unavailable, continuing without audit",
						"err", aErr)
				} else {
					auditLogger = audit.NewLogger(
						auditStore,
						cfg.Audit.AsyncBufferSize,
						500*time.Millisecond,
						cfg.Audit.OutputCaptureMaxBytes,
					)
					// Wire audit logger into tools.
					if t, ok := reg.Lookup("shell"); ok {
						if st, ok := t.(*tools.ShellTool); ok {
							st.SetAuditLogger(auditLogger)
						}
					}
					if t, ok := reg.Lookup("ssh"); ok {
						if st, ok := t.(*tools.SSHTool); ok {
							st.SetAuditLogger(auditLogger)
						}
					}
					if t, ok := reg.Lookup("sudo"); ok {
						if st, ok := t.(*tools.SudoTool); ok {
							st.SetAuditLogger(auditLogger)
						}
					}
					if t, ok := reg.Lookup("aws"); ok {
						if st, ok := t.(*tools.AWSTool); ok {
							st.SetAuditLogger(auditLogger)
						}
					}
					if t, ok := reg.Lookup("websearch"); ok {
						if st, ok := t.(*tools.WebSearchTool); ok {
							st.SetAuditLogger(auditLogger)
						}
					}
					if t, ok := reg.Lookup("webfetch"); ok {
						if st, ok := t.(*tools.WebFetchTool); ok {
							st.SetAuditLogger(auditLogger)
						}
					}
				}
			}
			defer func() {
				if auditLogger != nil {
					auditLogger.Close()
				}
			}()

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

			// Build per-tool permission rulesets. The interactive flag
			// only controls whether the user is prompted for "ask"
			// decisions; rules are always evaluated so that presets
			// (destructive, service, etc.) are never bypassed.
			toolRules := make(map[string]*tools.RuleSet)
			for _, tool := range []string{"shell", "ssh", "sudo", "aws"} {
				rs := tools.MergeRules(cfg.Permission.Presets, cfg.Permission.Rules)
				toolRules[tool] = &rs
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
					ctxcomp.NewEstimator(cfg.Model),
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

			// Phase 34: subagent orchestrator with task tool.
			// Channel is non-nil only when TUI is active (see below).
			var subagentEventChan chan tools.SubagentEvent
			orch := &tools.TaskOrchestrator{
				AuditLogger:     auditLogger,
				ParentSessionID: sess.ID(),
				MaxParallel:     5,
			}
			orch.RunSubtask = func(ctx context.Context, task tools.Subtask) tools.TaskResult {
				childSess := session.New()
				if subagentEventChan != nil {
					select {
					case subagentEventChan <- tools.SubagentEvent{
						Session:   childSess,
						SessionID: childSess.ID(),
						ParentID:  sess.ID(),
						Label:     task.ID,
						Status:    "",
					}:
					default:
					}
				}
				// Filter registry to exclude denied tools and
				// pass parent's permission callback to subagent.
				childReg := reg.FilterByPerm()
				var childPermFunc agent.ToolPermissionFunc
				if orch.PermFunc != nil {
					childPermFunc = orch.PermFunc.(agent.ToolPermissionFunc)
				}
				reply, err := agent.RunTurnWithTools(
					ctx, provider, childSess,
					cfg.Agent.Default, scopeResult.Content,
					"", cfg.Model, task.Prompt,
					childReg, cfg.Agent.MaxToolIterations,
					cfg.Tools.Shell.Enabled,
					memStore, memRetriever,
					compressor, modelInfo,
					nil, childPermFunc, toolRules,
					nil, // no status gen for subagents
				)
				status := "ok"
				errStr := ""
				if err != nil {
					status = "error"
					errStr = err.Error()
					reply = ""
				}
				if subagentEventChan != nil {
					select {
					case subagentEventChan <- tools.SubagentEvent{
						Session:   childSess,
						SessionID: childSess.ID(),
						ParentID:  sess.ID(),
						Label:     task.ID,
						Status:    status,
					}:
					default:
					}
				}
				return tools.TaskResult{
					TaskID:      task.ID,
					Description: task.Description,
					Status:      status,
					Output:      reply,
					Error:       errStr,
				}
			}
			// Apply task tool permission level from config.
			taskTool := tools.NewTaskTool(orch)
			taskPerm := cfg.Permission.Task
			if taskPerm == "" {
				taskPerm = "ask"
			}
			taskTool.SetPerm(taskPerm)
			reg.Register(taskTool)

			// Initialize MCP manager.
			var mcpManager *tools.MCPManager
			if len(cfg.MCP.Servers) > 0 {
				mcpManager = tools.NewMCPManager(
					cfg.MCP.Servers, reg,
				)
				if err := mcpManager.Start(
					cmd.Context(),
				); err != nil {
					slog.Warn("MCP manager error", "err", err)
				}

				// Apply MCP permission level.
				mcpPerm := cfg.Permission.MCP
				if mcpPerm == "" {
					mcpPerm = "ask"
				}
				mcpManager.SetDefaultPermLevel(mcpPerm)
			}
			defer func() {
				if mcpManager != nil {
					mcpManager.Shutdown()
				}
			}()

			// Propagate SIGINT/SIGTERM to the root context so
			// running tool executions are cancelled promptly.
			rootCtx, rootCancel := context.WithCancel(cmd.Context())
			defer rootCancel()

			// Inject session ID and audit logger into context.
			rootCtx = tools.WithSessionID(rootCtx, sess.ID())
			if auditLogger != nil {
				rootCtx = tools.WithAuditLogger(rootCtx, auditLogger)
			}

			// Phase 37: build status description generator.
			// On by default — generates descriptions from tool
			// metadata without any LLM call.
			// Set tui.status_model: "off" to disable.
			var statusGen *agent.StatusGenerator
			if cfg.TUI.StatusModel == "off" {
				// Explicitly disabled.
			} else {
				statusGen = agent.NewStatusGenerator(nil, "")
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				rootCancel()
				signal.Stop(sigCh)
			}()

			// Use TUI when terminal and --no-tui not set.
			if !noTuiFlag && isTerminal() {
				if err := tuicl.Init(); err != nil {
					slog.Warn("clipboard unavailable, copy/paste disabled", "err", err)
				}
				scrollLines := cfg.TUI.Nav.ScrollLinesPerTick
				if scrollLines <= 0 {
					scrollLines = 5
				}

				// Wire subagent event channel for TUI tab lifecycle.
				subagentEventChan = make(chan tools.SubagentEvent, 20)

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
					SubagentEventChan:    subagentEventChan,
					Orchestrator:         orch,
					StatusGen:            statusGen,
				})
				opts := []tea.ProgramOption{}
				if cfg.TUI.Fullscreen {
					opts = append(opts, tea.WithAltScreen())
				}
				p := tea.NewProgram(m, opts...)
				_, err := p.Run()
				if err != nil {
					return fmt.Errorf("run TUI: %w", err)
				}
				return nil
			}

			// Fall back to plain REPL.
			repl := &agent.REPL{
				NoTUI:        noTuiFlag,
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
				Orchestrator:    orch,
				StatusGen:       statusGen,
			}

			return repl.Run(rootCtx)
		},
	}
)

// configCmd is the parent for `shmorby config` subcommands.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Config management (migrate, validate, show)",
}

var (
	configFileFlag string
	configDryRun   bool
	configQuiet    bool
)

func init() {
	// ── config migrate ──────────────────────────────────────
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Merge missing config fields from defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			src := configFileFlag
			if src == "" {
				src = filepath.Join(xdg.UserConfigDir(), "config.yaml")
			}
			dst := src // write back in place

			if configDryRun {
				if err := config.DryMigrate(src, dst); err != nil {
					return err
				}
				cmd.Println("No files written (--dry-run).")
				return nil
			}

			if err := config.Migrate(src, dst); err != nil {
				return err
			}
			cmd.Printf("✓ Config migrated: %s\n", src)
			return nil
		},
	}
	migrateCmd.Flags().StringVar(&configFileFlag, "file", "", "config file path (default: ~/.config/shmorby/config.yaml)")
	migrateCmd.Flags().BoolVar(&configDryRun, "dry-run", false, "show diff without writing")
	configCmd.AddCommand(migrateCmd)

	// ── config show ──────────────────────────────────────────
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print current default config as YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Print(config.ShowDefaults())
			return nil
		},
	}
	configCmd.AddCommand(showCmd)

	// ── config validate ──────────────────────────────────────
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a config file against the schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configFileFlag
			if path == "" {
				path = filepath.Join(xdg.UserConfigDir(), "config.yaml")
			}

			if err := config.ValidateFile(path); err != nil {
				return err
			}
			if !configQuiet {
				cmd.Printf("✓ %s: valid\n", path)
			}
			return nil
		},
	}
	validateCmd.Flags().StringVar(&configFileFlag, "file", "", "config file path (default: ~/.config/shmorby/config.yaml)")
	validateCmd.Flags().BoolVarP(&configQuiet, "quiet", "q", false, "suppress non-error output")
	configCmd.AddCommand(validateCmd)

	rootCmd.AddCommand(configCmd)
}

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

Subcommands:
  config migrate          Merge missing config fields from defaults
  config show             Print default config as YAML
  config validate         Validate a config file

Config file (config.yaml):
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
  /set        Override a config parameter at runtime
  /quit       Exit shmorby
  /reset      Clear conversation history
  /model      Switch LLM model
  /platform   Switch LLM provider
  /apikey     Set API key for current provider
  /agent      Switch agent mode (operate, diagnose, chat)
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
