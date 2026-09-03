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

	"github.com/spf13/cobra"
	"shmorby/internal/agent"
	"shmorby/internal/config"
	"shmorby/internal/llm"
	"shmorby/internal/scope"
	"shmorby/internal/session"
	"shmorby/internal/tools"
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
	continueFlag = false
	sessionFlag  = ""
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
					return fmt.Errorf("config invalid:\n%w", err)
				}
				return fmt.Errorf("load config: %w", err)
			}

			if validateFlag {
				cmd.Println("config valid")
				return nil
			}

			// Open the session store and resolve the root session,
			// honoring --continue/--session. A resume
			// from another directory chdirs to the stored one and
			// re-reads config there before anything else resolves
			// paths (opencode failure mode).
			sessStore, sess, err := openRootSession(cmd, &cfg)
			if err != nil {
				return err
			}
			// Flush session state to the store before the store is
			// closed. Catches pending metadata mutations that never
			// triggered a turn. The Close defer is
			// registered first so LIFO runs Sync() before Close().
			if sessStore != nil {
				defer sessStore.Close()
			}
			if sess != nil {
				defer func() {
					if err := sess.Sync(); err != nil {
						slog.Warn("session sync on exit failed",
							"err", err)
					}
				}()
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

			// Resolve the project root for code-mode file tools.
			// Defaults to the launch CWD (matching opencode behavior);
			// a resume from another directory has already chdir'd, so
			// "launch CWD" is the session directory there.
			projectRoot, prErr := tools.NewProjectRoot(
				cfg.Code.Workdir,
				cfg.Code.AllowedPatterns,
				cfg.Code.BlockedPatterns,
			)
			if prErr != nil {
				return fmt.Errorf("init project root: %w", prErr)
			}
			slog.Info("project root", "path", projectRoot.Root)

			// Register all configured tools on the registry.
			reg := tools.NewRegistry()
			ledgerCtx := registerTools(reg, &cfg, workdir, projectRoot)

			// Wire audit logger into tools when enabled.
			auditLogger := initAuditLogger(&cfg, reg)
			defer func() {
				if auditLogger != nil {
					auditLogger.Close()
				}
			}()

			// Initialize memory store and retriever.
			memStore, memRetriever := initMemoryStore(
				cmd.Context(), &cfg,
			)

			// Build per-tool permission rulesets. The interactive flag
			// only controls whether the user is prompted for "ask"
			// decisions; rules are always evaluated so that presets
			// (destructive, service, etc.) are never bypassed.
			toolRules := make(map[string]*tools.RuleSet)
			for _, tool := range []string{
				tools.ToolShell, tools.ToolSSH, tools.ToolSudo, tools.ToolAWS,
			} {
				rs := tools.MergeRules(cfg.Permission.Presets, cfg.Permission.Rules)
				toolRules[tool] = &rs
			}

			// Apply tool output byte cap from config (0 = unlimited).
			tools.MaxOutput.Store(int64(cfg.Context.MaxToolOutputBytes))

			// Build summarizer provider when compression is enabled
			// (may differ from main provider).
			var sumProvider llm.Provider
			if cfg.Context.Enabled {
				sumProvider = initSummarizerProvider(&cfg, provider)
			}

			// Build context compressor from config.
			compressor, modelInfo := initCompressor(
				&cfg, memStore, sumProvider,
			)

			// Build runtime config overrider. The root session was
			// resolved above (fresh or resumed) in openRootSession.
			overrider := agent.NewConfigOverrider(
				&cfg,
				&provider,
				reg,
				compressor,
				agent.WithLogLevelSetter(func(level string) {
					l, err := parseLogLevel(level)
					if err == nil {
						slog.SetLogLoggerLevel(l)
					}
				}),
				agent.WithMemoryStore(memStore), // propagates auto_capture at runtime
				agent.WithSessionMetaUpdater(func() {
					// Keep the session's persisted metadata in sync
					// with runtime config changes so Sync on exit
					// writes current values.
					sess.UpdateMeta(
						cfg.Provider, cfg.Model, cfg.Agent.Default,
					)
				}),
			)

			// Subagent orchestrator with task tool.
			// Buffering allows non-blocking sends from the
			// orchestrator even in REPL mode.
			subagentEventChan := make(
				chan tools.SubagentEvent, 20,
			)

			// Compute subtask timeout from config. Each subtask
			// can run up to MaxToolIterations tool calls, each
			// with its own timeout (tools.timeout). When
			// tools.subtask_timeout is set, use it directly.
			// Otherwise derive a generous upper bound:
			// tools.timeout × maxIterations × 2.
			// Prevents indefinite subagent hangs.
			var subtaskTimeout time.Duration
			if cfg.Tools.SubtaskTimeout > 0 {
				subtaskTimeout = time.Duration(
					cfg.Tools.SubtaskTimeout,
				) * time.Second
			} else {
				subtaskTimeout = time.Duration(
					cfg.Tools.Timeout,
				) * time.Second * time.Duration(
					cfg.Agent.MaxToolIterations,
				) * 2
				if subtaskTimeout > 0 && subtaskTimeout < time.Minute {
					subtaskTimeout = time.Minute
				}
			}

			orch := &tools.TaskOrchestrator{
				AuditLogger:     auditLogger,
				ParentSessionID: sess.ID(),
				MaxParallel:     5,
				SubtaskTimeout:  subtaskTimeout,
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
				// Filter registry to exclude denied tools for subagents.
				// Subagents never receive the parent's permission callback;
				// passing nil defaults to PermAllow, preventing parallel
				// stdin reads that deadlock the terminal.
				childReg := reg.FilterByPerm()
				reply, err := agent.RunTurnWithTools(
					ctx, provider, childSess,
					cfg.Agent.Default, scopeResult.Content,
					"", cfg.Model, task.Prompt,
					childReg, cfg.Agent.MaxToolIterations,
					cfg.Tools.Shell.Enabled,
					memStore, memRetriever,
					compressor, modelInfo,
					nil, nil, toolRules,
					nil, // no status gen for subagents
					ledgerCtx,
					projectRoot.Root,
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
			if err := reg.Register(taskTool); err != nil {
				slog.Warn("task tool registration failed", "err", err)
			}

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

			// Wire session ID into providers that need it
			// (e.g. OpenCode Zen requires X-Opencode-Session).
			if sp, ok := provider.(llm.SessionProvider); ok {
				sp.SetSessionID(sess.ID())
			}

			// Build status description generator.
			// On by default — generates descriptions from tool
			// metadata without any LLM call.
			// Set tui.status_model: "off" to disable.
			var statusGen *agent.StatusGenerator
			if cfg.TUI.StatusModel == "off" {
				// Explicitly disabled.
			} else {
				statusGen = agent.NewStatusGenerator()
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				rootCancel()
				signal.Stop(sigCh)
			}()

			// Use TUI when terminal and --no-tui not set.
			return runTUIOrREPL(
				rootCtx, &cfg, provider, sess, reg,
				compressor, modelInfo, scopeResult,
				memStore, memRetriever, toolRules,
				overrider, orch,
				statusGen, ledgerCtx,
				projectRoot.Root,
				subagentEventChan,
			)
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
		Short: "Merge missing config fields from defaults (exhaustive)",
		Long: `Merge every missing default field into an existing YAML file.

Exhaustive backfill: every non-empty default and every
enabled=false is injected when absent, so migrate --dry-run is
exhaustive and a fresh DefaultConfig round-trips. Empty strings,
empty maps/slices and other zero values remain omitted intentionally
and are documented in examples/shmorby.yaml. Comments and file
permissions are preserved via yaml.Node round-trip and atomic
tmp+rename.`,
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
  --agent string          Agent mode: operate, diagnose, chat, code (default "operate")
  --system-prompt-file    Override path to system prompt txt
  --no-tui                Disable TUI, use plain stdin/stdout REPL
  --log-level string      Log level: debug, info, warn, error (default "info")
  -c, --continue          Resume the most recent session for this directory
  -s, --session string    Resume a specific session by id
  --version               Print version and exit

Subcommands:
  config migrate          Merge missing config fields from defaults
  config show             Print default config as YAML
  config validate         Validate a config file
  audit list              List audit entries with optional filters
  audit get <id>          Show a single audit entry with output
  audit session <id>      Show all audit entries for a session
  audit export            Export audit entries (json or csv)
  audit vacuum            Archive and remove old audit entries
  audit stats             Show audit DB statistics
  session list            List persisted sessions (current directory)
  session show <id>       Show session metadata or full transcript
  session rm <id>         Archive a session (--force deletes rows)
  session prune           Apply session retention policy
  ledger list             List encrypted environment ledger sections
  ledger get <section>    Print a ledger section as JSON
  ledger set <section>    Replace a ledger section with JSON
  ledger delete <section> Remove a ledger section
  doctor                  Run self-diagnostics and report tool health

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
  /agent      Switch agent mode (operate, diagnose, chat, code)
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
		&agentFlag, "agent", "", "operate|diagnose|chat|code")
	rootCmd.Flags().StringVar(
		&scopeFile, "scope-file", "", "operational context markdown")
	rootCmd.Flags().StringVar(
		&systemPrompt, "system-prompt-file", "", "system prompt override file")
	rootCmd.Flags().BoolVar(
		&noTuiFlag, "no-tui", false, "disable TUI, use plain REPL")
	rootCmd.Flags().BoolVar(
		&validateFlag, "validate", false, "validate config and exit")
	rootCmd.Flags().BoolVarP(
		&continueFlag, "continue", "c", false,
		"resume the most recent session for this directory")
	rootCmd.Flags().StringVarP(
		&sessionFlag, "session", "s", "",
		"resume a specific session by id")
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
