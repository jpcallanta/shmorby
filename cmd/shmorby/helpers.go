package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"shmorby/internal/agent"
	"shmorby/internal/audit"
	"shmorby/internal/config"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/ledger"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/scope"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	"shmorby/internal/tui"
	tuicl "shmorby/internal/tui/clipboard"
)

// Wires all configured tools onto the registry and returns the
// ledger context string for system-prompt injection. Ledger
// context is empty when ledger is disabled or unavailable.
// projectRoot is the resolved project directory for code-mode
// file tools; may be nil when code mode is not configured.
func registerTools(
	reg *tools.Registry,
	cfg *config.Config,
	workdir string,
	projectRoot *tools.ProjectRoot,
) string {
	if cfg.Tools.Shell.Enabled {
		t := tools.NewShellTool(
			cfg.Agent.Shell,
			workdir,
			cfg.Permission.Shell,
		)
		t.SetDefaultTimeout(cfg.Tools.Timeout)
		// Built-in tools should never collide; log if they do.
		if err := reg.Register(t); err != nil {
			slog.Warn("shell tool registration failed", "err", err)
		}
	}

	tSSH := tools.NewSSHTool(cfg.Permission.SSH, nil)
	tSSH.SetDefaultTimeout(cfg.Tools.Timeout)
	if err := reg.Register(tSSH); err != nil {
		slog.Warn("ssh tool registration failed", "err", err)
	}

	if cfg.Tools.Sudo.Enabled {
		tSudo := tools.NewSudoTool(cfg.Permission.Sudo, nil)
		tSudo.SetDefaultTimeout(cfg.Tools.Timeout)
		if err := reg.Register(tSudo); err != nil {
			slog.Warn("sudo tool registration failed", "err", err)
		}
	}

	if cfg.Tools.AWS.Enabled {
		tAWS := tools.NewAWSTool(cfg.Permission.AWS, nil)
		tAWS.SetDefaultTimeout(cfg.Tools.Timeout)
		if err := reg.Register(tAWS); err != nil {
			slog.Warn("aws tool registration failed", "err", err)
		}
	}

	tFind := tools.NewFindTool(cfg.Permission.Find)
	if projectRoot != nil {
		tFind.SetProjectRoot(projectRoot)
	}
	if err := reg.Register(tFind); err != nil {
		slog.Warn("find tool registration failed", "err", err)
	}

	// Register code-mode tools. When a project root is configured,
	// file operations are confined to the project directory.
	tFileRead := tools.NewFileReadTool(cfg.Permission.FileRead)
	if projectRoot != nil {
		tFileRead.SetProjectRoot(projectRoot)
	}
	if err := reg.Register(tFileRead); err != nil {
		slog.Warn("file_read tool registration failed", "err", err)
	}
	tFileEdit := tools.NewFileEditTool(cfg.Permission.FileEdit)
	if projectRoot != nil {
		tFileEdit.SetProjectRoot(projectRoot)
	}
	if err := reg.Register(tFileEdit); err != nil {
		slog.Warn("file_edit tool registration failed", "err", err)
	}
	tFileWrite := tools.NewFileWriteTool(cfg.Permission.FileWrite)
	if projectRoot != nil {
		tFileWrite.SetProjectRoot(projectRoot)
	}
	if err := reg.Register(tFileWrite); err != nil {
		slog.Warn("file_write tool registration failed", "err", err)
	}
	tGrep := tools.NewGrepTool(cfg.Permission.Grep)
	if projectRoot != nil {
		tGrep.SetProjectRoot(projectRoot)
	}
	if err := reg.Register(tGrep); err != nil {
		slog.Warn("grep tool registration failed", "err", err)
	}

	if cfg.Tools.WebSearch.Enabled {
		tWS := tools.NewWebSearchTool(cfg.Permission.Shell, nil)
		tWS.SetDefaultTimeout(cfg.Tools.Timeout)
		tWS.SetBaseURL(cfg.Tools.WebSearch.BaseURL)
		tWS.SetEngine(cfg.Tools.WebSearch.Engine)
		if cfg.Tools.WebSearch.ExaAPIKey != "" {
			tWS.SetExaAPIKey(cfg.Tools.WebSearch.ExaAPIKey)
		}
		if err := reg.Register(tWS); err != nil {
			slog.Warn("websearch tool registration failed", "err", err)
		}
	}

	if cfg.Tools.WebFetch.Enabled {
		tWF := tools.NewWebFetchTool(cfg.Permission.Shell, nil)
		tWF.SetDefaultTimeout(cfg.Tools.Timeout)
		if err := reg.Register(tWF); err != nil {
			slog.Warn("webfetch tool registration failed", "err", err)
		}
	}

	// Register ledger tools when enabled.
	// ledger_get is read-only (diagnose mode).
	// ledger_set is operate-level (mutating).
	var ledgerCtx string
	if cfg.Ledger.Enabled {
		tLG := tools.NewLedgerGetTool(cfg.Permission.Shell)
		if err := reg.Register(tLG); err != nil {
			slog.Warn("ledger_get tool registration failed", "err", err)
		}
		tLS := tools.NewLedgerSetTool(cfg.Permission.Shell)
		if err := reg.Register(tLS); err != nil {
			slog.Warn("ledger_set tool registration failed", "err", err)
		}

		// Build ledger context for injection.
		// Open the ledger, format context, close.
		// Non-fatal if ledger is unavailable.
		if l, err := ledger.Open(); err == nil {
			ledgerCtx = ledger.FormatContext(
				l, cfg.Ledger.ContextBudget,
			)
			l.Close()
		} else {
			slog.Warn(
				"ledger unavailable, context injection skipped",
				"err", err,
			)
		}
	}

	return ledgerCtx
}

