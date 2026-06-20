package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
)

// Holds scope metadata for the /scope command.
type ScopeInfo struct {
	PrimaryPath  string
	Instructions []string
	TotalBytes   int
}

// Holds state for the interactive chat loop.
type REPL struct {
	Provider     llm.Provider
	Session      *session.Session
	Mode         string
	Scope        string
	Model        string
	Override     string
	In           io.Reader
	Out          io.Writer
	Registry     *tools.Registry
	MaxToolIter  int
	ShellEnabled bool
	ScopeInfo    ScopeInfo
	Store        memory.Store
	Retriever    *memory.Retriever
	Compressor   *ctxcomp.Compressor
	ModelInfo    llm.ModelInfo
	ToolPermFunc ToolPermissionFunc
	ToolRules    map[string]*tools.RuleSet
	scanner      *bufio.Scanner

	// Streaming support for non-TUI mode.
	streamEnabled       bool
	thinkingDone        chan struct{}
	thinkingSpinnerDone chan struct{}
	toolDone            chan struct{}
	toolSpinnerDone     chan struct{}
}

// Starts the interactive REPL loop reading from In and writing to Out.
// Runs until /quit, ctx cancellation, or EOF.
func (r *REPL) Run(ctx context.Context) error {
	r.streamEnabled = stdoutIsTerminal.Load()
	fmt.Fprint(r.Out, Prompt())

	r.scanner = bufio.NewScanner(r.In)

	for r.scanner.Scan() {
		// Check for context cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}

		line := strings.TrimSpace(r.scanner.Text())

		// Check for empty input.
		if line == "" {
			fmt.Fprint(r.Out, Prompt())

			continue
		}

		// Check for slash commands.
		if cmd, done, err := r.handleCommand(line); done {
			return nil
		} else if err != nil {
			fmt.Fprintf(r.Out, "Error: %v\n", err)
			fmt.Fprint(r.Out, Prompt())

			continue
		} else if cmd {
			fmt.Fprint(r.Out, Prompt())

			continue
		}

		// Normal chat turn.
		var reply string
		var err error

		// Start thinking spinner for all paths.
		if r.streamEnabled {
			r.thinkingDone = make(chan struct{})
			r.thinkingSpinnerDone = make(chan struct{})
			td := r.thinkingDone
			tsd := r.thinkingSpinnerDone
			go func(done chan struct{}, sd chan struct{}) {
				defer close(sd)
				ticker := time.NewTicker(100 * time.Millisecond)
				start := time.Now()
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						fmt.Fprint(r.Out, ThinkingLine(time.Since(start)))
					case <-done:
						fmt.Fprint(r.Out, ClearLine())
						return
					}
				}
			}(td, tsd)
		}

		if r.Registry != nil {
			// Build onEvent closure for tool visibility.
			onEvent := func(ev AgentEvent) {
				switch ev.Type {
				case "tool-start":
					if r.thinkingDone != nil {
						close(r.thinkingDone)
						r.thinkingDone = nil
					}
					fmt.Fprintln(r.Out)
					fmt.Fprintln(r.Out, ToolStart(ev.Name, ev.Info))

					if r.streamEnabled {
						// Start tool spinner.
						r.toolDone = make(chan struct{})
						r.toolSpinnerDone = make(chan struct{})
						td := r.toolDone
						tsd := r.toolSpinnerDone
						go func(done chan struct{}, sd chan struct{}) {
							defer close(sd)
							ticker := time.NewTicker(100 * time.Millisecond)
							start := time.Now()
							defer ticker.Stop()
							for {
								select {
								case <-ticker.C:
									fmt.Fprint(r.Out, RunningLine(time.Since(start)))
								case <-done:
									fmt.Fprint(r.Out, ClearLine())
									return
								}
							}
						}(td, tsd)
					}

				case "tool-end":
					if r.toolDone != nil {
						close(r.toolDone)
						r.toolDone = nil
					}
					fmt.Fprintln(r.Out, ToolEnd(ev.Name, ev.Info, ev.Output))
					fmt.Fprintln(r.Out)
				}
			}

			permFunc := r.ToolPermFunc
			if permFunc == nil && r.ToolRules != nil {
				permFunc = r.toolPermissionFunc
			}

			if r.streamEnabled {
				// Streaming path with progressive text output.
				onDelta := func(delta string) {
					// Kill thinking spinner on first delta.
					if r.thinkingDone != nil {
						close(r.thinkingDone)
						r.thinkingDone = nil
					}
					fmt.Fprint(r.Out, delta)
				}
				reply, err = RunTurnWithToolsStream(
					ctx, r.Provider, r.Session,
					r.Mode, r.Scope, r.Override, r.Model, line,
					r.Registry, r.MaxToolIter, r.ShellEnabled,
					r.Store, r.Retriever,
					r.Compressor, r.ModelInfo,
					onEvent, onDelta, permFunc, r.ToolRules,
				)

				// Fall back to non-streaming when provider doesn't
				// support it (e.g. opencode_zen).
				if err != nil && strings.Contains(
					err.Error(), "streaming not",
				) {
					reply, err = RunTurnWithTools(
						ctx, r.Provider, r.Session,
						r.Mode, r.Scope, r.Override,
						r.Model, line,
						r.Registry, r.MaxToolIter,
						r.ShellEnabled,
						r.Store, r.Retriever,
						r.Compressor, r.ModelInfo,
						onEvent, permFunc, r.ToolRules,
					)
				}
			} else {
				// Non-streaming fallback (piped / CI).
				reply, err = RunTurnWithTools(
					ctx, r.Provider, r.Session,
					r.Mode, r.Scope, r.Override, r.Model, line,
					r.Registry, r.MaxToolIter, r.ShellEnabled,
					r.Store, r.Retriever,
					r.Compressor, r.ModelInfo,
					onEvent, permFunc, r.ToolRules,
				)
			}
		} else {
			// No tools path.
			reply, err = RunTurn(
				ctx, r.Provider, r.Session,
				r.Mode, r.Scope, r.Override, r.Model, line,
				r.Store, r.Retriever,
				r.Compressor, r.ModelInfo,
			)
		}

		// Ensure spinner is killed after any path completes.
		if r.thinkingDone != nil {
			close(r.thinkingDone)
			r.thinkingDone = nil
		}

		if err != nil {
			fmt.Fprintf(r.Out, "\n%s\n", colorize(ansiRed, "Error: "+err.Error()))
			fmt.Fprint(r.Out, Prompt())

			continue
		}

		// Render separator + markdown reply + footer separator.
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, Separator("agent"))
		fmt.Fprintln(r.Out, FormatMarkdown(reply))
		fmt.Fprintln(r.Out, Separator(""))

		// Show memory retrieval indicator when memory was used.
		if r.Retriever != nil && r.Retriever.Stats().LastCount > 0 {
			fmt.Fprintln(r.Out, MemoryIndicator(r.Retriever.Stats().LastCount))
		}

		fmt.Fprint(r.Out, Prompt())
	}

	if err := r.scanner.Err(); err != nil {
		return fmt.Errorf("scanner: %w", err)
	}

	return nil
}

