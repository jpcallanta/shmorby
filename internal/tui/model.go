// Package tui implements the Bubbletea-based terminal UI.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"shmorby/internal/agent"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	tuicl "shmorby/internal/tui/clipboard"
	tuicompl "shmorby/internal/tui/completion"
	"shmorby/internal/tui/history"
	"shmorby/internal/tui/keybinds"
	"shmorby/internal/tui/navigation"
	"shmorby/internal/tui/palette"
	tuirender "shmorby/internal/tui/render"
	"shmorby/internal/tui/sessiontab"
	"shmorby/internal/tui/spinner"
	"shmorby/internal/tui/styles"
	tuivp "shmorby/internal/tui/viewport"
)

// Messages sent through the Bubbletea update loop.
type submitMsg struct{ text string }
type agentReplyMsg struct {
	text          string
	memoryEntries int
}
type toolStatusMsg struct {
	name   string
	status string
}
type errorMsg struct{ err error }
type outputMsg struct{ text string }
type spinnerTickMsg struct{}

// Streaming messages.
type streamDeltaMsg struct {
	delta string
}
type streamDoneMsg struct{}

// Permission messages.
type permissionResultMsg struct {
	choice PermissionChoice
}
type permissionReqMsg struct {
	prompt PermissionPrompt
}

// settleMsg signals that the stream settle timer has expired.
type settleMsg struct{}

// spinnerStopMsg stops the spinner after output is rendered.
type spinnerStopMsg struct{}

// agentModeChangedMsg signals that the agent mode was switched.
type agentModeChangedMsg struct {
	mode string
}

// leaderTimeoutMsg signals that the leader key sequence timed out.
type leaderTimeoutMsg struct{}

// Logging messages.
type logEntryMsg struct {
	entry LogEntry
}
type thinkingDeltaMsg struct {
	delta string
}
type thinkingEndMsg struct{}
type setLogLevelMsg struct {
	level slog.Level
}
type agentEventMsg struct {
	event agent.AgentEvent
}

// subagentEventMsg notifies the TUI about subagent lifecycle events.
type subagentEventMsg struct {
	event tools.SubagentEvent
}

// outputEntry is a single line in the scrollable output pane.
type outputEntry struct {
	kind string // "user", "agent", "tool", "error"
	text string
}

// Model is the top-level Bubbletea model.
type Model struct {
	// Input
	textarea textarea.Model

	// Output
	output   []outputEntry
	viewport tuivp.Model

	// Streaming
	streamBuf StreamBuffer

	// Spinner
	spinner     spinner.Model
	spinnerText string
	startTime   time.Time
	tokensDown  int

	// State
	running    bool
	fullscreen bool
	width      int
	height     int

	// Agent
	provider  llm.Provider
	session   *session.Session
	mode      string
	scope     string
	model     string
	override  string
	registry  *tools.Registry
	maxIter   int
	shell     bool
	scopeInfo ScopeInfo

	// Theme
	theme styles.Theme

	// Context cancel
	cancel context.CancelFunc

	// Slash-command completion
	complEngine    *tuicompl.Engine
	complMatches   []tuicompl.Command
	complIdx       int
	showCompletion bool

	// Permission prompt
	permission *PermissionPrompt

	// Current tool being executed
	currentTool       string
	currentToolStatus string

	// Output selection
	selectionMode  bool
	selectionStart int
	selectionEnd   int
	copyNotify     string
	copyNotifyTime time.Time

	// Configuration
	glamourEnabled bool
	scrollLines    int

	// Stream settle timer
	settleTimer *time.Timer

	// Memory
	memoryStore memory.Store
	retriever   *memory.Retriever

	// Context compression
	compressor *ctxcomp.Compressor
	modelInfo  llm.ModelInfo
	ctxStats   *CtxStats

	// Pending actions for confirmation prompts
	pendingClearMemory bool
	haltPrompt         bool

	// Phase 19: navigation components
	modeSwitcher      *navigation.ModeSwitcher
	referenceEngine   *navigation.ReferenceEngine
	shellCmdHandler   *navigation.ShellCmdHandler
	scrollAccel       *navigation.ScrollAcceleration
	leaderKey         *keybinds.LeaderKey
	whichKey          *keybinds.WhichKeyModel
	commandPalette    *palette.CommandPalette
	inputHistory      *history.History
	reverseSearch     *history.ReverseISearch
	tabBar            *sessiontab.TabBar
	showReverseSearch bool

	// Phase 20: logging
	logEntries           []LogEntry
	logExpanded          bool
	logLevel             slog.Level
	thinking             ThinkingBuffer
	thinkingExpanded     bool
	logChan              chan LogEntry
	logMaxEntries        int
	logDisplayLimit      int
	logCollapse          bool
	logCollapseThreshold int
	logHandler           *TUILogHandler

	// Agent event channel (tool status from agent loop).
	agentEventChan chan agent.AgentEvent

	// Subagent event channel (child session lifecycle from orchestrator).
	subagentEventChan chan tools.SubagentEvent

	// Phase 21: help overlay
	showHelp *HelpModel

	// Phase 26: interactive permission prompts
	permissionReqChan chan PermissionPrompt
	toolRules         map[string]*tools.RuleSet

	// Phase 32: runtime config overrides.
	configOverrider *agent.ConfigOverrider

	// Phase 34: child subagent sessions keyed by session ID.
	childSessions map[string]*session.Session

	// Phase 34: subagent orchestrator for permission propagation.
	orchestrator *tools.TaskOrchestrator

	// Backup of parent output entries for tab switching.
	parentOutput []outputEntry

	// Phase 37: async status description generator.
	statusGen *agent.StatusGenerator
}

// CtxStats holds compression and token usage statistics for display.
type CtxStats struct {
	EstimatedTokens   int
	ContextWindow     int
	Compressions      int
	Mode              string
	Fallback          bool
	OffloadedMessages int
	StorageUsedBytes  int64
}

// ScopeInfo holds scope metadata for the /scope command.
type ScopeInfo struct {
	PrimaryPath  string
	Instructions []string
	TotalBytes   int
}

// Config holds dependencies for creating the TUI model.
type Config struct {
	Provider       llm.Provider
	Session        *session.Session
	Mode           string
	Scope          string
	Model          string
	Override       string
	Registry       *tools.Registry
	MaxToolIter    int
	ShellEnabled   bool
	Fullscreen     bool
	ThemeName      string
	ScopeInfo      ScopeInfo
	GlamourEnabled bool
	ScrollLines    int
	FollowMode     bool
	MemoryStore    memory.Store
	Retriever      *memory.Retriever
	Compressor     *ctxcomp.Compressor
	ModelInfo      llm.ModelInfo

	// Phase 19: navigation config
	Shell         string
	PermLevel     string
	LeaderKeyStr  string
	LeaderTimeout int
	HistorySize   int

	// ToolTimeout is the default timeout in seconds for tool calls.
	ToolTimeout int

	// Phase 20: logging config
	LogEnabled           bool
	LogDefaultLevel      string
	LogMaxEntries        int
	LogDisplayLimit      int
	LogCollapse          bool
	LogCollapseThreshold int
	LogChan              chan LogEntry
	LogHandler           *TUILogHandler

	// Phase 26: per-tool permission rule sets
	ToolRules map[string]*tools.RuleSet

	// Phase 32: runtime config overrides.
	ConfigOverrider *agent.ConfigOverrider

	// Phase 34: subagent lifecycle events channel (nil = no TUI tracking).
	SubagentEventChan chan tools.SubagentEvent

	// Phase 34: subagent orchestrator for permission propagation.
	Orchestrator *tools.TaskOrchestrator

	// Phase 37: async status description generator (nil = random text).
	StatusGen *agent.StatusGenerator
}

