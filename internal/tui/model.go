// Package tui implements the Bubbletea-based terminal UI.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"shmorby/internal/agent"
	"shmorby/internal/approval"
	"shmorby/internal/backend"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/exec"
	"shmorby/internal/execution"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	tuicl "shmorby/internal/tui/clipboard"
	tuicompl "shmorby/internal/tui/completion"
	"shmorby/internal/tui/history"
	"shmorby/internal/tui/navigation"
	"shmorby/internal/tui/palette"
	tuirender "shmorby/internal/tui/render"
	"shmorby/internal/tui/sessiontab"
	"shmorby/internal/tui/spinner"
	"shmorby/internal/tui/styles"
	tuivp "shmorby/internal/tui/viewport"
	"shmorby/internal/util"
	"shmorby/internal/xuuid"
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
type permissionExpiredMsg struct {
	id string
}
type executionCancelMsg struct {
	errs []error
}

// settleMsg signals that the stream settle timer has expired.
type settleMsg struct{}

// spinnerStopMsg stops the spinner after output is rendered.
type spinnerStopMsg struct{}

// agentModeChangedMsg signals that the agent mode was switched.
type agentModeChangedMsg struct {
	mode string
}

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
type backendEventMsg struct {
	event backend.Event
}

// subagentEventMsg notifies the TUI about subagent lifecycle events.
type subagentEventMsg struct {
	event tools.SubagentEvent
}

// outputEntry is a single line in the scrollable output pane.
type outputEntry struct {
	kind string // "user", "agent", "tool", "error", "image", "memory"
	text string
}

// pendingImage is an image attached to the next user message.
// Dimensions are captured at attach time for the chip display.
type pendingImage struct {
	data   llm.ImageData
	width  int
	height int
}

// maxImageBytes caps a single attached image at 20 MiB, matching
// OpenAI's per-image upload limit.
const maxImageBytes = 20 << 20

// maxListedModels caps how many models bare /model appends to the
// output: syncViewport re-wraps every entry on each render, so an
// unbounded catalog would slow every keystroke. Longer lists end
// with an explicit "… and N more" line.
const maxListedModels = 50

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
	spinner    spinner.Model
	startTime  time.Time
	tokensDown int

	// State
	running    bool
	fullscreen bool
	width      int
	height     int

	// Agent
	backend       backend.Backend
	backendEvents <-chan backend.Event
	activeInputID string
	provider      llm.Provider
	session       *session.Session
	mode          string
	scope         string
	model         string
	override      string
	scopeInfo     ScopeInfo

	// Theme
	theme styles.Theme

	// Context cancel
	cancel context.CancelFunc

	// Durable execution manager (nil = persistence disabled).
	executions *execution.Manager

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

	// Pending image attachments (clipboard paste)
	pendingImages []pendingImage

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

	// Navigation components
	modeSwitcher      *navigation.ModeSwitcher
	referenceEngine   *navigation.ReferenceEngine
	shellCmdHandler   *navigation.ShellCmdHandler
	scrollAccel       *navigation.ScrollAcceleration
	commandPalette    *palette.CommandPalette
	inputHistory      *history.History
	reverseSearch     *history.ReverseISearch
	tabBar            *sessiontab.TabBar
	showReverseSearch bool

	// Logging
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

	// Subagent event channel (child session lifecycle from orchestrator).
	subagentEventChan chan tools.SubagentEvent

	// Help overlay
	showHelp *HelpModel

	// Interactive permission prompts
	permissionReqChan    chan PermissionPrompt
	permissionExpireChan chan string
	approvals            *approval.Service
	toolRules            map[string]*tools.RuleSet

	// Runtime config overrides.
	configOverrider *agent.ConfigOverrider

	// Child subagent sessions keyed by session ID.
	childSessions map[string]*session.Session

	// Subagent orchestrator for permission propagation.
	orchestrator *tools.TaskOrchestrator

	// Backup of parent output entries for tab switching.
	parentOutput []outputEntry

	// Async status description generator.
	statusGen *agent.StatusGenerator

	// Pre-formatted ledger context for injection.
	ledgerCtx string

	// Resolved project directory for the env hint (empty = default).
	projectRoot string
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
	Backend        backend.Backend
	BackendEvents  <-chan backend.Event
	Provider       llm.Provider
	Session        *session.Session
	Mode           string
	Scope          string
	Model          string
	Override       string
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

	// Navigation config
	Shell       string
	PermLevel   string
	HistorySize int

	// ToolTimeout is the default timeout in seconds for tool calls.
	ToolTimeout int

	// Logging config
	LogEnabled           bool
	LogDefaultLevel      string
	LogMaxEntries        int
	LogDisplayLimit      int
	LogCollapse          bool
	LogCollapseThreshold int
	LogChan              chan LogEntry
	LogHandler           *TUILogHandler

	// Per-tool permission rule sets
	ToolRules map[string]*tools.RuleSet

	// Runtime config overrides.
	ConfigOverrider *agent.ConfigOverrider

	// Subagent lifecycle events channel (nil = no TUI tracking).
	SubagentEventChan chan tools.SubagentEvent

	// Subagent orchestrator for permission propagation.
	Orchestrator *tools.TaskOrchestrator

	// Durable execution manager (nil = no execution tracking).
	Executions *execution.Manager

	// Shared durable approval service.
	Approvals *approval.Service

	// Async status description generator (nil = random text).
	StatusGen *agent.StatusGenerator

	// Pre-formatted ledger context for injection.
	// Empty string disables ledger context injection.
	LedgerCtx string

	// ProjectRoot is the resolved project directory for the
	// environment hint in the system prompt (empty = default).
	ProjectRoot string
}

