package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"

	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	"shmorby/internal/tui/history"
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
	history      *history.History
	oldState     *term.State
	inRaw        bool

	// Suspension state for SIGTSTP/SIGCONT handling.
	// Track whether we were suspended so readLine can
	// handle the interrupted stdin read gracefully.
	suspended      atomic.Bool
	savedOld       atomic.Value  // stores *term.State (atomic, race-free)
	jobControlDone chan struct{} // closed in Run's defer to stop handler

	// Streaming support for non-TUI mode.
	streamEnabled       bool
	thinkingDone        chan struct{}
	thinkingSpinnerDone chan struct{}
	toolDone            chan struct{}
	toolSpinnerDone     chan struct{}

	// NoTUI disables raw mode, spinners, and ANSI formatting.
	NoTUI bool

	// Runtime config overrides.
	ConfigOverrider *ConfigOverrider

	// Subagent orchestrator for permission propagation.
	Orchestrator *tools.TaskOrchestrator

	// Async status description generator.
	StatusGen *StatusGenerator

	// Pre-formatted ledger context for injection.
	// Empty string disables ledger context injection.
	LedgerCtx string

	// ProjectRoot is the resolved project directory for the
	// environment hint in the system prompt (empty = default).
	ProjectRoot string
}

// Starts the interactive REPL loop reading from In and writing to Out.
// Runs until /quit, ctx cancellation, or EOF.
func (r *REPL) Run(ctx context.Context) error {
	r.streamEnabled = !r.NoTUI && stdoutIsTerminal.Load()

	fmt.Fprint(r.Out, Prompt())
	r.history = history.New(1000)

	// Only enter raw mode for the interactive TUI-like REPL.
	if !r.NoTUI {
		fi, err := os.Stdin.Stat()
		if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			oldState, termErr := term.MakeRaw(int(os.Stdin.Fd()))
			if termErr == nil {
				r.oldState = oldState
				r.savedOld.Store(oldState)
				r.inRaw = true
			}
		}
	}

	// Set up SIGTSTP/SIGCONT handling for proper job control.
	// Without this, suspending (Ctrl+Z) leaves the terminal in raw
	// mode and resuming (fg) corrupts the display.
	if r.inRaw {
		r.setupJobControl()
	}

	defer func() {
		// Tear down job control handler before restoring the
		// terminal so the signal goroutine cannot race with
		// the restore below.
		if r.jobControlDone != nil {
			close(r.jobControlDone)
		}
		r.killSpinners()
		if r.inRaw {
			// Load from savedOld (atomic) — the signal handler
			// updates savedOld after each resume, so this is
			// always the most recent cooked-mode state.
			if saved, ok := r.savedOld.Load().(*term.State); ok && saved != nil {
				term.Restore(int(os.Stdin.Fd()), saved)
			}
		}
		if r.streamEnabled {
			fmt.Fprint(r.Out, ansiReset)
		}
	}()

	if !r.inRaw {
		r.scanner = bufio.NewScanner(r.In)
	}

	for {
		if err := ctx.Err(); err != nil {

			return err
		}

		line, err := r.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(r.Out)

				return nil
			}

			return err
		}

		line = strings.TrimSpace(line)

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

		if !r.streamEnabled {
			// Plain output: no spinners, no streaming, no ANSI.
			reply, err = r.runPlainTurn(ctx, line)
		} else {
			reply, err = r.runStreamTurn(ctx, line)
		}

		if err != nil {
			fmt.Fprintf(r.Out, "Error: %v\n", err)
			fmt.Fprint(r.Out, Prompt())

			continue
		}

		if r.NoTUI {
			fmt.Fprintln(r.Out, reply)
		} else {
			// Render separator + markdown reply + footer separator.
			fmt.Fprintln(r.Out)
			fmt.Fprintln(r.Out, Separator("agent"))
			fmt.Fprintln(r.Out, FormatMarkdown(reply))
			fmt.Fprintln(r.Out, Separator(""))

			// Show memory retrieval indicator when memory was used.
			if r.Retriever != nil && r.Retriever.Stats().LastCount > 0 {
				fmt.Fprintln(r.Out, MemoryIndicator(r.Retriever.Stats().LastCount))
			}
		}

		fmt.Fprint(r.Out, Prompt())
	}
}