// NewModel creates a Bubbletea model ready to run.
func NewModel(cfg Config) Model {
	theme := styles.GetTheme(cfg.ThemeName)
	vp := tuivp.New(80, 20)

	ta := textarea.New()
	ta.Placeholder = ""
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.SetWidth(80)
	ta.Focus()

	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)

	if !cfg.FollowMode {
		vp.SetFollowMode(false)
	}
	scrollLines := cfg.ScrollLines
	if scrollLines <= 0 {
		scrollLines = 5
	}
	histSize := cfg.HistorySize
	if histSize <= 0 {
		histSize = 100
	}
	leaderTimeout := time.Duration(cfg.LeaderTimeout) * time.Millisecond
	if leaderTimeout <= 0 {
		leaderTimeout = 2 * time.Second
	}

	ms := navigation.NewModeSwitcher()
	if cfg.Mode != "" {
		ms.SetCurrent(cfg.Mode)
	}

	lk := keybinds.NewLeaderKey(cfg.LeaderKeyStr, leaderTimeout)
	lk.RegisterBinding("c", keybinds.ActionCompact)
	lk.RegisterBinding("n", keybinds.ActionNew)
	lk.RegisterBinding("l", keybinds.ActionList)
	lk.RegisterBinding("m", keybinds.ActionModel)
	lk.RegisterBinding("t", keybinds.ActionTheme)
	lk.RegisterBinding("a", keybinds.ActionAgent)
	lk.RegisterBinding("u", keybinds.ActionUndo)
	lk.RegisterBinding("r", keybinds.ActionRedo)
	lk.RegisterBinding("e", keybinds.ActionEditor)
	lk.RegisterBinding("x", keybinds.ActionExport)
	lk.RegisterBinding("q", keybinds.ActionQuit)
	lk.RegisterBinding("s", keybinds.ActionStatus)
	lk.RegisterBinding("b", keybinds.ActionSidebar)
	lk.RegisterBinding("h", keybinds.ActionTips)
	lk.RegisterBinding("y", keybinds.ActionCopy)
	lk.RegisterBinding("j", keybinds.ActionSessionChild)
	lk.RegisterBinding("k", keybinds.ActionSessionParent)

	h := history.New(histSize)
	cp := palette.New()
	cp.AddItem(palette.CommandItem{
		Name: "quit", Slash: "/quit", Description: "Exit shmorby",
	})
	cp.AddItem(palette.CommandItem{
		Name: "reset", Slash: "/reset", Description: "Clear session history",
	})
	cp.AddItem(palette.CommandItem{
		Name: "model", Slash: "/model", Description: "Show current model",
	})
	cp.AddItem(palette.CommandItem{
		Name: "agent", Slash: "/agent", Description: "Switch agent mode",
	})
	cp.AddItem(palette.CommandItem{
		Name: "scope", Slash: "/scope", Description: "Show scope files",
	})
	cp.AddItem(palette.CommandItem{
		Name: "memory", Slash: "/memory", Description: "Memory management",
	})
	cp.AddItem(palette.CommandItem{
		Name: "context", Slash: "/context", Description: "Context stats",
	})
	cp.AddItem(palette.CommandItem{
		Name: "help", Slash: "/help", Description: "Show help",
	})
	cp.AddItem(palette.CommandItem{
		Name: "set", Slash: "/set", Description: "Override config parameter",
	})

	// Parse default log level.
	logLevel := slog.LevelInfo
	switch cfg.LogDefaultLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	logChan := cfg.LogChan
	if logChan == nil {
		logChan = make(chan LogEntry, 100)
	}

	logMax := cfg.LogMaxEntries
	if logMax <= 0 {
		logMax = 100
	}
	logDisplay := cfg.LogDisplayLimit
	if logDisplay <= 0 {
		logDisplay = 20
	}
	logThreshold := cfg.LogCollapseThreshold
	if logThreshold <= 0 {
		logThreshold = 5
	}

	return Model{
		textarea:        ta,
		viewport:        vp,
		theme:           theme,
		provider:        cfg.Provider,
		session:         cfg.Session,
		mode:            cfg.Mode,
		scope:           cfg.Scope,
		model:           cfg.Model,
		override:        cfg.Override,
		registry:        cfg.Registry,
		maxIter:         cfg.MaxToolIter,
		shell:           cfg.ShellEnabled,
		fullscreen:      cfg.Fullscreen,
		scopeInfo:       cfg.ScopeInfo,
		complEngine:     tuicompl.New(),
		glamourEnabled:  cfg.GlamourEnabled,
		scrollLines:     scrollLines,
		memoryStore:     cfg.MemoryStore,
		retriever:       cfg.Retriever,
		compressor:      cfg.Compressor,
		modelInfo:       cfg.ModelInfo,
		modeSwitcher:    ms,
		referenceEngine: navigation.NewReferenceEngine(),
		shellCmdHandler: navigation.NewShellCmdHandler(
			navigation.OSExecutor{}, cfg.Shell, cfg.Mode, cfg.PermLevel,
		),
		scrollAccel:          navigation.NewScrollAcceleration(),
		leaderKey:            lk,
		whichKey:             keybinds.NewWhichKey(cfg.LeaderKeyStr),
		commandPalette:       cp,
		inputHistory:         h,
		reverseSearch:        history.NewReverseISearch(h),
		tabBar:               sessiontab.New("default", "default"),
		logChan:              logChan,
		logLevel:             logLevel,
		logExpanded:          !cfg.LogCollapse,
		logMaxEntries:        logMax,
		logDisplayLimit:      logDisplay,
		logCollapse:          cfg.LogCollapse,
		logCollapseThreshold: logThreshold,
		logHandler:           cfg.LogHandler,
		agentEventChan:       make(chan agent.AgentEvent, 20),
		subagentEventChan:    cfg.SubagentEventChan,
		orchestrator:         cfg.Orchestrator,
		permissionReqChan:    make(chan PermissionPrompt),
		toolRules:            cfg.ToolRules,
		showHelp:             NewHelpModel(),
		configOverrider:      cfg.ConfigOverrider,
		childSessions:        make(map[string]*session.Session),
		statusGen:            cfg.StatusGen,
	}
}

// Init returns the initial command (no-op) plus a log listener when
// the log channel is configured.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tea.HideCursor)
	if m.logChan != nil {
		cmds = append(cmds, m.listenLogChan())
	}
	cmds = append(cmds, m.listenAgentEvents())
	if m.subagentEventChan != nil {
		cmds = append(cmds, m.listenSubagentEvents())
	}
	cmds = append(cmds, m.listenPermissionReqs())
	return tea.Batch(cmds...)
}

// listenLogChan reads from the log channel and returns entries as
// bubbletea messages. Returns a follow-up command to keep listening.
func (m Model) listenLogChan() tea.Cmd {
	return func() tea.Msg {
		entry, ok := <-m.logChan
		if !ok {
			return nil
		}
		return logEntryMsg{entry: entry}
	}
}

// listenAgentEvents reads from the agent event channel and returns
// events as bubbletea messages. Returns a follow-up command.
func (m Model) listenAgentEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.agentEventChan
		if !ok {
			return nil
		}
		return agentEventMsg{event: ev}
	}
}

// listenSubagentEvents reads from the subagent event channel and
// returns events as bubbletea messages. Returns a follow-up command.
func (m Model) listenSubagentEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.subagentEventChan
		if !ok {
			return nil
		}
		return subagentEventMsg{event: ev}
	}
}

// listenPermissionReqs reads from the permission request channel and
// returns prompts as bubbletea messages. Returns a follow-up command.
func (m Model) listenPermissionReqs() tea.Cmd {
	return func() tea.Msg {
		prompt, ok := <-m.permissionReqChan
		if !ok {
			return nil
		}
		return permissionReqMsg{prompt: prompt}
	}
}