// Creates a Bubbletea model ready to run.
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

	ms := navigation.NewModeSwitcher()
	mode := cfg.Mode

	if mode == "" {
		mode = "operate"
	}

	ms.SetCurrent(mode)

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
		Name: "executions", Slash: "/executions",
		Description: "List durable executions",
	})
	cp.AddItem(palette.CommandItem{
		Name: "cancel", Slash: "/cancel",
		Description: "Cancel an execution by id",
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
		backend:         cfg.Backend,
		backendEvents:   cfg.BackendEvents,
		textarea:        ta,
		viewport:        vp,
		theme:           theme,
		provider:        cfg.Provider,
		session:         cfg.Session,
		mode:            mode,
		scope:           cfg.Scope,
		model:           cfg.Model,
		override:        cfg.Override,
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
			exec.OSExecutor{}, cfg.Shell, mode, cfg.PermLevel,
		),
		scrollAccel:          navigation.NewScrollAcceleration(),
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
		subagentEventChan:    cfg.SubagentEventChan,
		orchestrator:         cfg.Orchestrator,
		executions:           cfg.Executions,
		permissionReqChan:    make(chan PermissionPrompt),
		permissionExpireChan: make(chan string, 1),
		approvals:            cfg.Approvals,
		toolRules:            cfg.ToolRules,
		showHelp:             NewHelpModel(),
		configOverrider:      cfg.ConfigOverrider,
		childSessions:        make(map[string]*session.Session),
		statusGen:            cfg.StatusGen,
		ledgerCtx:            cfg.LedgerCtx,
		projectRoot:          cfg.ProjectRoot,
	}
}

// Keeps m.mode, modeSwitcher, and shell guard aligned as single
// source of truth. Returns false if mode is unknown.
func (m *Model) setMode(mode string) bool {
	if !m.modeSwitcher.SetCurrent(mode) {
		return false
	}

	m.mode = mode
	m.shellCmdHandler.SetMode(mode)
	m.syncViewport()

	return true
}

// Returns the initial command (no-op) plus a log listener when
// the log channel is configured.
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tea.HideCursor)
	if m.logChan != nil {
		cmds = append(cmds, m.listenLogChan())
	}
	if m.backendEvents != nil {
		cmds = append(cmds, m.listenBackendEvents())
	}
	if m.subagentEventChan != nil {
		cmds = append(cmds, m.listenSubagentEvents())
	}
	cmds = append(cmds, m.listenPermissionReqs())
	cmds = append(cmds, m.listenPermissionExpirations())
	return tea.Batch(cmds...)
}

// Reads from the log channel and returns entries as
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

// Reads backend events and returns one as a bubbletea message.
func (m Model) listenBackendEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.backendEvents
		if !ok {
			return nil
		}
		return backendEventMsg{event: ev}
	}
}

func updateModel(model Model, msg tea.Msg) (Model, tea.Cmd) {
	updated, cmd := model.Update(msg)
	if next, ok := updated.(Model); ok {
		return next, cmd
	}

	return model, cmd
}

// Reads from the subagent event channel and
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

// Reads from the permission request channel and
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