// Handles slash commands. Returns (handled, shouldQuit, error).
func (r *REPL) handleCommand(line string) (bool, bool, error) {
	if !strings.HasPrefix(line, "/") {

		return false, false, nil
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {

		return false, false, nil
	}

	switch parts[0] {
	case "/quit":

		return true, true, nil

	case "/reset":
		r.Session.Reset()
		fmt.Fprintln(r.Out, "Session reset.")

		return true, false, nil

	case "/model":
		fmt.Fprintf(r.Out, "%s (%s)\n", r.Provider.Name(), r.Model)

		return true, false, nil

	case "/agent":
		if len(parts) == 2 {
			switch parts[1] {
			case "diagnose":
				r.Mode = "diagnose"
				fmt.Fprintln(r.Out, "Switched to diagnose mode.")

				return true, false, nil

			case "operate":
				r.Mode = "operate"
				fmt.Fprintln(r.Out, "Switched to operate mode.")

				return true, false, nil

			default:
				return true, false, fmt.Errorf("unknown agent mode: %s", parts[1])
			}
		}
		fmt.Fprintln(r.Out, r.Mode)

		return true, false, nil

	case "/scope":
		fmt.Fprintf(r.Out, "scope: %d bytes\n", r.ScopeInfo.TotalBytes)
		if r.ScopeInfo.PrimaryPath != "" {
			fmt.Fprintf(r.Out, "  primary: %s\n", r.ScopeInfo.PrimaryPath)
		}
		for _, inst := range r.ScopeInfo.Instructions {
			fmt.Fprintf(r.Out, "  instruction: %s\n", inst)
		}

		return true, false, nil

	case "/help":
		fmt.Fprint(r.Out, `Shmorby help

AGENT MODES
  tab / shift+tab    Cycle agent modes
  operate            Full tool access (default)
  diagnose           Read-only inspection

SLASH COMMANDS
  /help              Show this help
  /quit              Exit shmorby
  /reset             Clear conversation history
  /model <name>      Switch LLM model
  /agent <mode>      Switch agent mode
  /scope             Show loaded scope context
  /memory            Memory management
  /context           Token usage and compression stats
  /log <level>       Set log verbosity
  /tui               Toggle fullscreen mode

KEYBOARD SHORTCUTS
  ctrl+h             Show help
  ctrl+p             Command palette
  ctrl+r             Reverse-i-search input history
  ctrl+c             Quit shmorby
  ctrl+v             Paste from clipboard
  ctrl+l             Toggle log section
  ctrl+t             Toggle thinking block
  ctrl+x             Leader key
  tab / shift+tab    Cycle agent modes (empty input)
  pgup / pgdn        Scroll output by page
  up / down          Scroll output by line
  home / end         Top / bottom of output

LEADER KEY (ctrl+x)
  ctrl+x c           Compact session
  ctrl+x n           New session
  ctrl+x l           Session list
  ctrl+x m           Model list / switch
  ctrl+x t           Theme list / switch
  ctrl+x a           Agent list / switch
  ctrl+x u           Undo last message
  ctrl+x r           Redo
  ctrl+x e           Open external editor
  ctrl+x x           Export session
  ctrl+x q           Quit
  ctrl+x s           Status view
  ctrl+x h           Tips / help
  ctrl+x b           Toggle sidebar
  ctrl+x y           Copy selected text

PERMISSIONS
  shell              allow
  ssh                allow
  sudo               ask (default disabled)
  aws                ask (default disabled)

Current mode: `+r.Mode+`
`)

		return true, false, nil

	case "/memory":
		return r.handleMemoryCommand(parts)

	case "/context":
		return r.handleContextCommand(parts)

	case "/log":
		return r.handleLogCommand(parts)

	default:
		// Unknown slash command.
		return true, false, fmt.Errorf("unknown command: %s", parts[0])
	}
}

// Handles /memory slash command and its subcommands.
func (r *REPL) handleMemoryCommand(parts []string) (bool, bool, error) {
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "":
		// Show recent entries.
		if r.Store == nil {
			fmt.Fprintln(r.Out, "Memory store not available.")

			return true, false, nil
		}
		entries, err := r.Store.List(20, 0)
		if err != nil {
			return true, false, fmt.Errorf("list memory: %w", err)
		}
		if len(entries) == 0 {
			fmt.Fprintln(r.Out, "No memory entries.")

			return true, false, nil
		}
		for _, e := range entries {
			fmt.Fprintf(r.Out, "[%s] %s: %s\n",
				e.Timestamp.Format("2006-01-02 15:04"),
				e.Tool, e.Command,
			)
		}
		fmt.Fprintf(r.Out, "(%d entries)\n", len(entries))

		return true, false, nil

	case "search":
		if len(parts) < 3 {
			return true, false, fmt.Errorf("usage: /memory search <query>")
		}
		if r.Retriever == nil {
			fmt.Fprintln(r.Out, "Memory search not available.")

			return true, false, nil
		}
		query := strings.Join(parts[2:], " ")
		result, err := r.Retriever.Retrieve(context.Background(), query)
		if err != nil {
			return true, false, fmt.Errorf("search memory: %w", err)
		}
		entries := result.Entries
		if len(entries) == 0 {
			fmt.Fprintln(r.Out, "No matching entries.")

			return true, false, nil
		}
		for _, e := range entries {
			fmt.Fprintf(r.Out, "[%s] %s: %s\n",
				e.Timestamp.Format("2006-01-02 15:04"),
				e.Tool, e.Command,
			)
		}
		fmt.Fprintf(r.Out, "(%d results)\n", len(entries))

		return true, false, nil

	case "forget":
		if len(parts) < 3 {
			return true, false, fmt.Errorf("usage: /memory forget <id>")
		}
		if r.Store == nil {
			fmt.Fprintln(r.Out, "Memory store not available.")

			return true, false, nil
		}
		id := parts[2]
		if err := r.Store.Delete(id); err != nil {
			return true, false, fmt.Errorf("delete memory: %w", err)
		}
		fmt.Fprintf(r.Out, "Deleted entry %s.\n", id)

		return true, false, nil

	case "clear":
		if r.Store == nil {
			fmt.Fprintln(r.Out, "Memory store not available.")

			return true, false, nil
		}
		// List all entries to get IDs for deletion.
		entries, err := r.Store.List(100000, 0)
		if err != nil {
			return true, false, fmt.Errorf("list memory: %w", err)
		}
		if len(entries) == 0 {
			fmt.Fprintln(r.Out, "No entries to clear.")

			return true, false, nil
		}
		for _, e := range entries {
			_ = r.Store.Delete(e.ID)
		}
		fmt.Fprintf(r.Out, "Cleared %d entries.\n", len(entries))

		return true, false, nil

	case "stats":
		if r.Store == nil {
			fmt.Fprintln(r.Out, "Memory store not available.")

			return true, false, nil
		}
		count, err := r.Store.Count()
		if err != nil {
			return true, false, fmt.Errorf("count memory: %w", err)
		}
		fmt.Fprintf(r.Out, "Entries: %d\n", count)

		// Show retrieval stats if retriever is available.
		if r.Retriever != nil {
			stats := r.Retriever.Stats()
			total := stats.Hits + stats.Misses
			hitRate := 0
			if total > 0 {
				hitRate = stats.Hits * 100 / total
			}
			fmt.Fprintf(r.Out, "Retrievals: %d hits, %d misses (%d%% hit rate)\n",
				stats.Hits, stats.Misses, hitRate)
		}

		return true, false, nil

	default:
		return true, false, fmt.Errorf("unknown /memory subcommand: %s", sub)
	}
}