// Update handles incoming messages and key events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		m.textarea.SetHeight(m.inputLineHeight())
		m.ensureLayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		m.viewport.MouseMsg(msg)
		// Sync selection mode from viewport (click enters selection mode).
		if m.viewport.SelectionMode() && !m.selectionMode {
			m.selectionMode = true
			m.textarea.Blur()
		}
		// Sync drag selection continuously for highlighting.
		if m.selectionMode {
			start, end, active := m.viewport.DragSelection()
			if active || m.viewport.IsDragging() {
				m.selectionStart = start
				m.selectionEnd = end
				m.syncViewport()
			}
		}
		return m, nil

	case submitMsg:
		return m.handleSubmit(msg.text)

	case agentReplyMsg:
		m.running = false
		m.currentTool = ""
		m.currentToolStatus = ""
		m.updateCtxStats()
		// Estimate token count from reply length (~4 chars/token).
		m.tokensDown = len(msg.text) / 4
		m.textarea.Reset()
		text := msg.text
		if m.glamourEnabled {
			text = tuirender.RenderMarkdown(text, m.width-2)
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: text,
		})
		// Show memory indicator when memory was used.
		if msg.memoryEntries > 0 {
			memoryIndicator := fmt.Sprintf(
				"[memory: %d entries]",
				msg.memoryEntries,
			)
			m.output = append(m.output, outputEntry{
				kind: "memory",
				text: memoryIndicator,
			})
		}
		m.syncViewport()
		// Delay spinner stop so user sees output before spinner
		// disappears (prevents "frozen" perception).
		return m, tea.Batch(
			tea.Tick(
				200*time.Millisecond,
				func(_ time.Time) tea.Msg {
					return spinnerStopMsg{}
				},
			),
		)

	case settleMsg:
		return m.handleSettle()

	case spinnerStopMsg:
		m.spinner.Stop()
		return m, nil

	case agentModeChangedMsg:
		m.mode = msg.mode
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Switched to %s mode.", msg.mode),
		})
		m.syncViewport()
		return m, nil

	case leaderTimeoutMsg:
		if m.leaderKey.Active() {
			if time.Now().After(m.leaderKey.Deadline()) {
				m.leaderKey.Deactivate()
				m.whichKey.Dismiss()
			} else {
				return m, tea.Tick(
					time.Until(m.leaderKey.Deadline()),
					func(_ time.Time) tea.Msg {
						return leaderTimeoutMsg{}
					},
				)
			}
		}
		return m, nil

	case logEntryMsg:
		m.logEntries = append(m.logEntries, msg.entry)
		if len(m.logEntries) > m.logMaxEntries {
			m.logEntries = m.logEntries[len(m.logEntries)-m.logMaxEntries:]
		}
		if len(m.logEntries) > m.logCollapseThreshold {
			m.logExpanded = false
		}
		m.syncViewport()
		return m, m.listenLogChan()

	case thinkingDeltaMsg:
		m.thinking.AddDelta(msg.delta)
		m.syncViewport()
		return m, nil

	case thinkingEndMsg:
		m.thinking.End()
		m.syncViewport()
		return m, nil

	case setLogLevelMsg:
		m.logLevel = msg.level
		if m.logHandler != nil {
			m.logHandler.SetLevel(msg.level)
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Log level: %s", msg.level),
		})
		m.syncViewport()
		return m, nil

	case permissionReqMsg:
		m.permission = &msg.prompt
		return m, m.listenPermissionReqs()

	case subagentEventMsg:
		ev := msg.event
		switch ev.Status {
		case "":
			if sess, ok := ev.Session.(*session.Session); ok && sess != nil {
				m.childSessions[ev.SessionID] = sess
			}
			m.tabBar.AddTab(sessiontab.Tab{
				ID: ev.SessionID, Label: ev.Label,
				Active: false, Spinning: true,
				IsSubagent: true, ParentID: ev.ParentID,
			})
		default:
			m.tabBar.UpdateTabStatus(ev.SessionID, ev.Status)
		}

		return m, m.listenSubagentEvents()

	case agentEventMsg:
		switch msg.event.Type {
		case "tool-start":
			m.currentTool = msg.event.Name
			m.currentToolStatus = msg.event.Info
			m.spinner.Start("running…")
			m.output = append(m.output, outputEntry{
				kind: "tool",
				text: fmt.Sprintf(
					"$ %s", msg.event.Info,
				),
			})
			m.syncViewport()
		case "tool-status":
			// Replace generic spinner text with inference-generated
			// description when available.
			if msg.event.Status != "" {
				m.spinner.Start(msg.event.Status)
			}
		case "tool-end":
			m.spinner.Start("thinking…")
			m.output = append(m.output, outputEntry{
				kind: "tool",
				text: fmt.Sprintf(
					"%s: %s", msg.event.Name, msg.event.Info,
				),
			})
			if msg.event.Output != "" {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: msg.event.Output,
				})
			}
			m.currentTool = ""
			m.currentToolStatus = ""
			m.syncViewport()
		}
		return m, m.listenAgentEvents()

	case outputMsg:
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: msg.text,
		})
		m.syncViewport()
		return m, nil

	case streamDeltaMsg:
		if m.settleTimer != nil {
			m.settleTimer.Stop()
			m.settleTimer = nil
		}
		lines := m.streamBuf.WriteToken(msg.delta)
		m.tokensDown = m.streamBuf.Tokens()
		for _, line := range lines {
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: line,
			})
		}
		m.syncViewport()
		return m, nil

	case streamDoneMsg:
		// Defer final render until stream settles (50ms since last delta).
		if m.streamBuf.SettleElapsed() < 50*time.Millisecond {
			m.settleTimer = time.NewTimer(
				50*time.Millisecond - m.streamBuf.SettleElapsed(),
			)
			return m, func() tea.Msg {
				<-m.settleTimer.C
				return settleMsg{}
			}
		}
		return m.finalizeStream()

	case toolStatusMsg:
		m.currentTool = msg.name
		m.currentToolStatus = msg.status
		m.output = append(m.output, outputEntry{
			kind: "tool",
			text: fmt.Sprintf("%s: %s", msg.name, msg.status),
		})
		m.syncViewport()
		return m, nil

	case errorMsg:
		m.running = false
		m.currentTool = ""
		m.currentToolStatus = ""
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: msg.err.Error(),
		})
		m.syncViewport()
		return m, tea.Batch(
			tea.Tick(
				200*time.Millisecond,
				func(_ time.Time) tea.Msg {
					return spinnerStopMsg{}
				},
			),
		)

	case spinnerTickMsg:
		if m.running {
			m.spinner.Tick()
			return m, tea.Tick(
				100*time.Millisecond,
				func(_ time.Time) tea.Msg { return spinnerTickMsg{} },
			)
		}
		return m, nil

	case permissionResultMsg:
		if m.permission != nil {
			m.permission.Choice <- msg.choice
			m.permission = nil
		}
		if m.pendingClearMemory {
			m.pendingClearMemory = false
			if msg.choice == PermissionAllow {
				m.executeMemoryClear()
			} else {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: "Memory clear cancelled.",
				})
				m.syncViewport()
			}
		}
		return m, nil
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Halt-all prompt active: only y/n/esc.
	if m.haltPrompt {
		switch msg.String() {
		case "y":
			m.haltPrompt = false
			if m.cancel != nil {
				m.cancel()
			}
			m.running = false
			m.spinner.Stop()
			m.currentTool = ""
			m.currentToolStatus = ""
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: "All operations halted.",
			})
			m.syncViewport()
			return m, nil
		case "n", "esc":
			m.haltPrompt = false
			return m, nil
		}
		return m, nil
	}

	// Help overlay active: scroll or close.
	if m.showHelp != nil && m.showHelp.Visible() {
		// Keep the scroll clamp in sync with the real content size.
		m.showHelp.SetContentHeight(m.helpContentHeight())
		switch msg.Type {
		case tea.KeyPgUp:
			m.showHelp.PageUp(m.height)
			return m, nil
		case tea.KeyPgDown:
			m.showHelp.PageDown(m.height)
			return m, nil
		case tea.KeyUp:
			m.showHelp.ScrollUp()
			return m, nil
		case tea.KeyDown:
			m.showHelp.ScrollDown(m.height)
			return m, nil
		case tea.KeyHome:
			m.showHelp.ScrollToTop()
			return m, nil
		case tea.KeyEnd:
			m.showHelp.ScrollToBottom(m.height)
			return m, nil
		case tea.KeyEsc, tea.KeyEnter:
			m.showHelp.Hide()
			return m, nil
		default:
			m.showHelp.Hide()
			return m, nil
		}
	}

	// Permission prompt active: only y/n/a/esc.
	if m.permission != nil {
		switch msg.String() {
		case "y":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionAllow}
			}
		case "n":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionDeny}
			}
		case "a":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionAllowAll}
			}
		case "esc":
			return m, func() tea.Msg {
				return permissionResultMsg{choice: PermissionDeny}
			}
		}
		return m, nil
	}

	// Clipboard keybindings.
	s := msg.String()
	switch s {
	case "ctrl+c":
		var copied bool
		if m.selectionMode && m.selectionStart != m.selectionEnd {
			lines := outputTexts(m.output)
			start, end := m.selectionStart, m.selectionEnd
			if start > end {
				start, end = end, start
			}
			if start < len(lines) {
				if end > len(lines) {
					end = len(lines)
				}
				raw := strings.Join(lines[start:end], "\n")
				tuicl.Copy(StripANSI(raw))
				copied = true
			}
		} else if len(m.output) > 0 {
			// Copy last agent reply when no selection active.
			for i := len(m.output) - 1; i >= 0; i-- {
				if m.output[i].kind == "agent" {
					tuicl.Copy(StripANSI(m.output[i].text))
					copied = true
					break
				}
			}
		}
		if copied {
			m.copyNotify = "✓ copied to clipboard"
			m.copyNotifyTime = time.Now()
		}
		return m, nil
	case "ctrl+v":
		pasteText := tuicl.Paste()
		if pasteText != "" {
			val := m.textarea.Value()
			pos := len(val)
			m.textarea.SetValue(val + pasteText)
			m.textarea.SetCursor(pos + len(pasteText))
		}
		return m, nil
	case "ctrl+shift+pgdown":
		m.viewport.GotoBottom()
		return m, nil
	case "ctrl+shift+pgup":
		m.viewport.GotoTop()
		return m, nil
	}

	// Selection mode active: only esc to exit.
	if m.selectionMode {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyEnter:
			m.selectionMode = false
			m.viewport.SetSelectionMode(false)
			m.textarea.Focus()
			m.selectionStart = 0
			m.selectionEnd = 0
			return m, nil
		}
		return m, nil
	}

	// Command palette active: route keys to palette filter.
	if m.commandPalette.Visible() {
		return m.handlePaletteKey(msg)
	}

	// Reverse search active: route keys to search filter.
	if m.reverseSearch.Visible() {
		return m.handleReverseSearchKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case tea.KeyEnter:
		if m.running {
			return m, nil
		}
		if m.showCompletion && len(m.complMatches) > 0 {
			text := strings.TrimSpace(m.textarea.Value())
			if text == m.complMatches[m.complIdx].Name {
				m.showCompletion = false
				m.complMatches = nil
			} else {
				sel := m.complMatches[m.complIdx]
				m.textarea.SetValue(sel.Name + " ")
				m.textarea.SetCursor(len(sel.Name) + 1)
				m.showCompletion = false
				m.complMatches = nil
				return m, nil
			}
		}
		text := strings.TrimSpace(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		m.textarea.Reset()
		m.inputHistory.Add(text)
		m.showCompletion = false
		m.complMatches = nil
		return m, func() tea.Msg { return submitMsg{text: text} }

	case tea.KeyRight:
		if m.textarea.Value() == "" && m.tabBar.Visible() {
			m.nextChildTab()
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyLeft:
		if m.textarea.Value() == "" && m.tabBar.Visible() {
			m.prevChildTab()
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyUp:
		if m.showCompletion && len(m.complMatches) > 0 {
			m.complIdx--
			if m.complIdx < 0 {
				m.complIdx = len(m.complMatches) - 1
			}
			return m, nil
		}
		if m.textarea.Value() == "" && m.tabBar.Visible() {
			m.parentTab()
			return m, nil
		}
		if (m.textarea.Value() != "" && m.inputHistory.AtNewest()) || m.inputHistory.Size() == 0 {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		if entry, ok := m.inputHistory.Older(); ok {
			m.textarea.SetValue(entry)
		}
		return m, nil

	case tea.KeyDown:
		if m.showCompletion && len(m.complMatches) > 0 {
			m.complIdx++
			if m.complIdx >= len(m.complMatches) {
				m.complIdx = 0
			}
			return m, nil
		}
		// Empty input or cursor not at end → advance history.
		if m.textarea.Value() == "" || !m.inputHistory.AtNewest() {
			if entry, ok := m.inputHistory.Newer(); ok {
				if entry != "" {
					m.textarea.SetValue(entry)
				} else {
					m.textarea.Reset()
				}
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyTab:
		if m.showCompletion && len(m.complMatches) > 0 {
			sel := m.complMatches[m.complIdx]
			m.textarea.SetValue(sel.Name + " ")
			m.textarea.SetCursor(len(sel.Name) + 1)
			m.showCompletion = false
			m.complMatches = nil
			return m, nil
		}
		// Empty input → cycle agent mode forward (Tab).
		if m.textarea.Value() == "" && !m.running {
			m.modeSwitcher.CycleForward()
			m.mode = m.modeSwitcher.Current()
			m.shellCmdHandler.SetMode(m.mode)
			return m, func() tea.Msg {
				return agentModeChangedMsg{mode: m.mode}
			}
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyShiftTab:
		// Shift+Tab → cycle agent mode reverse.
		if m.textarea.Value() == "" && !m.running {
			m.modeSwitcher.CycleReverse()
			m.mode = m.modeSwitcher.Current()
			m.shellCmdHandler.SetMode(m.mode)
			return m, func() tea.Msg {
				return agentModeChangedMsg{mode: m.mode}
			}
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyEsc:
		if m.showCompletion {
			m.showCompletion = false
			m.complMatches = nil
			return m, nil
		}
		if m.leaderKey.Active() {
			m.leaderKey.Deactivate()
			m.whichKey.Dismiss()
			return m, nil
		}
		if m.commandPalette.Visible() {
			m.commandPalette.Dismiss()
			return m, nil
		}
		if m.reverseSearch.Visible() {
			m.reverseSearch.Dismiss()
			m.showReverseSearch = false
			return m, nil
		}
		if m.running && !m.haltPrompt {
			m.haltPrompt = true
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyCtrlP:
		// Command palette toggle.
		m.commandPalette.Toggle()
		return m, nil

	case tea.KeyCtrlL:
		// Toggle log section visibility.
		m.logExpanded = !m.logExpanded
		m.syncViewport()
		return m, nil

	case tea.KeyCtrlT:
		// Toggle thinking block visibility.
		m.thinkingExpanded = !m.thinkingExpanded
		m.syncViewport()
		return m, nil

	case tea.KeyCtrlH:
		// Toggle help overlay.
		if m.showHelp != nil {
			m.showHelp.Toggle()
		}
		return m, nil

	case tea.KeyCtrlR:
		// Reverse-i-search toggle.
		m.reverseSearch.Toggle()
		m.showReverseSearch = m.reverseSearch.Visible()
		return m, nil

	case tea.KeyCtrlS:
		// Reverse-i-search backward cycle when active.
		if m.reverseSearch.Visible() {
			m.reverseSearch.CycleReverse()
			return m, nil
		}
		return m, nil

	case tea.KeyPgUp:
		n := 1
		if m.scrollAccel != nil && m.scrollAccel.Enabled() {
			n = int(m.scrollAccel.Tick())
		}
		for i := 0; i < n; i++ {
			m.viewport.ScrollHalfPageUp()
		}
		return m, nil

	case tea.KeyPgDown:
		n := 1
		if m.scrollAccel != nil && m.scrollAccel.Enabled() {
			n = int(m.scrollAccel.Tick())
		}
		for i := 0; i < n; i++ {
			m.viewport.ScrollHalfPageDown()
		}
		return m, nil

	case tea.KeyRunes:
		// Leader key active → dispatch second key.
		if m.leaderKey.Active() {
			action, consumed := m.leaderKey.HandleKey(s)
			m.whichKey.Dismiss()
			if !consumed {
				return m, nil
			}
			return m.dispatchLeaderAction(action)
		}
		// @-reference trigger: detect @ at cursor position.
		if len(msg.Runes) == 1 && msg.Runes[0] == '@' {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.updateCompletion()
			return m, cmd
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCompletion()
		return m, cmd

	default:
		// Leader key detection.
		if m.leaderKey.IsLeaderKey(s) {
			m.leaderKey.Activate()
			m.whichKey.Show(
				m.leaderKey.BindingsList(),
				m.width,
				m.leaderKey.Timeout,
			)
			return m, func() tea.Msg {
				return leaderTimeoutMsg{}
			}
		}
		// Delegate to textarea, then check for completion trigger.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCompletion()
		return m, cmd
	}
}

// dispatchLeaderAction executes the action mapped by a leader-key binding.
func (m Model) dispatchLeaderAction(
	action keybinds.Action,
) (tea.Model, tea.Cmd) {
	switch action {
	case keybinds.ActionCompact:
		return m, func() tea.Msg {
			return submitMsg{text: "/context compact"}
		}
	case keybinds.ActionNew:
		return m, func() tea.Msg {
			return submitMsg{text: "new session"}
		}
	case keybinds.ActionModel:
		return m, func() tea.Msg {
			return submitMsg{text: "/model"}
		}
	case keybinds.ActionAgent:
		m.modeSwitcher.CycleForward()
		m.mode = m.modeSwitcher.Current()
		m.shellCmdHandler.SetMode(m.mode)
		return m, nil
	case keybinds.ActionQuit:
		return m, tea.Quit
	case keybinds.ActionStatus:
		return m, func() tea.Msg {
			return submitMsg{text: "/scope"}
		}
	case keybinds.ActionSessionChild:
		m.nextChildTab()
		return m, nil
	case keybinds.ActionSessionParent:
		m.parentTab()
		return m, nil
	case keybinds.ActionChild:
		m.firstChildTab()
		return m, nil
	default:
		return m, nil
	}
}

// updateCompletion checks the current input and updates completion state.
func (m *Model) updateCompletion() {
	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") {
		matches := m.complEngine.Complete(val)
		if len(matches) > 0 {
			m.complMatches = matches
			m.complIdx = 0
			m.showCompletion = true
			return
		}
	}
	// @-reference completion.
	if idx := strings.LastIndex(val, "@"); idx >= 0 {
		query := val[idx+1:]
		if idx == 0 || val[idx-1] == ' ' {
			items := m.referenceEngine.Complete(query)
			if len(items) > 0 {
				m.complMatches = nil
				for _, item := range items {
					m.complMatches = append(m.complMatches, tuicompl.Command{
						Name:        item.Label,
						Description: item.Kind,
					})
				}
				m.complIdx = 0
				m.showCompletion = true
				return
			}
		}
	}
	m.showCompletion = false
	m.complMatches = nil
}

// handleSubmit processes a user message by running the agent turn.
func (m Model) handleSubmit(text string) (tea.Model, tea.Cmd) {
	// !-prefixed shell commands run outside the agent loop.
	if handled, out, err := m.shellCmdHandler.Handle(text); handled {
		m.output = append(m.output, outputEntry{
			kind: "user",
			text: text,
		})
		if err != nil {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: err.Error(),
			})
		} else {
			result := navigation.FormatOutput(out)
			if result != "" {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: result,
				})
			}
		}
		m.syncViewport()
		return m, nil
	}

	// Resolve @-references in the message before sending to agent.
	text, refContent := m.resolveReferences(text)

	m.output = append(m.output, outputEntry{
		kind: "user",
		text: text,
	})
	m.syncViewport()

	if cmd, done, err := m.handleCommand(text); done {
		return m, tea.Quit
	} else if err != nil {
		return m, func() tea.Msg {
			return outputMsg{text: fmt.Sprintf("Error: %v", err)}
		}
	} else if cmd {
		return m, nil
	}

	// Clear completed subagent tabs from previous turn before
	// starting a new one.
	m.clearSubagentTabs()

	// Prepend resolved reference content as context for the agent.
	if refContent != "" {
		text = refContent + "\n\n" + text
	}

	m.running = true
	m.startTime = time.Now()
	m.tokensDown = 0
	m.streamBuf = NewStreamBuffer()
	m.spinner.Start("thinking…")

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	return m, tea.Batch(
		m.runAgentTurn(ctx, text),
		func() tea.Msg { return spinnerTickMsg{} },
		tea.HideCursor,
	)
}

// handleCommand processes slash commands. Returns (handled, shouldQuit, error).
func (m *Model) handleCommand(line string) (bool, bool, error) {
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
		m.session.Reset()
		m.output = m.output[:0]
		m.syncViewport()
		return true, false, nil

	case "/model":
		if len(parts) >= 2 {
			if m.configOverrider == nil {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: "Config override not available.",
				})
				m.syncViewport()
				return true, false, nil
			}
			msg, err := m.configOverrider.Set("model", parts[1])
			if err != nil {
				m.output = append(m.output, outputEntry{
					kind: "error",
					text: fmt.Sprintf("error: %v", err),
				})
				m.syncViewport()
				return true, false, nil
			}
			m.model = parts[1]
			llm.InvalidateModelInfo(parts[1])
			if m.provider != nil {
				if fetcher, ok := m.provider.(llm.ModelInfoFetcher); ok {
					info, fetchErr := llm.FetchModelInfo(
						context.Background(),
						fetcher, parts[1],
						m.configOverrider.Config(),
					)
					if fetchErr == nil {
						m.modelInfo = info
					}
				}
			}
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: msg,
			})
			m.syncViewport()
			return true, false, nil
		}
		providerName := "none"
		if m.provider != nil {
			providerName = m.provider.Name()
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("%s (%s)", providerName, m.model),
		})
		m.syncViewport()
		return true, false, nil

	case "/platform":
		if len(parts) >= 2 {
			if m.configOverrider == nil {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: "Config override not available.",
				})
				m.syncViewport()
				return true, false, nil
			}
			msg, err := m.configOverrider.Set(
				"provider", parts[1],
			)
			if err != nil {
				m.output = append(m.output, outputEntry{
					kind: "error",
					text: fmt.Sprintf("error: %v", err),
				})
				m.syncViewport()
				return true, false, nil
			}
			if newProv := m.configOverrider.Provider(); newProv != nil {
				m.provider = newProv
			}
			llm.InvalidateModelInfo(m.model)
			if m.provider != nil {
				if fetcher, ok := m.provider.(llm.ModelInfoFetcher); ok {
					info, fetchErr := llm.FetchModelInfo(
						context.Background(),
						fetcher, m.model,
						m.configOverrider.Config(),
					)
					if fetchErr == nil {
						m.modelInfo = info
					}
				}
			}
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: msg,
			})
			m.syncViewport()
			return true, false, nil
		}
		providerName := "none"
		if m.provider != nil {
			providerName = m.provider.Name()
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: providerName,
		})
		m.syncViewport()
		return true, false, nil

	case "/apikey":
		if len(parts) >= 2 {
			if m.configOverrider == nil {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: "Config override not available.",
				})
				m.syncViewport()
				return true, false, nil
			}
			value := strings.Join(parts[1:], " ")
			msg, err := m.configOverrider.Set(
				"apikey", value,
			)
			if err != nil {
				m.output = append(m.output, outputEntry{
					kind: "error",
					text: fmt.Sprintf("error: %v", err),
				})
				m.syncViewport()
				return true, false, nil
			}
			if newProv := m.configOverrider.Provider(); newProv != nil {
				m.provider = newProv
			}
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: msg,
			})
			m.syncViewport()
			return true, false, nil
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: "apikey is set (use /apikey <key> to change)",
		})
		m.syncViewport()
		return true, false, nil

	case "/agent":
		if len(parts) == 2 {
			if m.modeSwitcher.SetCurrent(parts[1]) {
				m.mode = parts[1]
				m.shellCmdHandler.SetMode(m.mode)
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: fmt.Sprintf("Switched to %s mode.", m.mode),
				})
				m.syncViewport()
				return true, false, nil
			}
			return true, false, fmt.Errorf("unknown agent mode: %s", parts[1])
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: m.mode,
		})
		m.syncViewport()
		return true, false, nil

	case "/scope":
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("scope: %d bytes", m.scopeInfo.TotalBytes))
		if m.scopeInfo.PrimaryPath != "" {
			sb.WriteString(fmt.Sprintf("\n  primary: %s", m.scopeInfo.PrimaryPath))
		}
		for _, inst := range m.scopeInfo.Instructions {
			sb.WriteString(fmt.Sprintf("\n  instruction: %s", inst))
		}
		text := sb.String()
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: text,
		})
		m.syncViewport()
		return true, false, nil

	case "/tui":
		m.fullscreen = !m.fullscreen
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Fullscreen: %v", m.fullscreen),
		})
		m.syncViewport()
		return true, false, nil

	case "/memory":
		m.handleMemoryCommand(parts)
		return true, false, nil

	case "/context":
		args := strings.TrimPrefix(line, "/context")
		args = strings.TrimSpace(args)
		m.handleContextCommand(args)
		return true, false, nil

	case "/help":
		if m.showHelp != nil {
			m.showHelp.Show()
		}
		return true, false, nil

	case "/log":
		if len(parts) == 2 {
			var lvl slog.Level
			switch parts[1] {
			case "debug":
				lvl = slog.LevelDebug
			case "info":
				lvl = slog.LevelInfo
			case "warn":
				lvl = slog.LevelWarn
			case "error":
				lvl = slog.LevelError
			default:
				return true, false, fmt.Errorf(
					"unknown log level: %s (want debug|info|warn|error)",
					parts[1],
				)
			}
			m.logLevel = lvl
			if m.logHandler != nil {
				m.logHandler.SetLevel(lvl)
			}
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: fmt.Sprintf("Log level: %s", lvl),
			})
			m.syncViewport()
			return true, false, nil
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Log level: %s", m.logLevel),
		})
		m.syncViewport()
		return true, false, nil

	case "/set":
		if len(parts) < 3 {
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: "usage: /set <param> <value>\n" +
					"Try /help for list of overrideable params.",
			})
			m.syncViewport()

			return true, false, nil
		}
		param := parts[1]
		value := strings.Join(parts[2:], " ")
		if m.configOverrider == nil {
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: "Config override not available.",
			})
			m.syncViewport()

			return true, false, nil
		}
		msg, err := m.configOverrider.Set(param, value)
		if err != nil {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: fmt.Sprintf("error: %v", err),
			})
		} else {
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: fmt.Sprintf("⚙ %s", msg),
			})
		}
		m.syncViewport()

		return true, false, nil

	default:
		return true, false, fmt.Errorf("unknown command: %s", parts[0])
	}
}