// Reads a timeout event for the active approval prompt.
func (m Model) listenPermissionExpirations() tea.Cmd {
	return func() tea.Msg {
		id, ok := <-m.permissionExpireChan
		if !ok {
			return nil
		}
		return permissionExpiredMsg{id: id}
	}
}

// Handles incoming messages and key events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		wasNew := m.viewport.NewContent()
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		m.textarea.SetHeight(m.inputLineHeight())
		m.syncViewport()
		if !wasNew {
			m.viewport.SetNewContent(false)
			m.ensureLayout()
		}

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
		// Legacy async path retained for external senders;
		// preferred path is synchronous via setMode with
		// immediate feedback and viewport sync.
		m.setMode(msg.mode)
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("Switched to %s mode.", msg.mode),
		})
		m.syncViewport()

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

	case permissionExpiredMsg:
		if m.permission != nil && m.permission.ApprovalID == msg.id {
			m.permission = nil
		}
		return m, m.listenPermissionExpirations()

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

	case backendEventMsg:
		event := msg.event
		if event.InputID != "" && m.activeInputID != "" &&
			event.InputID != m.activeInputID {
			return m, m.listenBackendEvents()
		}
		var follow tea.Cmd
		switch event.Type {
		case backend.EventReasoningDelta:
			var data backend.DeltaEventData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				m.thinking.AddDelta(data.Delta)
				m.syncViewport()
			}
		case backend.EventTextDelta:
			var data backend.DeltaEventData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				if m.thinking.Active() {
					m.thinking.End()
				}
				m, follow = updateModel(m, streamDeltaMsg{delta: data.Delta})
			}
		case backend.EventMessageCreated:
			var data backend.MessageEventData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				m.activeInputID = ""
				m, follow = updateModel(m, errorMsg{err: err})
			} else {
				m.activeInputID = ""
				if m.streamBuf.Tokens() > 0 {
					updated, cmd := m.finalizeStream()
					if next, ok := updated.(Model); ok {
						m = next
					}
					if data.MemoryEntries > 0 {
						m.output = append(m.output, outputEntry{
							kind: "memory",
							text: fmt.Sprintf(
								"[memory: %d entries]",
								data.MemoryEntries,
							),
						})
						m.syncViewport()
					}
					follow = cmd
				} else {
					if m.thinking.Active() {
						m.thinking.End()
					}
					m, follow = updateModel(m, agentReplyMsg{
						text:          data.Content,
						memoryEntries: data.MemoryEntries,
					})
				}
			}
		case backend.EventInputCancelled:
			var data struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(event.Data, &data)
			m.activeInputID = ""
			follow = func() tea.Msg {
				return errorMsg{err: errors.New(data.Error)}
			}
		case backend.EventToolStarted, backend.EventToolProgress,
			backend.EventToolEnded:
			var data backend.ToolEventData
			if err := json.Unmarshal(event.Data, &data); err == nil {
				agentType := "tool-start"
				if event.Type == backend.EventToolProgress {
					agentType = "tool-status"
				} else if event.Type == backend.EventToolEnded {
					agentType = "tool-end"
				}
				m, _ = updateModel(m, agentEventMsg{event: agent.AgentEvent{
					Type: agentType, Name: data.Tool,
					Info: data.Command, Status: data.Status,
					Output: data.Output,
				}})
			}
		}
		return m, tea.Batch(follow, m.listenBackendEvents())

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
		return m, nil

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

	case executionCancelMsg:
		for _, err := range msg.errs {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "cancel: " + err.Error(),
			})
		}
		if len(msg.errs) > 0 {
			m.syncViewport()
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

// Processes keyboard input.
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
			if m.executions != nil {
				mgr := m.executions

				return m, func() tea.Msg {
					return executionCancelMsg{errs: mgr.CancelAll()}
				}
			}

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
		// Image paste first; falls back to text paste so one
		// keybind serves both flows.
		if data, ok := tuicl.PasteImage(); ok {
			m.attachImage(data)
			return m, nil
		}
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

	// Selection mode active: navigation and exit.
	if m.selectionMode {
		switch msg.String() {
		case "g":
			m.viewport.GotoTop()

			return m, nil

		case "G":
			m.viewport.GotoBottom()

			return m, nil

		case "j", "down":
			m.viewport.ScrollDown(1)

			return m, nil

		case "k", "up":
			m.viewport.ScrollUp(1)

			return m, nil

		case "d", "ctrl+d":
			m.viewport.ScrollHalfPageDown()

			return m, nil

		case "u", "ctrl+u":
			m.viewport.ScrollHalfPageUp()

			return m, nil
		}

		switch msg.Type {
		case tea.KeyEsc, tea.KeyEnter:
			m.selectionMode = false
			m.viewport.SetSelectionMode(false)
			m.textarea.Focus()
			m.selectionStart = 0
			m.selectionEnd = 0

			return m, nil

		case tea.KeyHome:
			m.viewport.GotoTop()

			return m, nil

		case tea.KeyEnd:
			m.viewport.GotoBottom()

			return m, nil

		case tea.KeyPgUp:
			m.viewport.ScrollHalfPageUp()

			return m, nil

		case tea.KeyPgDown:
			m.viewport.ScrollHalfPageDown()

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
	case tea.KeyCtrlZ:
		return m, tea.Suspend

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
		// Empty input → cycle agent mode forward.
		if m.textarea.Value() == "" && !m.running {
			m.modeSwitcher.CycleForward()

			m.setMode(m.modeSwitcher.Current())
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: fmt.Sprintf(
					"Switched to %s mode.",
					m.modeSwitcher.Current(),
				),
			})
			m.syncViewport()

			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyShiftTab:
		// Shift+Tab → cycle agent mode reverse.
		if m.textarea.Value() == "" && !m.running {
			m.modeSwitcher.CycleReverse()

			m.setMode(m.modeSwitcher.Current())
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: fmt.Sprintf(
					"Switched to %s mode.",
					m.modeSwitcher.Current(),
				),
			})
			m.syncViewport()

			return m, nil
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
		if m.commandPalette.Visible() {
			m.commandPalette.Dismiss()
			return m, nil
		}
		if m.reverseSearch.Visible() {
			m.reverseSearch.Dismiss()
			m.showReverseSearch = false
			return m, nil
		}
		// Pending attachment clear: only when nothing else wants esc
		// and the input is untouched.
		if len(m.pendingImages) > 0 && m.textarea.Value() == "" {
			m.pendingImages = nil
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
		// Delegate to textarea, then check for completion trigger.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCompletion()
		return m, cmd
	}
}