// handleContextCommand handles the /context slash command and subcommands.
func (r *REPL) handleContextCommand(parts []string) (bool, bool, error) {
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "":
		if r.Compressor == nil {
			fmt.Fprintln(r.Out, "Context compression not enabled.")
			return true, false, nil
		}
		cfg := r.Compressor.Config()
		cw := r.ModelInfo.ContextWindow
		if cw == 0 {
			cw = cfg.FallbackContextWindow
		}
		messages := r.Session.Messages()
		estimated := r.Compressor.EstimateMessages(messages)
		fmt.Fprintf(r.Out, "Context status:\n")
		fmt.Fprintf(r.Out, "  Model: %s\n", r.Model)
		fmt.Fprintf(r.Out, "  Context window: %d tokens\n", cw)
		fmt.Fprintf(r.Out, "  Estimated tokens: %d\n", estimated)
		fmt.Fprintf(r.Out, "  Compression threshold: %.0f%%\n", cfg.Threshold*100)
		fmt.Fprintf(r.Out, "  Compressions this session: %d\n", r.Compressor.CompressionCount)
		fmt.Fprintf(r.Out, "  Mode: %s\n", cfg.Mode)
		return true, false, nil

	case "compress":
		if r.Compressor == nil {
			fmt.Fprintln(r.Out, "Context compression not enabled.")
			return true, false, nil
		}
		fmt.Fprintln(r.Out, "Compression triggered.")
		return true, false, nil

	case "stats":
		if r.Compressor == nil {
			fmt.Fprintln(r.Out, "Context compression not enabled.")
			return true, false, nil
		}
		offloaded := r.Compressor.OffloadCount
		fmt.Fprintf(r.Out, "Offloaded messages: %d\n", offloaded)
		return true, false, nil

	case "model":
		fmt.Fprintf(r.Out, "Model: %s\n", r.Model)
		if r.ModelInfo.ContextWindow > 0 {
			fmt.Fprintf(r.Out, "  Context window: %d (API-verified)\n", r.ModelInfo.ContextWindow)
		}
		if r.ModelInfo.MaxOutputTokens > 0 {
			fmt.Fprintf(r.Out, "  Max output: %d\n", r.ModelInfo.MaxOutputTokens)
		}
		if r.ModelInfo.SupportsTools {
			fmt.Fprintf(r.Out, "  Tool calling: supported\n")
		}
		return true, false, nil

	default:
		return true, false, fmt.Errorf("unknown /context subcommand: %s", sub)
	}
}