// runAgentTurn executes the agent in a goroutine and sends the reply.
// Uses streaming when the provider supports it and no tools are needed.
func (m Model) runAgentTurn(
	ctx context.Context, input string,
) tea.Cmd {
	return func() tea.Msg {
		// Track memory stats before the turn.
		var prevHits int
		if m.retriever != nil {
			prevHits = m.retriever.Stats().Hits
		}

		if m.registry == nil {
			events, err := m.provider.ChatStream(ctx, llm.ChatRequest{
				Model:  m.model,
				System: m.buildSystemPrompt(),
				Messages: []llm.Message{
					{Role: "user", Content: input},
				},
			})
			if err == nil {
				return m.consumeStream(events)
			}
		}

		var reply string
		var err error
		if m.registry != nil {
			// Only wire permission func when interactive rules
			// are configured (permission.interactive: true).
			var permFunc agent.ToolPermissionFunc
			if m.toolRules != nil {
				permFunc = m.toolPermissionFunc
			}
			// Propagate permission callback to subagent orchestrator
			// so subagents respect the same permission constraints.
			if m.orchestrator != nil {
				m.orchestrator.PermFunc = permFunc
			}
			reply, err = agent.RunTurnWithTools(
				ctx, m.provider, m.session,
				m.mode, m.scope, m.override, m.model, input,
				m.registry, m.maxIter, m.shell,
				m.memoryStore, m.retriever,
				m.compressor, m.modelInfo,
				func(ev agent.AgentEvent) {
					select {
					case m.agentEventChan <- ev:
					default:
					}
				},
				permFunc,
				m.toolRules,
				m.statusGen,
			)
		} else {
			reply, err = agent.RunTurn(
				ctx, m.provider, m.session,
				m.mode, m.scope, m.override, m.model, input,
				m.memoryStore, m.retriever,
				m.compressor, m.modelInfo,
			)
		}
		if err != nil {
			return errorMsg{err: err}
		}

		// Calculate memory entries used in this turn.
		var memoryEntries int
		if m.retriever != nil {
			currentHits := m.retriever.Stats().Hits
			if currentHits > prevHits {
				memoryEntries = m.retriever.Stats().LastCount
			}
		}

		return agentReplyMsg{text: reply, memoryEntries: memoryEntries}
	}
}