// killSpinners stops all running spinner goroutines and waits for them
// to drain so no ANSI sequences are written after terminal restore.
func (r *REPL) killSpinners() {
	if r.thinkingDone != nil {
		close(r.thinkingDone)
		r.thinkingDone = nil
	}
	if r.thinkingSpinnerDone != nil {
		<-r.thinkingSpinnerDone
		r.thinkingSpinnerDone = nil
	}
	if r.toolDone != nil {
		close(r.toolDone)
		r.toolDone = nil
	}
	if r.toolSpinnerDone != nil {
		<-r.toolSpinnerDone
		r.toolSpinnerDone = nil
	}
	if r.streamEnabled {
		fmt.Fprint(r.Out, ClearLine())
	}
}

// setupJobControl installs SIGTSTP/SIGCONT handlers to properly
// support Ctrl+Z suspend/resume when the terminal is in raw mode.
//
// On SIGTSTP: restores the terminal to cooked mode so the shell can
// display its prompt, then re-raises SIGTSTP to actually stop.
//
// On SIGCONT: re-enters raw mode, redraws the prompt, and sets a
// flag so readLine knows the stdin read was interrupted by a stop.
//
// Teardown: closing jobControlDone stops the signal handler and
// unregisters from the signal package.
func (r *REPL) setupJobControl() {
	if runtime.GOOS == "windows" {
		return
	}

	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	r.jobControlDone = done

	// Guard: only store if oldState is non-nil to avoid a typed-nil
	// that would pass the type assertion but panic term.Restore.
	if r.oldState != nil {
		r.savedOld.Store(r.oldState)
	}

	signal.Notify(sigCh, sigTSTP, sigCONT)

	go func() {
		for {
			select {
			case <-done:
				signal.Stop(sigCh)
				return
			case sig := <-sigCh:
				if sig != sigTSTP {
					continue
				}
				// Restore terminal to cooked mode before the
				// kernel suspends the process. Without this
				// the tty stays in raw mode and the shell's
				// "suspended" message is garbled.
				if r.inRaw {
					if saved, ok := r.savedOld.Load().(*term.State); ok && saved != nil {
						term.Restore(int(os.Stdin.Fd()), saved)
					}
				}
				r.suspended.Store(true)

				// Re-raise SIGTSTP with default handler so the
				// process actually stops. This call does not
				// return until SIGCONT arrives.
				signal.Stop(sigCh)
				p, _ := os.FindProcess(os.Getpid())
				if err := p.Signal(sigTSTP); err != nil {
					// Terminal already restored; log and
					// leave suspended=true so the REPL can
					// detect the broken state.
					fmt.Fprintf(r.Out,
						"warning: re-raise SIGTSTP failed: %v\n",
						err,
					)
				}

				// --- process resumes here after SIGCONT ---

				// Re-enter raw mode now that we're running again.
				if r.inRaw {
					oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
					if err == nil {
						// Only write savedOld — never
						// oldState. The defer in Run
						// loads from savedOld, so this
						// is the single source of truth
						// and avoids a data race.
						r.savedOld.Store(oldState)
					}
				}

				// Clear the interrupted-read flag and redraw the
				// prompt so the user sees a clean input line.
				r.suspended.Store(false)
				fmt.Fprint(r.Out, Prompt())

				// Re-register for both signals — signal.Stop
				// above removed all registrations, so both
				// must be re-added for the next cycle.
				signal.Notify(sigCh, sigTSTP, sigCONT)
			}
		}
	}()
}