// Creates the audit store and logger, then wires the logger into
// every auditable tool in the registry. Returns nil when audit is
// disabled or the store is unavailable.
func initAuditLogger(
	cfg *config.Config,
	reg *tools.Registry,
) *audit.Logger {
	if !cfg.Audit.Enabled {
		return nil
	}

	auditStore, err := audit.NewAuditStore(cfg.Audit.DBPath)
	if err != nil {
		slog.Warn("audit store unavailable, continuing without audit",
			"err", err)
		return nil
	}

	auditLogger := audit.NewLogger(
		auditStore,
		cfg.Audit.AsyncBufferSize,
		500*time.Millisecond,
		cfg.Audit.OutputCaptureMaxBytes,
	)

	// Wire audit logger into every tool that accepts one.
	// Each tool is looked up by name and tested against
	// the Auditable interface; tools that implement it
	// receive the logger for recording command execution.
	for _, name := range []string{
		tools.ToolShell, tools.ToolSSH, tools.ToolSudo, tools.ToolAWS,
		tools.ToolWebSearch, tools.ToolWebFetch,
		tools.ToolLedgerGet, tools.ToolLedgerSet,
	} {
		if t, ok := reg.Lookup(name); ok {
			if a, ok := t.(tools.Auditable); ok {
				a.SetAuditLogger(auditLogger)
			}
		}
	}

	return auditLogger
}

// Creates the memory store and retriever based on the embedding
// provider configured. Probes the embedding endpoint and skips
// memory when unreachable. Non-fatal on all errors.
func initMemoryStore(
	ctx context.Context,
	cfg *config.Config,
) (memory.Store, *memory.Retriever) {
	if !cfg.Memory.Enabled {
		return nil, nil
	}

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
			cfg.Memory.Embedding.Dimensions,
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
		pCtx, pCancel := context.WithTimeout(ctx, 5*time.Second)
		defer pCancel()

		_, pErr := emb.Embed(pCtx, []string{"ping"})
		if pErr != nil {
			slog.Warn(
				"embedding endpoint unreachable, memory disabled",
				"err", pErr,
			)
			return nil, nil
		}

		return newMemStoreWithEmb(ctx, cfg, emb)
	}

	return newMemStoreWithoutEmb(cfg)
}

// Creates a memory store backed by an embedder and re-indexes
// existing SQLite entries into the vector store.
func newMemStoreWithEmb(
	ctx context.Context,
	cfg *config.Config,
	emb memory.Embedder,
) (memory.Store, *memory.Retriever) {
	memCfg := memory.Config{
		Enabled:     cfg.Memory.Enabled,
		DBPath:      cfg.Memory.DBPath,
		MaxEntries:  cfg.Memory.MaxEntries,
		AutoCapture: cfg.Memory.AutoCapture,
	}

	store, err := memory.NewStore(memCfg, emb)
	if err != nil {
		slog.Warn("memory store unavailable, continuing without memory",
			"err", err)
		return nil, nil
	}

	// Re-index existing SQLite entries.
	_ = memory.StoreMigrateVectors(ctx, store)

	return store, newRetriever(store, cfg)
}