// consumeStream reads SSE events and converts them to tea.Msg values.
// It tracks whether reasoning deltas have been seen so that when a text
// delta arrives, the thinking block is finalized first.
func (m Model) consumeStream(
	events <-chan llm.StreamEvent,
) tea.Cmd {
	wasReasoning := false
	pendingText := ""
	return func() tea.Msg {
		// Return buffered text delta from a previous call.
		if pendingText != "" {
			d := pendingText
			pendingText = ""
			return streamDeltaMsg{delta: d}
		}
		for ev := range events {
			switch {
			case ev.Done:
				return streamDoneMsg{}
			case ev.Type == "reasoning" && ev.Delta != "":
				wasReasoning = true
				return thinkingDeltaMsg{delta: ev.Delta}
			case ev.Type == "text" && ev.Delta != "":
				if wasReasoning {
					wasReasoning = false
					pendingText = ev.Delta
					return thinkingEndMsg{}
				}
				return streamDeltaMsg{delta: ev.Delta}
			case ev.Type == "error":
				return errorMsg{
					err: fmt.Errorf("stream: %s", ev.Delta),
				}
			}
		}
		return streamDoneMsg{}
	}
}

// toolPermissionFunc implements agent.ToolPermissionFunc for the TUI.
// Sends a permission prompt to the TUI update loop and blocks until
// the user responds.
func (m *Model) toolPermissionFunc(
	toolName, command, reason string,
) agent.ToolPermissionResponse {
	prompt := NewPermissionPrompt(toolName, command, reason, "tool permission")
	m.permissionReqChan <- prompt

	choice := <-prompt.Choice

	switch choice {
	case PermissionAllowAll:
		return agent.PermAllowAll
	case PermissionAllow:
		return agent.PermAllow
	default:
		return agent.PermDeny
	}
}