// runPlainTurn executes a turn with plain text output — no spinners,
// no streaming, no ANSI. Used in --no-tui mode.
// In terminal mode a dot progress indicator replaces the spinner.
func (r *REPL) runPlainTurn(ctx context.Context, line string) (string, error) {
	var onEvent func(AgentEvent)
	dotDone := make(chan struct{})
	defer func() {
		select {
		case <-dotDone:
		default:
			close(dotDone)
		}
	}()

	if stdoutIsTerminal.Load() {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					fmt.Fprint(r.Out, ".")
				case <-dotDone:
					return
				}
			}
		}()

		onEvent = func(ev AgentEvent) {
			switch ev.Type {
			case "tool-start":
				select {
				case <-dotDone:
				default:
					close(dotDone)
				}
			case "tool-end":
				if ev.Output != "" {
					fmt.Fprintln(r.Out)
					fmt.Fprintln(r.Out, ev.Output)
				}
			}
		}
	} else {
		onEvent = func(ev AgentEvent) {
			switch ev.Type {
			case "tool-start":
				fmt.Fprintf(r.Out, "[running: %s %s]\n", ev.Name, ev.Info)
			case "tool-end":
				fmt.Fprintf(r.Out, "[done: %s]\n", ev.Name)
				if ev.Output != "" {
					fmt.Fprintln(r.Out, ev.Output)
				}
			}
		}
	}

	permFunc := r.ToolPermFunc
	if permFunc == nil && r.ToolRules != nil {
		permFunc = r.toolPermissionFunc
	}

	// Propagate permission callback to subagent orchestrator so
	// subagents respect the same permission constraints.
	if r.Orchestrator != nil {
		r.Orchestrator.PermFunc = permFunc
	}

	var reply string
	var err error
	if r.Registry != nil {
		reply, err = RunTurnWithTools(
			ctx, r.Provider, r.Session,
			r.Mode, r.Scope, r.Override, r.Model, line,
			r.Registry, r.MaxToolIter, r.ShellEnabled,
			r.Store, r.Retriever,
			r.Compressor, r.ModelInfo,
			onEvent, permFunc, r.ToolRules,
			r.StatusGen, r.LedgerCtx,
			r.ProjectRoot,
		)
	} else {
		reply, err = RunTurn(
			ctx, r.Provider, r.Session,
			r.Mode, r.Scope, r.Override, r.Model, line,
			r.Store, r.Retriever,
			r.Compressor, r.ModelInfo,
			r.LedgerCtx,
			r.ProjectRoot,
		)
	}

	if stdoutIsTerminal.Load() {
		select {
		case <-dotDone:
		default:
			close(dotDone)
		}
		fmt.Fprintln(r.Out)
	}

	return reply, err
}