// Creates a memory store without an embedder.
func newMemStoreWithoutEmb(
	cfg *config.Config,
) (memory.Store, *memory.Retriever) {
	memCfg := memory.Config{
		Enabled:     cfg.Memory.Enabled,
		DBPath:      cfg.Memory.DBPath,
		MaxEntries:  cfg.Memory.MaxEntries,
		AutoCapture: cfg.Memory.AutoCapture,
	}

	store, err := memory.NewStore(memCfg, nil)
	if err != nil {
		slog.Warn("memory store unavailable, continuing without memory",
			"err", err)
		return nil, nil
	}

	return store, newRetriever(store, cfg)
}

// Builds a memory retriever with vector search and budget
// settings from config.
func newRetriever(
	store memory.Store,
	cfg *config.Config,
) *memory.Retriever {
	retriever := memory.NewRetriever(store, 5)
	if cfg.Memory.ContextBudget > 0 {
		retriever.SetContextBudget(cfg.Memory.ContextBudget)
	}

	// Wire vector search into the retriever.
	vs, vEmb := memory.StoreVectorSearch(store)
	if vs != nil && vEmb != nil {
		retriever.SetVectorSearch(vs, vEmb)
	}

	return retriever
}

// Builds the context compressor and resolves model info for token
// estimation. Returns nil compressor when context compression is
// disabled.
func initCompressor(
	cfg *config.Config,
	memStore memory.Store,
	sumProvider llm.Provider,
) (*ctxcomp.Compressor, llm.ModelInfo) {
	var modelInfo llm.ModelInfo
	if !cfg.Context.Enabled {
		return nil, modelInfo
	}

	// Read model override for context window info.
	if mo, ok := cfg.Models[cfg.Model]; ok {
		modelInfo = llm.ModelInfo{
			ContextWindow:   mo.ContextWindow,
			MaxOutputTokens: mo.MaxOutputTokens,
		}
	}

	summaryFunc := buildSummaryFunc(cfg, sumProvider)

	compressor := ctxcomp.NewCompressor(
		ctxcomp.CompressorConfig{
			Enabled:               cfg.Context.Enabled,
			Mode:                  cfg.Context.Mode,
			Threshold:             cfg.Context.Threshold,
			MaxToolOutputTokens:   cfg.Context.MaxToolOutputTokens,
			MaxToolOutputLines:    cfg.Context.MaxToolOutputLines,
			OffloadToMemory:       cfg.Context.OffloadToMemory,
			MinMessagesToCompress: cfg.Context.MinMessagesToCompress,
			FallbackContextWindow: cfg.Context.FallbackContextWindow,
		},
		memStore,
		ctxcomp.NewEstimator(cfg.Model),
		summaryFunc,
	)

	return compressor, modelInfo
}

// Creates the LLM summarizer closure when a summary provider or
// model is configured. Returns nil for extractive-only
// summarization (current default). The provider parameter may be
// nil when the summarizer provider failed to initialise.
func buildSummaryFunc(
	cfg *config.Config,
	sumProvider llm.Provider,
) func(ctx context.Context, text string) (string, error) {
	if cfg.Context.SummaryProvider == "" && cfg.Context.SummaryModel == "" {
		return nil
	}

	// Nil provider means summarizer init failed; fall back
	// to extractive compression.
	if sumProvider == nil {
		return nil
	}

	sumModel := cfg.Context.SummaryModel
	if sumModel == "" {
		sumModel = cfg.Model
	}

	// Warn at startup when the summarizer falls back to the
	// main provider but that provider only serves local models
	// (ollama): a model that is not pulled locally fails at the
	// first compression and silently degrades to extractive.
	if cfg.Context.SummaryProvider == "" && cfg.Provider == "ollama" {
		slog.Warn(
			"summarizer will use the main provider " +
				"(ollama) with model " + sumModel + "; " +
				"the model must be pulled locally or " +
				"summarization falls back to extractive",
		)
	}

	return func(ctx context.Context, text string) (string, error) {
		// Bound the response via the summarizer model's max
		// output tokens; fall back to
		// max_tool_output_tokens when model info is unavailable.
		maxTokens := cfg.Context.MaxToolOutputTokens
		mi, miErr := sumProvider.ModelInfo(ctx, sumModel)
		if miErr == nil && mi.MaxOutputTokens > 0 {
			maxTokens = mi.MaxOutputTokens
		}
		resp, err := sumProvider.Chat(ctx, llm.ChatRequest{
			Model:     sumModel,
			MaxTokens: maxTokens,
			Messages: []llm.Message{
				{Role: "user", Content: text},
			},
		})
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}
}