// buildSystemPrompt returns the system prompt for streaming requests.
func (m Model) buildSystemPrompt() string {
	if m.override != "" {
		return m.override
	}
	return fmt.Sprintf(
		"You are in %s mode. %s",
		m.mode, m.scope,
	)
}

// finalizeStream renders the final accumulated content when streaming ends.
func (m Model) finalizeStream() (tea.Model, tea.Cmd) {
	// End any active thinking block.
	if m.thinking.Active() {
		m.thinking.End()
	}

	remaining := m.streamBuf.Flush()
	if remaining != "" {
		text := remaining
		if m.glamourEnabled {
			text = tuirender.RenderMarkdown(remaining, m.width-2)
		}
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: text,
		})
	}
	m.tokensDown = m.streamBuf.Tokens()
	m.running = false
	m.currentTool = ""
	m.currentToolStatus = ""
	m.textarea.Reset()
	m.updateCtxStats()
	m.syncViewport()
	// Delay spinner stop so user sees output first.
	return m, tea.Batch(
		tea.Tick(
			200*time.Millisecond,
			func(_ time.Time) tea.Msg {
				return spinnerStopMsg{}
			},
		),
	)
}

// handleSettle processes the settle timer expiry and finalizes the stream.
func (m Model) handleSettle() (tea.Model, tea.Cmd) {
	m.settleTimer = nil
	return m.finalizeStream()
}

// syncViewport updates the viewport with current output.
func (m *Model) syncViewport() {
	m.ensureLayout()
	vpWidth := m.viewport.Width()
	if vpWidth < 1 {
		vpWidth = 1
	}
	var sb strings.Builder
	for i, entry := range m.output {
		selected := m.selectionMode &&
			i >= m.selectionStart && i < m.selectionEnd
		// Strip incomplete ANSI sequences before styling.
		text := StripPartialANSI(entry.text)
		var rendered string
		switch entry.kind {
		case "user":
			rendered = m.theme.UserInput.Width(vpWidth).Render("❯ " + text)
		case "agent":
			rendered = m.theme.AgentReply.Width(vpWidth).Render(text)
		case "tool":
			rendered = m.theme.ToolRunning.Width(vpWidth).Render("⟳ " + text)
		case "error":
			rendered = m.theme.Error.Width(vpWidth).Render("✗ " + text)
		case "memory":
			rendered = m.theme.StatusKey.Width(vpWidth).Render("  " + text)
		}
		if selected {
			rendered = m.theme.Selection.Render(rendered)
		}
		sb.WriteString(rendered)
		sb.WriteString("\n")
	}

	// Thinking block (collapsible, above log section).
	if len(m.thinking.Lines()) > 0 || m.thinking.Active() {
		if m.thinkingExpanded {
			sb.WriteString(m.renderThinkingBlock())
		} else {
			sb.WriteString(m.renderThinkingPreview())
		}
	}

	// Log section (collapsible, at bottom of output).
	if len(m.logEntries) > 0 && m.logExpanded {
		sb.WriteString(m.renderLogSection())
	}

	m.viewport.SetContent(sb.String())
	m.viewport.NotifyContentAdded()
}

// ensureLayout recalculates viewport height from cached dimensions.
func (m *Model) ensureLayout() {
	if m.height <= 0 {
		return
	}
	inputHeight := m.inputLineHeight()
	// upper separator + lower separator + status bar.
	statusHeight := 3
	if m.tabBar.Visible() {
		statusHeight++
	}
	// New content indicator pushes status bar down by 1.
	if !m.viewport.FollowMode() && m.viewport.NewContent() {
		statusHeight++
	}
	vpHeight := m.height - inputHeight - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetHeight(vpHeight)
}

// inputLineHeight returns the number of visual lines the input prompt occupies.
func (m Model) inputLineHeight() int {
	inputText := StripPartialANSI(m.textarea.Value())
	avail := m.width - 2
	if avail < 1 {
		avail = 1
	}
	wrapped := wrapText(inputText, avail)
	lines := strings.Count(wrapped, "\n") + 1
	if lines < 1 {
		lines = 1
	}
	return lines
}