// Checks the current input and updates completion state.
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

// Processes a user message by running the agent turn. !-prefixed
// shell commands run outside the agent loop.
func (m Model) handleSubmit(text string) (tea.Model, tea.Cmd) {
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

	var images []llm.ImageData
	if len(m.pendingImages) > 0 {
		if !m.visionSupported() {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: fmt.Sprintf(
					"model %q does not support image input — "+
						"switch with /model <name>",
					m.model,
				),
			})
			m.syncViewport()

			return m, nil
		}
		images = m.pendingImageData()
		chip := pendingChipText(m.pendingImages...)
		m.pendingImages = nil
		m.output = append(m.output, outputEntry{
			kind: "image",
			text: chip,
		})
		m.syncViewport()
	}

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
	if id, err := xuuid.New(); err == nil {
		ctx = agent.WithInputID(ctx, id)
		m.activeInputID = id
	} else {
		slog.Warn("generate input id failed", "err", err)
	}
	if m.executions != nil {
		ctx = execution.WithManager(ctx, m.executions)
	}

	return m, tea.Batch(
		m.runAgentTurn(ctx, text, images),
		func() tea.Msg { return spinnerTickMsg{} },
		tea.HideCursor,
	)
}

// Validates and stages a clipboard image for the next message.
// Oversized images are rejected with an inline error.
func (m *Model) attachImage(data []byte) {
	if len(data) > maxImageBytes {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf(
				"image too large (%s > %s limit)",
				humanBytes(len(data)), humanBytes(maxImageBytes),
			),
		})
		m.syncViewport()

		return
	}
	media, _ := util.SniffImageType(data)
	w, h := imageDims(data)
	m.pendingImages = append(m.pendingImages, pendingImage{
		data:   llm.ImageData{MediaType: media, Data: data},
		width:  w,
		height: h,
	})

}

// Reports whether the active model accepts image input. Model info
// is re-resolved (cache-backed) so late config overrides and the
// vision heuristic apply even when startup fetch fell back.
func (m Model) visionSupported() bool {
	if m.provider != nil && m.configOverrider != nil {
		if fetcher, ok := m.provider.(llm.ModelInfoFetcher); ok {
			info, _ := llm.FetchModelInfo(
				context.Background(),
				fetcher, m.model, m.configOverrider.Config(),
			)

			return info.SupportsVision
		}
	}

	return m.modelInfo.SupportsVision
}