// handleLogCommand handles the /log slash command.
func (r *REPL) handleLogCommand(parts []string) (bool, bool, error) {
	// The REPL doesn't track a log level; just acknowledge the command.
	if len(parts) == 2 {
		switch parts[1] {
		case "debug", "info", "warn", "error":
			fmt.Fprintf(r.Out, "Log level: %s\n", parts[1])
			return true, false, nil
		default:
			return true, false, fmt.Errorf(
				"unknown log level: %s (want debug|info|warn|error)",
				parts[1],
			)
		}
	}
	fmt.Fprintln(r.Out, "Log level: info")
	return true, false, nil
}

// toolPermissionFunc implements the permission callback for the REPL,
// prompting the user via a fresh scanner on In.
func (r *REPL) toolPermissionFunc(toolName, command, reason string) ToolPermissionResponse {
	// Suspend thinking spinner while prompting.
	if r.thinkingDone != nil {
		close(r.thinkingDone)
		r.thinkingDone = nil
		<-r.thinkingSpinnerDone
	}
	// Suspend tool-running spinner while prompting.
	if r.toolDone != nil {
		close(r.toolDone)
		r.toolDone = nil
		<-r.toolSpinnerDone
	}

	fmt.Fprintf(r.Out, "\nPermission requested: %s\n", toolName)
	fmt.Fprintf(r.Out, "  command: %s\n", command)
	if reason != "" {
		fmt.Fprintf(r.Out, "  reason:  %s\n", reason)
	}
	fmt.Fprint(r.Out, "Allow? [y]es / [n]o / [a]llow all like this: ")

	// Use a fresh scanner to avoid racing with the main loop's scanner.
	s := bufio.NewScanner(r.In)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		switch strings.ToLower(line) {
		case "y", "yes":
			return PermAllow
		case "n", "no":
			return PermDeny
		case "a", "all":
			return PermAllowAll
		default:
			fmt.Fprint(r.Out, "y/n/a: ")
		}
	}
	return PermDeny
}