// cursorVisualPos returns the visual line and column of the cursor
// in the word-wrapped output for the given wrapped lines and original text.
func (m Model) cursorVisualPos(
	lines []string, text string, width int,
) (int, int) {
	cursorLine := m.textarea.Line()
	li := m.textarea.LineInfo()
	colRunes := li.ColumnOffset + li.StartColumn

	textLines := strings.Split(text, "\n")
	bytePos := 0
	for i := 0; i < cursorLine && i < len(textLines); i++ {
		bytePos += len(textLines[i]) + 1
	}
	if cursorLine < len(textLines) {
		runes := []rune(textLines[cursorLine])
		if colRunes > len(runes) {
			colRunes = len(runes)
		}
		bytePos += len(string(runes[:colRunes]))
	}
	if bytePos > len(text) {
		bytePos = len(text)
	}

	origPos := 0
	for li, line := range lines {
		lineLen := len(line)
		if origPos+lineLen > bytePos {
			return li, bytePos - origPos
		}
		if origPos+lineLen == bytePos {
			return li, lineLen
		}
		origPos += lineLen
		if origPos < len(text) {
			if text[origPos] == '\n' {
				origPos++
			} else if text[origPos] == ' ' &&
				origPos+1 < len(text) &&
				text[origPos+1] != '\n' {
				origPos++
			}
		}
	}
	return len(lines) - 1, len([]rune(lines[len(lines)-1]))
}

// helpContentHeight returns the number of body lines the help overlay
// currently renders (excluding the fixed title and footer rows), so the
// scroll clamp can use the real content size.
func (m Model) helpContentHeight() int {
	var params []agent.ParamInfo
	if m.configOverrider != nil {
		params = m.configOverrider.OverrideableParams()
	}
	return helpLineCount(helpContent(m.mode, params))
}

// View renders the entire UI.
func (m Model) View() string {
	m.ensureLayout()

	// Help overlay takes full screen when visible.
	if m.showHelp != nil && m.showHelp.Visible() {
		return m.renderHelpOverlay()
	}

	var sections []string

	// Output pane (top, scrollable).
	sections = append(sections, m.viewport.View())

	// New content indicator when follow mode paused.
	if !m.viewport.FollowMode() && m.viewport.NewContent() {
		sections = append(sections,
			m.theme.Spinner.Render("↓ new content"),
		)
	}

	// Command palette overlay.
	if m.commandPalette.Visible() {
		sections = append(sections, m.renderPaletteWithFilter())
	}

	// Which-key popup overlay.
	if m.whichKey.Visible() {
		sections = append(sections, m.whichKey.View())
	}

	// Completion popup.
	if m.showCompletion && len(m.complMatches) > 0 {
		popup := m.renderCompletionPopup()
		sections = append(sections, popup)
	}

	// Permission prompt.
	if m.permission != nil {
		perm := m.renderPermissionPrompt(m.width)
		sections = append(sections, perm)
	}

	// Halt-all confirmation prompt.
	if m.haltPrompt {
		sections = append(sections, m.renderHaltPrompt(m.width))
	}

	// Upper separator.
	sections = append(sections, m.renderSeparator(""))

	// Input line — plain text, no lipgloss styling on typed chars.
	promptChar := m.theme.PromptNormal.Render("❯")
	if m.textarea.Focused() {
		promptChar = m.theme.PromptActive.Render("❯")
	}
	inputText := StripPartialANSI(m.textarea.Value())
	avail := m.width - 2
	if avail < 1 {
		avail = 1
	}
	wrapped := wrapText(inputText, avail)
	m.textarea.SetHeight(m.inputLineHeight())

	lines := strings.Split(wrapped, "\n")

	var promptSection strings.Builder
	if m.textarea.Focused() {
		visLine, visCol := m.cursorVisualPos(lines, inputText, avail)
		for i, line := range lines {
			if i == 0 {
				promptSection.WriteString(promptChar + " ")
			} else {
				promptSection.WriteString("  ")
			}

			if i == visLine {
				r := []rune(line)
				if visCol > len(r) {
					visCol = len(r)
				}
				promptSection.WriteString(string(r[:visCol]))
				promptSection.WriteString("█")
				promptSection.WriteString(string(r[visCol:]))
			} else {
				promptSection.WriteString(line)
			}
			promptSection.WriteString("\n")
		}
	} else {
		for i, line := range lines {
			if i == 0 {
				promptSection.WriteString(promptChar + " " + line)
			} else {
				promptSection.WriteString("  " + line)
			}
			promptSection.WriteString("\n")
		}
	}
	sections = append(sections, promptSection.String())

	// Reverse-i-search overlay.
	if m.showReverseSearch {
		sections = append(sections, m.renderReverseSearch())
	}

	// Lower separator.
	sections = append(sections, m.renderSeparator(""))

	// Tab bar (only when 2+ sessions).
	if m.tabBar.Visible() {
		sections = append(sections, m.renderTabBar())
	}

	// Status line (indented 2 spaces).
	sections = append(sections, "  "+m.renderStatus())

	result := strings.Join(sections, "\n")

	if m.height > 0 {
		lines := strings.Count(result, "\n") + 1
		if pad := m.height - lines; pad > 0 {
			result += strings.Repeat("\n", pad)
		}
	}

	return result
}

// renderCompletionPopup renders the slash-command completion list.
func (m Model) renderCompletionPopup() string {
	var b strings.Builder
	for i, cmd := range m.complMatches {
		prefix := "  "
		if i == m.complIdx {
			prefix = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			prefix,
			m.theme.StatusValue.Render(cmd.Name),
			m.theme.StatusKey.Render(cmd.Description),
		))
	}
	return b.String()
}

// renderSeparator returns a horizontal rule, optionally with a label.
func (m Model) renderSeparator(label string) string {
	width := m.width
	if width <= 0 {
		width = 60
	}
	if label == "" {
		return m.theme.Separator.Render(
			strings.Repeat("─", width),
		)
	}
	prefix := strings.Repeat("─", 3)
	suffixLen := width - len(prefix) - len(label) - 2
	if suffixLen < 0 {
		suffixLen = 0
	}
	suffix := strings.Repeat("─", suffixLen)
	return m.theme.Separator.Render(
		prefix + " " + label + " " + suffix,
	)
}

// outputTexts extracts the text fields from output entries.
func outputTexts(entries []outputEntry) []string {
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.text
	}
	return texts
}

// updateCtxStats refreshes ctxStats from the current session and compressor.
func (m *Model) updateCtxStats() {
	if m.session == nil || m.compressor == nil {
		return
	}
	mode := m.compressor.Config()
	cw := m.modelInfo.ContextWindow
	if cw == 0 {
		cw = mode.FallbackContextWindow
	}
	messages := m.session.Messages()
	estimated := m.compressor.EstimateMessages(messages)
	fallback := m.modelInfo.ContextWindow == 0 && cw == mode.FallbackContextWindow

	offloaded := m.compressor.OffloadCount
	storageBytes := int64(offloaded * 512)

	m.ctxStats = &CtxStats{
		EstimatedTokens:   estimated,
		ContextWindow:     cw,
		Compressions:      m.compressor.CompressionCount,
		Mode:              mode.Mode,
		Fallback:          fallback,
		OffloadedMessages: offloaded,
		StorageUsedBytes:  storageBytes,
	}
}

// renderReverseSearch renders the ctrl+r reverse-i-search overlay.
func (m Model) renderReverseSearch() string {
	var b strings.Builder
	b.WriteString(m.theme.PopupTitle.Render(" Ctrl-R Search"))
	b.WriteString(m.theme.PopupItem.Render(" query: " + m.reverseSearch.Query()))
	b.WriteString("\n")
	matches := m.reverseSearch.Matches()
	if len(matches) > 0 {
		b.WriteString(strings.Repeat("─", 40) + "\n")
		start := 0
		if len(matches) > 5 {
			start = m.reverseSearch.SelectedIndex()
		}
		end := start + 5
		if end > len(matches) {
			end = len(matches)
		}
		for i := start; i < end; i++ {
			prefix := "  "
			if i == m.reverseSearch.SelectedIndex() {
				prefix = "▸ "
			}
			b.WriteString(fmt.Sprintf("%s%s\n", prefix, matches[i]))
		}
		if len(matches) > 5 {
			b.WriteString(fmt.Sprintf("  (%d matches)\n", len(matches)))
		}
	}
	return b.String()
}