// Converts pending attachments to provider image data.
func (m Model) pendingImageData() []llm.ImageData {
	images := make([]llm.ImageData, 0, len(m.pendingImages))
	for _, p := range m.pendingImages {
		images = append(images, p.data)
	}

	return images
}

// Renders the attachment summary shown above the input while images
// are staged.
func pendingChipText(images ...pendingImage) string {
	if len(images) == 1 {
		p := images[0]
		media := strings.TrimPrefix(p.data.MediaType, "image/")
		return fmt.Sprintf(
			"[📎 image %dx%d %s %s — ⚠ sent to provider (esc removes)]",
			p.width, p.height, strings.ToUpper(media),
			humanBytes(len(p.data.Data)),
		)
	}

	return fmt.Sprintf(
		"[📎 %d images — ⚠ sent to provider (esc removes)]",
		len(images),
	)
}

// Reads pixel dimensions from PNG/JPEG headers; 0s when the header
// cannot be parsed.
func imageDims(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}

	return cfg.Width, cfg.Height
}

// Formats a byte count for display, e.g. 421888 → "412.0 KiB".
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// Renders the pending attachment chip line above the input.
func (m Model) renderPendingChip() string {
	width := m.width - 2
	if width < 1 {
		width = 1
	}
	line := pendingChipText(m.pendingImages...)
	if lipgloss.Width(line) > width {
		line = lipgloss.NewStyle().MaxWidth(width).Render(line)
	}

	return m.theme.StatusKey.Render(line)
}

// Prints executions from the session database. An optional status or
// "all" argument filters the query.
func (m *Model) handleExecutionsCommand(parts []string) {
	if m.executions == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "execution tracking is disabled",
		})
		m.syncViewport()

		return
	}

	filter := execution.Filter{Limit: 20}
	if m.session != nil {
		filter.SessionID = m.session.ID()
	}

	for _, arg := range parts[1:] {
		if arg == "all" {
			filter.SessionID = ""

			continue
		}
		status, ok := execution.ParseStatus(arg)
		if !ok {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "usage: /executions [all|status]",
			})
			m.syncViewport()

			return
		}
		filter.Statuses = []execution.Status{status}
	}

	rows, err := m.executions.List(context.Background(), filter)
	if err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "executions: " + err.Error(),
		})
		m.syncViewport()

		return
	}

	if len(rows) == 0 {
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: "No executions found.",
		})
		m.syncViewport()

		return
	}

	var b strings.Builder
	b.WriteString("executions (sessions.db):\n")
	for _, r := range rows {
		age := time.Since(r.CreatedAt).Round(time.Second)
		fmt.Fprintf(&b, "  %s  %-16s %-10s %-8s %s\n",
			r.ID, r.Status, r.Tool, age, r.Command)
		if r.Error != "" {
			fmt.Fprintf(&b, "    %s\n", r.Error)
		}
	}

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}

// Processes slash commands. Returns (handled, shouldQuit, error).
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
		if lister, ok := m.provider.(llm.ModelLister); ok {
			ctx, cancel := context.WithTimeout(
				context.Background(), 5*time.Second,
			)
			models, err := lister.ListModels(ctx, m.model)
			cancel()
			if err != nil {
				slog.Debug("list models failed", "err", err)
			}
			// Cap the list: syncViewport re-wraps every output
			// entry on each render, so growth must stay bounded.
			shown := models
			if len(shown) > maxListedModels {
				shown = shown[:maxListedModels]
			}
			for _, mod := range shown {
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: "  " + mod.FormatSummary(),
				})
			}
			if hidden := len(models) - len(shown); hidden > 0 {
				extra := fmt.Sprintf("  … and %d more", hidden)
				m.output = append(m.output, outputEntry{
					kind: "agent",
					text: extra,
				})
			}
		}
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
			if m.setMode(parts[1]) {
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

	case "/cancel":
		if len(parts) < 2 {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "usage: /cancel <execution-id>",
			})
			m.syncViewport()

			return true, false, nil
		}
		if m.executions == nil {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "execution tracking is disabled",
			})
			m.syncViewport()

			return true, false, nil
		}
		if err := m.executions.Cancel(parts[1]); err != nil {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "cancel: " + err.Error(),
			})
		} else {
			m.output = append(m.output, outputEntry{
				kind: "agent",
				text: "Cancelled execution " + parts[1] + ".",
			})
		}
		m.syncViewport()

		return true, false, nil

	case "/executions":
		m.handleExecutionsCommand(parts)
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