// runStreamTurn executes a turn with streaming output, spinners,
// and ANSI formatting. Used in interactive terminal mode.
func (r *REPL) runStreamTurn(
	ctx context.Context, line string,
) (string, error) {
	var reply string
	var err error

	// Start thinking spinner.
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

	onEvent := func(ev AgentEvent) {
		switch ev.Type {
		case "tool-start":
			if r.thinkingDone != nil {
				close(r.thinkingDone)
				r.thinkingDone = nil
			}
			fmt.Fprintln(r.Out)
			fmt.Fprintln(r.Out, ToolStart(ev.Name, ev.Info))

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

		case "tool-status":
			// Inference-generated description arrived; no-op for
			// REPL — it shows the tool header, not a spinner
			// with arbitrary text. Logged for future use.

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

	if r.Registry != nil {
		onDelta := func(delta string) {
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
			r.StatusGen, r.LedgerCtx,
			r.ProjectRoot,
		)

		if err != nil && strings.Contains(err.Error(), "streaming not") {
			reply, err = RunTurnWithTools(
				ctx, r.Provider, r.Session,
				r.Mode, r.Scope, r.Override, r.Model, line,
				r.Registry, r.MaxToolIter, r.ShellEnabled,
				r.Store, r.Retriever,
				r.Compressor, r.ModelInfo,
				onEvent, permFunc, r.ToolRules,
				r.StatusGen, r.LedgerCtx,
				r.ProjectRoot,
			)
		}
	} else {
		reply, err = RunTurn(
			ctx, r.Provider, r.Session,
			r.Mode, r.Scope, r.Override, r.Model, line,
			r.Store, r.Retriever,
			r.Compressor, r.ModelInfo,
			r.LedgerCtx,
			r.ProjectRoot,
		)
	}

	// Ensure spinners are killed and drained.
	if r.thinkingDone != nil {
		close(r.thinkingDone)
		r.thinkingDone = nil
	}
	if r.thinkingSpinnerDone != nil {
		<-r.thinkingSpinnerDone
		r.thinkingSpinnerDone = nil
	}
	if r.toolDone != nil {
		close(r.toolDone)
		r.toolDone = nil
	}
	if r.toolSpinnerDone != nil {
		<-r.toolSpinnerDone
		r.toolSpinnerDone = nil
	}

	return reply, err
}

// maxLineBytes bounds the raw-mode input line so a multi-megabyte
// paste cannot grow the line buffer without limit. Non-raw
// mode is already bounded by bufio.Scanner's 64 KiB token limit.
const maxLineBytes = 1 << 20

// readLine reads one line of input. In raw mode (terminal) it reads
// character-by-character with history navigation via up/down arrows.
// In non-raw mode (piped stdin) it falls back to bufio.Scanner.
func (r *REPL) readLine() (string, error) {
	if !r.inRaw {
		if r.scanner.Scan() {

			return r.scanner.Text(), nil
		}

		if r.scanner.Err() != nil {
			return "", r.scanner.Err()
		}

		return "", io.EOF
	}

	line := make([]byte, 0, 256)
	buf := make([]byte, 1)

	// Once the cap is hit the remainder of the line is consumed and
	// discarded until the newline, so the error cannot truncate an
	// otherwise valid next command.
	tooLong := false

	// Bound EINTR retries to avoid an infinite loop if the OS
	// keeps delivering signals. 10 retries covers the normal
	// suspend/resume case with ample margin.
	eintrRetries := 0
	const maxEintrRetries = 10

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			// EINTR means a signal interrupted the read (e.g.
			// SIGTSTP). The job-control handler restores the
			// terminal; retry the read. We don't gate on the
			// suspended flag because there's a race: the handler
			// may clear it before readLine checks, which would
			// otherwise kill the REPL.
			if errors.Is(err, syscall.EINTR) && eintrRetries < maxEintrRetries {
				eintrRetries++
				continue
			}

			return "", err
		}
		eintrRetries = 0 // reset on successful read
		if n == 0 {
			continue
		}

		c := buf[0]
		switch {
		case c == '\r' || c == '\n':
			fmt.Fprint(r.Out, "\r\n")
			if tooLong {
				fmt.Fprintf(r.Out, "error: input too long "+
					"(limit %d bytes); line discarded\r\n",
					maxLineBytes)
				line = line[:0]
				tooLong = false
				continue
			}
			result := string(line)
			if result != "" {
				r.history.Add(result)
			}

			return result, nil

		case c == '\x7f' || c == '\b':
			if len(line) > 0 {
				line = line[:len(line)-1]
				fmt.Fprint(r.Out, "\b \b")
			}

		case c == '\x03':

			return "", fmt.Errorf("interrupted")

		case c == '\x04':
			if len(line) == 0 {

				return "", io.EOF
			}

		case c == '\x1b':
			seq := make([]byte, 1)
			if _, err := os.Stdin.Read(seq); err != nil {
				continue
			}
			if seq[0] != '[' {
				continue
			}
			cmd := make([]byte, 1)
			if _, err := os.Stdin.Read(cmd); err != nil {
				continue
			}
			switch cmd[0] {
			case 'A':
				if entry, ok := r.history.Older(); ok {
					for range line {
						fmt.Fprint(r.Out, "\b \b")
					}
					line = append(line[:0], entry...)
					fmt.Fprint(r.Out, entry)
				}
			case 'B':
				if entry, ok := r.history.Newer(); ok {
					for range line {
						fmt.Fprint(r.Out, "\b \b")
					}
					line = append(line[:0], entry...)
					fmt.Fprint(r.Out, entry)
				}
			}

		case c == '\t':
			if !tooLong && len(line) < maxLineBytes {
				line = append(line, c)
				fmt.Fprint(r.Out, "\t")
			} else {
				tooLong = true
			}

		default:
			if c >= 32 {
				// Stop appending (and echoing) at the cap so
				// a huge paste cannot grow the buffer or the
				// scrollback.
				if tooLong || len(line) >= maxLineBytes {
					tooLong = true
					continue
				}
				line = append(line, c)
				fmt.Fprint(r.Out, string(c))
			}
		}
	}
}