// Creates a separate LLM provider for context summarization when
// summary_provider is configured. Returns nil on failure so the
// caller falls back to extractive compression.
func initSummarizerProvider(
	cfg *config.Config,
	mainProvider llm.Provider,
) llm.Provider {
	if cfg.Context.SummaryProvider == "" {
		return mainProvider
	}

	sumCfg := *cfg // shallow copy
	sumCfg.Provider = cfg.Context.SummaryProvider
	if cfg.Context.SummaryModel != "" {
		sumCfg.Model = cfg.Context.SummaryModel
	}

	sumProvider, err := llm.NewProvider(sumCfg)
	if err != nil {
		slog.Warn(
			"failed to init summarizer provider, "+
				"using extractive fallback",
			"err", err,
		)
		// Return nil so buildSummaryFunc skips LLM
		// summarization, matching the logged fallback.
		return nil
	}

	return sumProvider
}

// Dispatches to the TUI when a terminal is available and --no-tui
// is not set; otherwise falls back to the plain REPL.
func runTUIOrREPL(
	ctx context.Context,
	cfg *config.Config,
	provider llm.Provider,
	sess *session.Session,
	reg *tools.Registry,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
	sr scope.LoadResult,
	memStore memory.Store,
	memRetriever *memory.Retriever,
	toolRules map[string]*tools.RuleSet,
	overrider *agent.ConfigOverrider,
	orch *tools.TaskOrchestrator,
	statusGen *agent.StatusGenerator,
	ledgerCtx string,
	projectRoot string,
	subagentEventChan chan tools.SubagentEvent,
) error {
	if !noTuiFlag && agent.IsStdoutTerminal() {
		return runTUI(
			cfg, provider, sess, reg,
			compressor, modelInfo, sr,
			memStore, memRetriever, toolRules,
			overrider, orch,
			statusGen, ledgerCtx, projectRoot,
			subagentEventChan,
		)
	}

	return runREPL(
		ctx, cfg, provider, sess, reg,
		compressor, modelInfo, sr,
		memStore, memRetriever, toolRules,
		overrider, orch, statusGen, ledgerCtx,
		projectRoot,
	)
}

// Initializes and runs the Bubbletea TUI with all required
// configuration, logging, and subagent event wiring.
func runTUI(
	cfg *config.Config,
	provider llm.Provider,
	sess *session.Session,
	reg *tools.Registry,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
	sr scope.LoadResult,
	memStore memory.Store,
	memRetriever *memory.Retriever,
	toolRules map[string]*tools.RuleSet,
	overrider *agent.ConfigOverrider,
	orch *tools.TaskOrchestrator,
	statusGen *agent.StatusGenerator,
	ledgerCtx string,
	projectRoot string,
	subagentEventChan chan tools.SubagentEvent,
) error {
	if err := tuicl.Init(); err != nil {
		slog.Warn("clipboard unavailable, copy/paste disabled",
			"err", err)
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
		Scope:          sr.Content,
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
			PrimaryPath:  sr.PrimaryPath,
			Instructions: sr.Instructions,
			TotalBytes:   sr.TotalBytes,
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
		LedgerCtx:            ledgerCtx,
		ProjectRoot:          projectRoot,
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

// Starts the plain stdin/stdout REPL without TUI.
func runREPL(
	ctx context.Context,
	cfg *config.Config,
	provider llm.Provider,
	sess *session.Session,
	reg *tools.Registry,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
	sr scope.LoadResult,
	memStore memory.Store,
	memRetriever *memory.Retriever,
	toolRules map[string]*tools.RuleSet,
	overrider *agent.ConfigOverrider,
	orch *tools.TaskOrchestrator,
	statusGen *agent.StatusGenerator,
	ledgerCtx string,
	projectRoot string,
) error {
	repl := &agent.REPL{
		NoTUI:        noTuiFlag,
		Provider:     provider,
		Session:      sess,
		Mode:         cfg.Agent.Default,
		Model:        cfg.Model,
		Scope:        sr.Content,
		Override:     systemPrompt,
		In:           os.Stdin,
		Out:          os.Stdout,
		Registry:     reg,
		MaxToolIter:  cfg.Agent.MaxToolIterations,
		ShellEnabled: cfg.Tools.Shell.Enabled,
		ScopeInfo: agent.ScopeInfo{
			PrimaryPath:  sr.PrimaryPath,
			Instructions: sr.Instructions,
			TotalBytes:   sr.TotalBytes,
		},
		Store:           memStore,
		Retriever:       memRetriever,
		Compressor:      compressor,
		ModelInfo:       modelInfo,
		ToolRules:       toolRules,
		ConfigOverrider: overrider,
		Orchestrator:    orch,
		StatusGen:       statusGen,
		LedgerCtx:       ledgerCtx,
		ProjectRoot:     projectRoot,
	}

	return repl.Run(ctx)
}