// Submits an input through the shared backend.
func (m Model) runAgentTurn(
	ctx context.Context, input string, images []llm.ImageData,
) tea.Cmd {
	return func() tea.Msg {
		if m.backend == nil || m.session == nil {
			return errorMsg{err: errors.New("backend unavailable")}
		}
		if m.orchestrator != nil && m.toolRules != nil {
			m.orchestrator.PermFunc = m.toolPermissionFunc
		}
		_, err := m.backend.SubmitInput(ctx, m.session.ID(), backend.Input{
			ID:        backend.InputID(agent.InputIDFrom(ctx)),
			Text:      input,
			Images:    images,
			ModelInfo: m.modelInfo,
			// Keep the existing non-streaming TUI turn behavior.
			Stream:            false,
			Mode:              m.mode,
			Model:             m.model,
			Override:          m.override,
			PermissionHandler: m.backendPermissionHandler(),
		})
		if err != nil {
			return errorMsg{err: err}
		}

		return nil
	}
}

func (m Model) backendPermissionHandler() backend.PermissionHandler {
	if m.toolRules == nil {
		return nil
	}

	return agent.AdaptPermissionHandler(m.toolPermissionFunc)
}

// Reads SSE events and converts them to tea.Msg values.
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

// Implements agent.ToolPermissionFunc for the TUI.
func (m *Model) toolPermissionFunc(
	request agent.ToolPermissionRequest,
) agent.ToolPermissionResponse {
	service := m.approvals
	if service == nil {
		service = approval.NewService(nil, 0)
	}
	result := service.Request(
		request.Context,
		session.ApprovalRecord{
			SessionID:       request.SessionID,
			ExecutionID:     request.ExecutionID,
			Tool:            request.Tool,
			CommandRedacted: request.CommandRedacted,
			RulePattern:     request.RulePattern,
			RuleAction:      request.RuleAction,
			Reason:          request.Reason,
		},
		func(record session.ApprovalRecord) approval.Prompt {
			prompt := NewPermissionPrompt(
				request.Tool, request.Command, request.Reason,
				request.RulePattern,
			)
			prompt.ApprovalID = record.ID
			m.permissionReqChan <- prompt

			return approval.Prompt{
				Decisions: prompt.Choice,
				OnExpire: func() {
					select {
					case m.permissionExpireChan <- record.ID:
					default:
					}
				},
				OnDecision: func(approval.Decision) {
					select {
					case m.permissionExpireChan <- record.ID:
					default:
					}
				},
			}
		},
	)
	if request.Resolution != nil {
		request.Resolution.ApprovalID = result.ApprovalID
		request.Resolution.DecidedBy = result.DecidedBy
		request.Resolution.DecisionSource = result.DecisionSource
		request.Resolution.Scope = result.Scope
	}

	switch result.Decision {
	case approval.DecisionAlways:
		return agent.PermAllowAll
	case approval.DecisionOnce:
		return agent.PermAllow
	default:
		return agent.PermDeny
	}
}

// Returns the system prompt for streaming requests.
func (m Model) buildSystemPrompt() string {
	if m.override != "" {
		return m.override
	}
	return fmt.Sprintf(
		"You are in %s mode. %s",
		m.mode, m.scope,
	)
}

// Renders the final accumulated content when streaming ends.
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

// Processes the settle timer expiry and finalizes the stream.
func (m Model) handleSettle() (tea.Model, tea.Cmd) {
	m.settleTimer = nil
	return m.finalizeStream()
}

// Updates the viewport with current output.
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
		case "image":
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

// Calculates the total height of non-viewport fixed chrome.
func (m Model) fixedHeight() int {
	inputHeight := m.inputLineHeight()
	// upper separator + lower separator + status bar.
	statusHeight := 3

	// Pending image chip line above the input.
	if len(m.pendingImages) > 0 {
		statusHeight++
	}

	if m.tabBar.Visible() {
		statusHeight++
	}

	// New content indicator pushes status bar down by 1.
	if !m.viewport.FollowMode() && m.viewport.NewContent() {
		statusHeight++
	}

	return inputHeight + statusHeight
}

// Calculates available item row count for an overlay given its chrome rows.
func (m Model) itemBudget(headerRows, footerRows int) int {
	budget := m.height - m.fixedHeight() - headerRows - footerRows - 1
	if budget < 1 {
		budget = 1
	}

	return budget
}