// Prompts for and reads one secret line. In raw (terminal) mode the
// echo is suppressed via term.ReadPassword, which briefly re-enables
// canonical mode and restores the raw state on return. In piped mode
// stdin is not a terminal — there is nothing to mask — so the line
// comes from the scanner instead.
func (r *REPL) readSecret(prompt string) (string, error) {
	fmt.Fprint(r.Out, prompt)

	if !r.inRaw {
		if r.scanner == nil || !r.scanner.Scan() {
			return "", fmt.Errorf("no input")
		}

		return strings.TrimSpace(r.scanner.Text()), nil
	}

	keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprint(r.Out, "\r\n")
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}

	return strings.TrimSpace(string(keyBytes)), nil
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
		fmt.Fprint(r.Out, ClearScreen())
		fmt.Fprintln(r.Out, "Session reset.")

		return true, false, nil

	case "/model":
		if len(parts) >= 2 {
			if r.ConfigOverrider == nil {
				fmt.Fprintln(
					r.Out, "Config override not available.",
				)
				return true, false, nil
			}
			msg, err := r.ConfigOverrider.Set("model", parts[1])
			if err != nil {
				fmt.Fprintf(r.Out, "error: %v\n", err)
				return true, false, nil
			}
			r.Model = parts[1]
			fmt.Fprintln(r.Out, msg)
			return true, false, nil
		}
		fmt.Fprintf(r.Out, "%s (%s)\n", r.Provider.Name(), r.Model)

		return true, false, nil

	case "/platform":
		if len(parts) >= 2 {
			if r.ConfigOverrider == nil {
				fmt.Fprintln(
					r.Out, "Config override not available.",
				)
				return true, false, nil
			}
			msg, err := r.ConfigOverrider.Set(
				"provider", parts[1],
			)
			if err != nil {
				fmt.Fprintf(r.Out, "error: %v\n", err)
				return true, false, nil
			}
			if newProv := r.ConfigOverrider.Provider(); newProv != nil {
				r.Provider = newProv
			}
			fmt.Fprintln(r.Out, msg)
			return true, false, nil
		}
		fmt.Fprintf(r.Out, "%s\n", r.Provider.Name())

		return true, false, nil

	case "/apikey":
		if r.ConfigOverrider == nil {
			fmt.Fprintln(r.Out, "Config override not available.")

			return true, false, nil
		}
		// The old inline form echoed the key onto the screen and
		// into terminal scrollback; it is ignored now and the key
		// always comes from a masked prompt.
		if len(parts) >= 2 {
			fmt.Fprintln(r.Out,
				"note: inline keys are visible in scrollback; "+
					"using the prompt instead")
		}
		key, err := r.readSecret("Enter API key: ")
		if err != nil {
			fmt.Fprintf(r.Out, "error: %v\n", err)

			return true, false, nil
		}
		if key == "" {
			fmt.Fprintln(r.Out, "empty input; apikey unchanged.")

			return true, false, nil
		}
		msg, err := r.ConfigOverrider.Set("apikey", key)
		if err != nil {
			fmt.Fprintf(r.Out, "error: %v\n", err)

			return true, false, nil
		}
		fmt.Fprintln(r.Out, msg)

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

			case "chat":
				r.Mode = "chat"
				fmt.Fprintln(r.Out, "Switched to chat mode.")

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
		help := r.buildHelp()
		fmt.Fprint(r.Out, help)

		return true, false, nil

	case "/memory":
		return r.handleMemoryCommand(parts)

	case "/context":
		return r.handleContextCommand(parts)

	case "/log":
		return r.handleLogCommand(parts)

	case "/set":
		if len(parts) < 3 {
			fmt.Fprint(r.Out, "usage: /set <param> <value>\n")
			fmt.Fprint(r.Out,
				"Try /help for list of overrideable params.\n",
			)

			return true, false, nil
		}
		param := parts[1]
		value := strings.Join(parts[2:], " ")
		if r.ConfigOverrider == nil {
			fmt.Fprintln(r.Out,
				"Config override not available.",
			)

			return true, false, nil
		}
		msg, err := r.ConfigOverrider.Set(param, value)
		if err != nil {
			fmt.Fprintf(r.Out, "error: %v\n", err)
		} else {
			fmt.Fprintf(r.Out, "%s\n", msg)
		}

		return true, false, nil

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
		fmt.Fprintf(r.Out,
			"  Compressions this session: %d\n",
			r.Compressor.CompressionCount.Load())
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
		offloaded := r.Compressor.OffloadCount.Load()
		fmt.Fprintf(r.Out, "Offloaded messages: %d\n", offloaded)
		return true, false, nil

	case "model":
		fmt.Fprintf(r.Out, "Model: %s\n", r.Model)
		if r.ModelInfo.ContextWindow > 0 {
			fmt.Fprintf(r.Out,
				"  Context window: %d (API-verified)\n",
				r.ModelInfo.ContextWindow)
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

// buildHelp returns the full help text including config parameters.
func (r *REPL) buildHelp() string {
	var b strings.Builder

	b.WriteString(`Shmorby help

AGENT MODES
  tab / shift+tab    Cycle agent modes
  operate            Full tool access (default)
  diagnose           Read-only inspection
  chat               General conversation & research

SLASH COMMANDS
  /help              Show this help
  /set <param> <value>  Override a config parameter (runtime only)
  /quit              Exit shmorby
  /reset             Clear conversation history
  /model <name>      Switch LLM model
  /platform <name>   Switch LLM provider
  /apikey            Set API key (hidden prompt)
  /agent <mode>      Switch agent mode
  /scope             Show loaded scope context
  /memory            Memory management
  /context           Token usage and compression stats
  /log <level>       Set log verbosity
  /tui               Toggle fullscreen mode
`)

	// CONFIG PARAMETERS section.
	b.WriteString("CONFIG PARAMETERS (current value - valid options)\n")
	if r.ConfigOverrider != nil {
		for _, p := range r.ConfigOverrider.OverrideableParams() {
			// Pad key to align values.
			key := p.Key
			if len(key) < 30 {
				key += strings.Repeat(" ", 30-len(key))
			}
			opt := p.ValidOptions
			b.WriteString(fmt.Sprintf("  %s %s - %s\n", key, p.CurrentValue, opt))
		}
	}
	b.WriteString("\n")

	b.WriteString(`KEYBOARD SHORTCUTS
  ctrl+h             Show help
  ctrl+p             Command palette
  ctrl+r             Reverse-i-search input history
  ctrl+c             Quit shmorby
  ctrl+z             Suspend (fg to resume)
  ctrl+v             Paste from clipboard
  ctrl+l             Toggle log section
  ctrl+t             Toggle thinking block
  ctrl+x             Leader key
  tab / shift+tab    Cycle agent modes (empty input)
  pgup / pgdn        Scroll output by page
  up / down          Scroll output / input history
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
  shell              ` + r.cfgPermLevel("shell") + `
  ssh                ` + r.cfgPermLevel("ssh") + `
  sudo               ` + r.cfgPermLevel("sudo") + ` (default disabled)
  aws                ` + r.cfgPermLevel("aws") + ` (default disabled)

Current mode: ` + r.Mode + `
`)

	return b.String()
}

// cfgPermLevel returns the config permission level for a tool.
func (r *REPL) cfgPermLevel(tool string) string {
	if r.ConfigOverrider != nil {
		// Read from shared config via overrider.
		for _, p := range r.ConfigOverrider.OverrideableParams() {
			if p.Key == "permission."+tool {
				return p.CurrentValue
			}
		}
	}
	return "ask"
}

// toolPermissionFunc implements the permission callback for the REPL,
// prompting the user via a fresh scanner on In.
func (r *REPL) toolPermissionFunc(
	toolName, command, reason string,
) ToolPermissionResponse {
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

	if r.inRaw {
		buf := make([]byte, 1)
		// Bound EINTR retries for the same reason as readLine:
		// Ctrl+Z during a permission prompt must not silently
		// return PermDeny.
		eintrRetries := 0
		const maxEintrRetries = 10
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if errors.Is(err, syscall.EINTR) && eintrRetries < maxEintrRetries {
					eintrRetries++
					continue
				}

				return PermDeny
			}
			eintrRetries = 0
			if n == 0 {
				continue
			}
			switch buf[0] {
			case 'y', 'Y':
				fmt.Fprintln(r.Out, "y")

				return PermAllow
			case 'n', 'N':
				fmt.Fprintln(r.Out, "n")

				return PermDeny
			case 'a', 'A':
				fmt.Fprintln(r.Out, "a")

				return PermAllowAll
			}
		}
	}

	// Use single-byte reads for non-raw mode to avoid bufio.Scanner
	// over-reading past the permission response.
	buf := make([]byte, 1)
	var line []byte
	for {
		n, err := r.In.Read(buf)
		if err != nil || n == 0 {
			return PermDeny
		}
		c := buf[0]
		switch {
		case c == '\n' || c == '\r':
			s := strings.TrimSpace(string(line))
			line = nil
			switch strings.ToLower(s) {
			case "y", "yes":
				return PermAllow
			case "n", "no":
				return PermDeny
			case "a", "all":
				return PermAllowAll
			default:
				fmt.Fprint(r.Out, "y/n/a: ")
			}
		default:
			line = append(line, c)
		}
	}
}