// renderTabBar renders the session tab bar with horizontal scrolling.
func (m Model) renderTabBar() string {
	tabs := m.tabBar.Tabs()
	activeIdx := m.tabBar.ActiveIndex()

	type tabRendered struct {
		str   string
		width int
	}
	renders := make([]tabRendered, len(tabs))
	totalW := 0
	for i, t := range tabs {
		label := t.Label
		switch {
		case t.Status == "ok":
			label += " ✓"
		case t.Status == "error":
			label += " ✗"
		}
		var style lipgloss.Style
		if i == activeIdx {
			style = m.theme.TabActive
		} else if t.Spinning {
			style = m.theme.TabSpin
		} else {
			style = m.theme.TabInactive
		}
		s := style.Render(" " + label + " ")
		renders[i] = tabRendered{s, lipgloss.Width(s)}
		totalW += renders[i].width
	}

	if totalW <= m.width {
		var b strings.Builder
		for _, r := range renders {
			b.WriteString(r.str)
		}
		return b.String()
	}

	indW := 1
	contentW := m.width - 2*indW
	if contentW <= 0 {
		return ""
	}

	activeStart := 0
	for i := 0; i < activeIdx; i++ {
		activeStart += renders[i].width
	}
	activeEnd := activeStart + renders[activeIdx].width

	offset := m.tabBar.ScrollOffset()
	if activeEnd > offset+contentW {
		offset = activeEnd - contentW
	}
	if activeStart < offset {
		offset = activeStart
	}
	if maxOff := totalW - contentW; offset > maxOff {
		offset = maxOff
	}
	if offset < 0 {
		offset = 0
	}
	m.tabBar.SetScrollOffset(offset)

	var b strings.Builder
	pos := 0
	for _, r := range renders {
		endPos := pos + r.width
		if endPos <= offset {
			pos = endPos
			continue
		}
		if pos >= offset+contentW {
			break
		}
		if pos >= offset && endPos <= offset+contentW {
			b.WriteString(r.str)
		}
		pos = endPos
	}

	result := b.String()
	if offset > 0 {
		result = m.theme.TabOverflow.Render("◂") + result
	}
	if offset+contentW < totalW {
		result += m.theme.TabOverflow.Render("▸")
	}
	return result
}

// switchSessionTab switches to the tab with the given ID, swapping
// viewport content between parent and child sessions.
func (m *Model) switchSessionTab(id string) {
	activeID := m.tabBar.ActiveID()
	if activeID == id {
		return
	}
	// Save parent output when leaving parent tab.
	isParent := true
	for _, t := range m.tabBar.Tabs() {
		if t.ID == activeID && t.IsSubagent {
			isParent = false
			break
		}
	}
	if activeID == m.session.ID() || isParent {
		m.parentOutput = m.output
	}
	m.tabBar.Activate(id)
	m.loadTabOutput(id)
}

// loadTabOutput renders the active tab's session messages into m.output.
func (m *Model) loadTabOutput(id string) {
	if id == m.session.ID() && m.parentOutput != nil {
		m.output = m.parentOutput
		m.syncViewport()
		return
	}
	sess, ok := m.childSessions[id]
	if !ok {
		return
	}
	msgs := sess.Messages()
	out := make([]outputEntry, 0, len(msgs))
	for _, msg := range msgs {
		kind := "agent"
		switch msg.Role {
		case "user":
			kind = "user"
		case "tool":
			kind = "tool"
		}
		text := msg.Content
		if text == "" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if text != "" {
					text += "\n"
				}
				text += fmt.Sprintf("tool: %s(%s)", tc.Name, tc.Args)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			out = append(out, outputEntry{kind: kind, text: line})
		}
	}
	m.output = out
	m.syncViewport()
}

// nextChildTab activates the next tab to the right.
func (m *Model) nextChildTab() {
	tabs := m.tabBar.Tabs()
	active := m.tabBar.ActiveIndex()
	if active < len(tabs)-1 {
		m.switchSessionTab(tabs[active+1].ID)
	}
}

// prevChildTab activates the previous tab to the left.
func (m *Model) prevChildTab() {
	tabs := m.tabBar.Tabs()
	active := m.tabBar.ActiveIndex()
	if active > 0 {
		m.switchSessionTab(tabs[active-1].ID)
	}
}

// parentTab activates the first non-subagent (parent) tab.
func (m *Model) parentTab() {
	for _, t := range m.tabBar.Tabs() {
		if !t.IsSubagent {
			m.switchSessionTab(t.ID)
			return
		}
	}
}

// firstChildTab activates the first subagent (child) tab.
func (m *Model) firstChildTab() {
	for _, t := range m.tabBar.Tabs() {
		if t.IsSubagent {
			m.switchSessionTab(t.ID)
			return
		}
	}
}

// clearSubagentTabs removes all subagent tabs and resets to parent
// session output.
func (m *Model) clearSubagentTabs() {
	for _, t := range m.tabBar.Tabs() {
		if t.IsSubagent {
			m.tabBar.RemoveTab(t.ID)
			delete(m.childSessions, t.ID)
		}
	}
	m.childSessions = make(map[string]*session.Session)
	if m.parentOutput != nil {
		m.output = m.parentOutput
		m.parentOutput = nil
		m.syncViewport()
	}
}

// resolveReferences finds @-refs in text, resolves them, and returns the
// cleaned text plus any resolved content to inject as context.
func (m *Model) resolveReferences(text string) (string, string) {
	var resolved []string
	remaining := text
	for {
		idx := strings.LastIndex(remaining, "@")
		if idx < 0 {
			break
		}
		// Only match @ at word boundary.
		if idx > 0 && remaining[idx-1] != ' ' {
			remaining = remaining[:idx]
			continue
		}
		query := remaining[idx+1:]
		if query == "" {
			break
		}
		// Find the end of the @ref token.
		end := idx + 1
		for end < len(remaining) && remaining[end] != ' ' {
			end++
		}
		alias := remaining[idx+1 : end]
		content, kind, err := m.referenceEngine.Resolve(alias)
		if err == nil && content != "" {
			resolved = append(resolved,
				fmt.Sprintf("[%s @%s]:\n%s", kind, alias, content),
			)
			remaining = remaining[:idx]
		} else {
			break
		}
	}
	if len(resolved) == 0 {
		return text, ""
	}
	cleaned := strings.TrimSpace(remaining)
	return cleaned, strings.Join(resolved, "\n\n")
}

// renderPaletteWithFilter renders the command palette with inline filter.
func (m Model) renderPaletteWithFilter() string {
	items := m.commandPalette.Filtered()
	var b strings.Builder
	b.WriteString(m.theme.PopupTitle.Render(" Command Palette") + "\n")
	b.WriteString(m.theme.FilterBox.Render(
		" filter: "+m.commandPalette.Filter()+"_",
	) + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n")
	for i, item := range items {
		prefix := "  "
		if i == m.commandPalette.SelectedIndex() {
			prefix = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			prefix,
			m.theme.PopupItem.Render(item.Name),
			m.theme.PopupDesc.Render(item.Description),
		))
	}
	if len(items) == 0 {
		b.WriteString(m.theme.PopupDesc.Render("  (no matches)") + "\n")
	}
	return b.String()
}

// handlePaletteKey routes key events when the command palette is visible.
func (m Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.commandPalette.Dismiss()
		return m, nil
	case tea.KeyCtrlP:
		// Ctrl+P again dismisses the palette.
		m.commandPalette.Dismiss()
		return m, nil
	case tea.KeyEnter:
		m.commandPalette.Execute()
		return m, nil
	case tea.KeyUp:
		m.commandPalette.MoveUp()
		return m, nil
	case tea.KeyDown:
		m.commandPalette.MoveDown()
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		filter := m.commandPalette.Filter()
		if len(filter) > 0 {
			filter = filter[:len(filter)-1]
		}
		m.commandPalette.SetFilter(filter)
		return m, nil
	}
	// Any printable rune → append to filter.
	if msg.Type == tea.KeyRunes {
		filter := m.commandPalette.Filter() + string(msg.Runes)
		m.commandPalette.SetFilter(filter)
		return m, nil
	}
	return m, nil
}

// handleReverseSearchKey routes key events when reverse-i-search is visible.
func (m Model) handleReverseSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.reverseSearch.Dismiss()
		m.showReverseSearch = false
		return m, nil
	case tea.KeyEnter:
		sel := m.reverseSearch.Selected()
		m.reverseSearch.Dismiss()
		m.showReverseSearch = false
		if sel != "" {
			m.textarea.SetValue(sel)
		}
		return m, nil
	case tea.KeyCtrlR:
		// Ctrl+R again cycles forward through matches.
		m.reverseSearch.CycleForward()
		return m, nil
	case tea.KeyCtrlS:
		// Ctrl+S cycles backward through matches.
		m.reverseSearch.CycleReverse()
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		q := m.reverseSearch.Query()
		if len(q) > 0 {
			q = q[:len(q)-1]
		}
		m.reverseSearch.SetQuery(q)
		return m, nil
	}
	// Any printable rune → append to query.
	if msg.Type == tea.KeyRunes {
		for _, r := range msg.Runes {
			m.reverseSearch.AddRune(r)
		}
		return m, nil
	}
	return m, nil
}