// Recalculates viewport height from cached dimensions.
func (m *Model) ensureLayout() {
	if m.height <= 0 {
		return
	}

	vpHeight := m.height - m.fixedHeight() - m.overlayHeight()
	if vpHeight < 0 {
		vpHeight = 0
	}

	m.viewport.SetHeight(vpHeight)
}

// Calculates total height of active overlay popups.
func (m Model) overlayHeight() int {
	h := 0

	if m.commandPalette.Visible() {
		pal := strings.TrimSuffix(m.renderPaletteWithFilter(), "\n")
		h += strings.Count(pal, "\n") + 1
	}

	if m.showCompletion && len(m.complMatches) > 0 {
		compl := strings.TrimSuffix(m.renderCompletionPopup(), "\n")
		h += strings.Count(compl, "\n") + 1
	}

	if m.permission != nil {
		perm := strings.TrimSuffix(
			m.renderPermissionPrompt(m.width),
			"\n",
		)
		h += strings.Count(perm, "\n") + 1
	}

	if m.haltPrompt {
		halt := strings.TrimSuffix(m.renderHaltPrompt(m.width), "\n")
		h += strings.Count(halt, "\n") + 1
	}

	if m.showReverseSearch {
		rev := strings.TrimSuffix(m.renderReverseSearch(), "\n")
		h += strings.Count(rev, "\n") + 1
	}

	return h
}

// Returns the number of visual lines the input prompt occupies.
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

// Returns the visual line and column of the cursor
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

// Returns the number of body lines the help overlay
// currently renders (excluding the fixed title and footer rows), so the
// scroll clamp can use the real content size.
func (m Model) helpContentHeight() int {
	var params []agent.ParamInfo
	if m.configOverrider != nil {
		params = m.configOverrider.OverrideableParams()
	}
	return helpLineCount(helpContent(m.mode, params))
}

// Renders the entire UI.
func (m Model) View() string {
	m.ensureLayout()

	// Help overlay takes full screen when visible.
	if m.showHelp != nil && m.showHelp.Visible() {
		return m.renderHelpOverlay()
	}

	var sections []string

	// Output pane (top, scrollable).
	if m.viewport.Height() > 0 {
		sections = append(sections, m.viewport.View())
	}

	// New content indicator when follow mode paused.
	if !m.viewport.FollowMode() && m.viewport.NewContent() {
		sections = append(sections,
			m.theme.Spinner.Render("↓ new content"),
		)
	}

	// Command palette overlay.
	if m.commandPalette.Visible() {
		sections = append(
			sections,
			strings.TrimSuffix(m.renderPaletteWithFilter(), "\n"),
		)
	}

	// Completion popup.
	if m.showCompletion && len(m.complMatches) > 0 {
		popup := strings.TrimSuffix(m.renderCompletionPopup(), "\n")
		sections = append(sections, popup)
	}

	// Permission prompt.
	if m.permission != nil {
		perm := strings.TrimSuffix(
			m.renderPermissionPrompt(m.width),
			"\n",
		)
		sections = append(sections, perm)
	}

	// Halt-all confirmation prompt.
	if m.haltPrompt {
		sections = append(
			sections,
			strings.TrimSuffix(m.renderHaltPrompt(m.width), "\n"),
		)
	}

	// Upper separator.
	sections = append(sections, m.renderSeparator(""))

	// Pending image chip above the input.
	if len(m.pendingImages) > 0 {
		sections = append(sections, m.renderPendingChip())
	}

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
	sections = append(
		sections,
		strings.TrimRight(promptSection.String(), "\n"),
	)

	// Reverse-i-search overlay.
	if m.showReverseSearch {
		sections = append(
			sections,
			strings.TrimSuffix(m.renderReverseSearch(), "\n"),
		)
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

// Renders the slash-command completion list.
func (m Model) renderCompletionPopup() string {
	maxItems := len(m.complMatches)
	budget := m.itemBudget(0, 0)
	if budget < maxItems {
		maxItems = budget
	}

	start := 0
	if len(m.complMatches) > maxItems {
		start = m.complIdx - maxItems/2
		if start+maxItems > len(m.complMatches) {
			start = len(m.complMatches) - maxItems
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxItems
	if end > len(m.complMatches) {
		end = len(m.complMatches)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		cmd := m.complMatches[i]
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

	return strings.TrimSuffix(b.String(), "\n")
}

// Returns a horizontal rule, optionally with a label.
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

// Extracts the text fields from output entries.
func outputTexts(entries []outputEntry) []string {
	texts := make([]string, len(entries))
	for i, e := range entries {
		texts[i] = e.text
	}
	return texts
}

// Refreshes ctxStats from the current session and compressor.
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

	offloaded := int(m.compressor.OffloadCount.Load())
	storageBytes := int64(offloaded * 512)

	m.ctxStats = &CtxStats{
		EstimatedTokens:   estimated,
		ContextWindow:     cw,
		Compressions:      int(m.compressor.CompressionCount.Load()),
		Mode:              mode.Mode,
		Fallback:          fallback,
		OffloadedMessages: offloaded,
		StorageUsedBytes:  storageBytes,
	}
}

// Renders the ctrl+r reverse-i-search overlay.
func (m Model) renderReverseSearch() string {
	var b strings.Builder
	b.WriteString(m.theme.PopupTitle.Render(" Ctrl-R Search"))
	b.WriteString(m.theme.PopupItem.Render(" query: " + m.reverseSearch.Query()))
	b.WriteString("\n")
	matches := m.reverseSearch.Matches()
	if len(matches) > 0 {
		b.WriteString(strings.Repeat("─", 40) + "\n")
		maxMatches := 5
		budget := m.itemBudget(3, 0)
		if budget < maxMatches {
			maxMatches = budget
		}
		start := 0
		if len(matches) > maxMatches {
			start = m.reverseSearch.SelectedIndex() - maxMatches/2
			if start+maxMatches > len(matches) {
				start = len(matches) - maxMatches
			}
			if start < 0 {
				start = 0
			}
		}
		end := start + maxMatches
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
		if len(matches) > maxMatches {
			b.WriteString(fmt.Sprintf("  (%d matches)\n", len(matches)))
		}
	}

	return strings.TrimSuffix(b.String(), "\n")
}

// Renders the session tab bar with horizontal scrolling.
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

// Switches to the tab with the given ID, swapping
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

// Renders the active tab's session messages into m.output.
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

// Activates the next tab to the right.
func (m *Model) nextChildTab() {
	tabs := m.tabBar.Tabs()
	active := m.tabBar.ActiveIndex()
	if active < len(tabs)-1 {
		m.switchSessionTab(tabs[active+1].ID)
	}
}

// Activates the previous tab to the left.
func (m *Model) prevChildTab() {
	tabs := m.tabBar.Tabs()
	active := m.tabBar.ActiveIndex()
	if active > 0 {
		m.switchSessionTab(tabs[active-1].ID)
	}
}

// Activates the first non-subagent (parent) tab.
func (m *Model) parentTab() {
	for _, t := range m.tabBar.Tabs() {
		if !t.IsSubagent {
			m.switchSessionTab(t.ID)
			return
		}
	}
}

// Activates the first subagent (child) tab.
func (m *Model) firstChildTab() {
	for _, t := range m.tabBar.Tabs() {
		if t.IsSubagent {
			m.switchSessionTab(t.ID)
			return
		}
	}
}

// Removes all subagent tabs and resets to parent
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

// Finds @-refs in text, resolves them, and returns the
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

// Renders the command palette with inline filter.
func (m Model) renderPaletteWithFilter() string {
	items := m.commandPalette.Filtered()
	var b strings.Builder
	b.WriteString(m.theme.PopupTitle.Render(" Command Palette") + "\n")
	b.WriteString(m.theme.FilterBox.Render(
		" filter: "+m.commandPalette.Filter()+"_",
	) + "\n")
	b.WriteString(strings.Repeat("─", 40) + "\n")

	maxItems := len(items)
	budget := m.itemBudget(3, 0)
	if budget < maxItems {
		maxItems = budget
	}

	start := 0
	sel := m.commandPalette.SelectedIndex()
	if len(items) > maxItems {
		start = sel - maxItems/2
		if start+maxItems > len(items) {
			start = len(items) - maxItems
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxItems
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		item := items[i]
		prefix := "  "
		if i == sel {
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

	return strings.TrimSuffix(b.String(), "\n")
}

// Routes key events when the command palette is visible.
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
		slash := m.commandPalette.Execute()
		if slash == "" {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "No command selected.",
			})
			m.syncViewport()
			return m, nil
		}
		// Dispatch through the shared submit path so the command
		// behaves identically to typing it.
		return m, func() tea.Msg { return submitMsg{text: slash} }
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

// Routes key events when reverse-i-search is visible.
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
