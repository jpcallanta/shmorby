// Package app contains the in-process backend implementation.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"shmorby/internal/agent"
	"shmorby/internal/approval"
	"shmorby/internal/backend"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/execution"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
	"shmorby/internal/xuuid"
)

var (
	ErrSessionNotFound      = errors.New("backend: session not found")
	ErrApprovalUnavailable  = errors.New("backend: approval service unavailable")
	ErrExecutionUnavailable = errors.New("backend: execution manager unavailable")
)

// Config contains the dependencies shared by the TUI and REPL.
type Config struct {
	Provider      llm.Provider
	ProviderRef   *llm.Provider
	ProviderName  string
	Directory     string
	SessionStore  session.Store
	Session       *session.Session
	Registry      *tools.Registry
	Mode          string
	Scope         string
	Model         string
	Override      string
	MaxIterations int
	ShellEnabled  bool
	MemoryStore   memory.Store
	Retriever     *memory.Retriever
	Compressor    *ctxcomp.Compressor
	ModelInfo     llm.ModelInfo
	ToolRules     map[string]*tools.RuleSet
	StatusGen     *agent.StatusGenerator
	LedgerCtx     string
	ProjectRoot   string
	Executions    *execution.Manager
	Approvals     *approval.Service
}

// InProcess implements backend.Backend over the existing agent and stores.
type InProcess struct {
	cfg Config
	hub *backend.Hub

	mu         sync.Mutex
	sessions   map[string]*session.Session
	metadata   map[string]session.Meta
	turnLocks  map[string]*sync.Mutex
	cancellers map[string]context.CancelFunc
}

var _ backend.Backend = (*InProcess)(nil)

// Creates a backend shared by all in-process frontends.
func NewBackend(cfg Config) *InProcess {
	if cfg.MaxIterations < 1 {
		cfg.MaxIterations = 1
	}
	b := &InProcess{
		cfg:        cfg,
		hub:        backend.NewHub(),
		sessions:   make(map[string]*session.Session),
		metadata:   make(map[string]session.Meta),
		turnLocks:  make(map[string]*sync.Mutex),
		cancellers: make(map[string]context.CancelFunc),
	}
	if cfg.Session != nil {
		id := cfg.Session.ID()
		meta := b.defaultSessionMeta(id)
		if cfg.SessionStore != nil {
			if _, stored, err := cfg.SessionStore.Load(id); err == nil {
				meta = stored
			}
		}
		b.sessions[id] = cfg.Session
		b.metadata[id] = meta
	}
	if cfg.Approvals != nil {
		cfg.Approvals.SetRequestObserver(func(record session.ApprovalRecord) {
			_ = b.publish(backend.Event{
				Type:        backend.EventApprovalRequested,
				SessionID:   record.SessionID,
				ExecutionID: record.ExecutionID,
				ApprovalID:  record.ID,
				Data: mustJSON(map[string]string{
					"tool":    record.Tool,
					"command": record.CommandRedacted,
					"reason":  record.Reason,
				}),
			})
		})
	}

	return b
}

// Creates and stores a session.
func (b *InProcess) CreateSession(
	ctx context.Context,
	spec backend.SessionSpec,
) (backend.Session, error) {
	if err := ctx.Err(); err != nil {
		return backend.Session{}, err
	}
	sess := session.New()
	now := time.Now().UTC()
	meta := session.Meta{
		ID:        sess.ID(),
		Directory: spec.Directory,
		AgentMode: spec.AgentMode,
		Provider:  spec.Provider,
		Model:     spec.Model,
		ParentID:  spec.ParentID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if b.cfg.SessionStore != nil {
		if err := b.cfg.SessionStore.Save(meta); err != nil {
			return backend.Session{}, fmt.Errorf("create session: %w", err)
		}
		sess.BindStore(b.cfg.SessionStore, meta)
	}
	b.mu.Lock()
	b.sessions[sess.ID()] = sess
	b.metadata[sess.ID()] = meta
	b.mu.Unlock()
	if err := b.publish(backend.Event{
		Type:      backend.EventSessionCreated,
		SessionID: sess.ID(),
		Data:      mustJSON(meta),
	}); err != nil {
		return backend.Session{}, err
	}

	return backend.Session{Meta: meta, Messages: sess.Messages()}, nil
}

// Lists persisted root sessions or the in-memory session.
func (b *InProcess) ListSessions(
	ctx context.Context,
	filter backend.SessionFilter,
) ([]backend.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.cfg.SessionStore != nil {
		rows, err := b.cfg.SessionStore.List(session.Query{
			Directory:       filter.Directory,
			IncludeArchived: filter.IncludeArchived,
			Limit:           filter.Limit,
		})
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		out := make([]backend.Session, 0, len(rows))
		for _, meta := range rows {
			out = append(out, backend.Session{Meta: meta})
		}
		return out, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]backend.Session, 0, len(b.sessions))
	for id := range b.sessions {
		meta := b.metadata[id]
		if filter.Directory != "" && meta.Directory != filter.Directory {
			continue
		}
		out = append(out, backend.Session{Meta: meta})
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}

	return out, nil
}

// Returns session metadata and its conversation snapshot.
func (b *InProcess) GetSession(
	ctx context.Context,
	id string,
) (backend.Session, error) {
	if err := ctx.Err(); err != nil {
		return backend.Session{}, err
	}
	b.mu.Lock()
	sess := b.sessions[id]
	b.mu.Unlock()
	if sess != nil {
		meta := b.metadataFor(id)
		if b.cfg.SessionStore != nil {
			if _, storedMeta, err := b.cfg.SessionStore.Load(id); err == nil {
				meta = storedMeta
				b.mu.Lock()
				b.metadata[id] = storedMeta
				b.mu.Unlock()
			}
		}
		return backend.Session{Meta: meta, Messages: sess.Messages()}, nil
	}
	if b.cfg.SessionStore == nil {
		return backend.Session{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	sess, meta, err := b.cfg.SessionStore.Load(id)
	if err != nil {
		return backend.Session{}, fmt.Errorf("get session: %w", err)
	}
	return backend.Session{Meta: meta, Messages: sess.Messages()}, nil
}

// Runs one agent turn and emits its lifecycle events.
func (b *InProcess) SubmitInput(
	ctx context.Context,
	sessionID string,
	input backend.Input,
) (backend.InputID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sess, err := b.getLiveSession(sessionID)
	if err != nil {
		return "", err
	}
	if input.ID == "" {
		id, idErr := xuuid.New()
		if idErr != nil {
			return "", fmt.Errorf("generate input ID: %w", idErr)
		}
		input.ID = backend.InputID(id)
	}
	inputID := string(input.ID)

	lock := b.sessionTurnLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	turnCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.cancellers[sessionID] = cancel
	b.mu.Unlock()
	defer func() {
		cancel()
		b.mu.Lock()
		delete(b.cancellers, sessionID)
		b.mu.Unlock()
	}()
	turnCtx = agent.WithInputID(turnCtx, inputID)
	turnCtx = tools.WithSessionID(turnCtx, sessionID)

	if err := b.publish(backend.Event{
		Type:      backend.EventInputAccepted,
		SessionID: sessionID,
		InputID:   inputID,
		Data:      mustJSON(backend.InputEventData{Text: input.Text}),
	}); err != nil {
		return input.ID, err
	}

	mode := input.Mode
	if mode == "" {
		mode = b.cfg.Mode
	}
	model := input.Model
	if model == "" {
		model = b.cfg.Model
	}
	modelInfo := input.ModelInfo
	if modelInfo == (llm.ModelInfo{}) {
		modelInfo = b.cfg.ModelInfo
	}
	onEvent := func(event agent.AgentEvent) {
		b.publishAgentEvent(sessionID, inputID, event)
	}
	onDelta := func(delta string) {
		_ = b.publish(backend.Event{
			Type:      backend.EventTextDelta,
			SessionID: sessionID,
			InputID:   inputID,
			Data:      mustJSON(backend.DeltaEventData{Delta: delta}),
		})
	}
	onReasoning := func(delta string) {
		_ = b.publish(backend.Event{
			Type:      backend.EventReasoningDelta,
			SessionID: sessionID,
			InputID:   inputID,
			Data:      mustJSON(backend.DeltaEventData{Delta: delta}),
		})
	}
	handler := input.PermissionHandler
	if handler != nil {
		input.PermissionHandler = func(
			request backend.PermissionRequest,
		) backend.PermissionDecision {
			decision := handler(request)
			eventType := backend.EventApprovalDecided
			approvalID := ""
			decisionName := "reject"
			if request.Resolution != nil {
				approvalID = request.Resolution.ApprovalID
				if request.Resolution.DecisionSource == "timeout" {
					eventType = backend.EventApprovalExpired
				}
			}
			switch decision {
			case backend.PermissionAllow:
				decisionName = string(backend.DecisionOnce)
			case backend.PermissionAllowAll:
				decisionName = string(backend.DecisionAlways)
			}
			_ = b.publish(backend.Event{
				Type:        eventType,
				SessionID:   sessionID,
				InputID:     inputID,
				ExecutionID: request.ExecutionID,
				ApprovalID:  approvalID,
				Data: mustJSON(map[string]string{
					"decision": decisionName,
				}),
			})
			return decision
		}
	}
	permFunc := agent.AdaptBackendPermissionHandler(input.PermissionHandler)
	var previousCompressions int64
	var messagesBeforeCompression []session.Message
	if b.cfg.Compressor != nil {
		previousCompressions = b.cfg.Compressor.CompressionCount.Load()
		messagesBeforeCompression = sess.Messages()
	}
	var previousHits int
	if b.cfg.Retriever != nil {
		previousHits = b.cfg.Retriever.Stats().Hits
	}

	provider := b.currentProvider()
	runTurn := func() (string, error) {
		if b.cfg.Registry != nil {
			return agent.RunTurnWithTools(
				turnCtx, provider, sess,
				mode, b.cfg.Scope, inputOverride(input, b.cfg.Override),
				model, input.Text, b.cfg.Registry,
				b.cfg.MaxIterations, b.cfg.ShellEnabled,
				b.cfg.MemoryStore, b.cfg.Retriever,
				b.cfg.Compressor, modelInfo,
				onEvent, permFunc, b.cfg.ToolRules,
				b.cfg.StatusGen, b.cfg.LedgerCtx,
				b.cfg.ProjectRoot, input.Images...,
			)
		}

		return agent.RunTurn(
			turnCtx, provider, sess, mode, b.cfg.Scope,
			inputOverride(input, b.cfg.Override), model, input.Text,
			b.cfg.MemoryStore, b.cfg.Retriever,
			b.cfg.Compressor, modelInfo,
			b.cfg.LedgerCtx, b.cfg.ProjectRoot, input.Images...,
		)
	}

	var reply string
	var runErr error
	if input.Stream {
		reply, runErr = agent.RunTurnWithToolsStreamEvents(
			turnCtx, provider, sess,
			mode, b.cfg.Scope, inputOverride(input, b.cfg.Override),
			model, input.Text, b.cfg.Registry,
			b.cfg.MaxIterations, b.cfg.ShellEnabled,
			b.cfg.MemoryStore, b.cfg.Retriever,
			b.cfg.Compressor, modelInfo,
			onEvent, onDelta, onReasoning, permFunc, b.cfg.ToolRules,
			b.cfg.StatusGen, b.cfg.LedgerCtx,
			b.cfg.ProjectRoot, input.Images...,
		)
		if runErr != nil && strings.Contains(
			runErr.Error(), "streaming not",
		) {
			reply, runErr = runTurn()
		}
	} else {
		reply, runErr = runTurn()
	}
	if b.cfg.Compressor != nil &&
		b.cfg.Compressor.CompressionCount.Load() > previousCompressions &&
		messagesWereCompacted(messagesBeforeCompression, sess.Messages()) {
		_ = b.publish(backend.Event{
			Type:      backend.EventContextCompacted,
			SessionID: sessionID,
			InputID:   inputID,
		})
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || turnCtx.Err() != nil {
			_ = b.publish(backend.Event{
				Type:      backend.EventInputCancelled,
				SessionID: sessionID,
				InputID:   inputID,
				Data:      mustJSON(map[string]string{"error": runErr.Error()}),
			})
		}
		return input.ID, runErr
	}

	memoryEntries := 0
	if b.cfg.Retriever != nil && b.cfg.Retriever.Stats().Hits > previousHits {
		memoryEntries = b.cfg.Retriever.Stats().LastCount
	}
	b.mu.Lock()
	meta := b.metadata[sessionID]
	if meta.Title == "" {
		meta.Title = session.DeriveTitle(input.Text)
	}
	meta.AgentMode = mode
	meta.Model = model
	if provider != nil {
		meta.Provider = provider.Name()
	}
	meta.UpdatedAt = time.Now().UTC()
	b.metadata[sessionID] = meta
	b.mu.Unlock()
	_ = b.publish(backend.Event{
		Type:      backend.EventMessageCreated,
		SessionID: sessionID,
		InputID:   inputID,
		Data: mustJSON(backend.MessageEventData{
			Role: "assistant", Content: reply,
			MemoryEntries: memoryEntries,
		}),
	})
	_ = b.publish(backend.Event{
		Type:      backend.EventSessionUpdated,
		SessionID: sessionID,
		InputID:   inputID,
	})

	return input.ID, nil
}

// Cancels the active input for a session.
func (b *InProcess) Interrupt(
	ctx context.Context,
	sessionID string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	cancel := b.cancellers[sessionID]
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	return nil
}

// Forces the configured compressor to process one session.
func (b *InProcess) Compact(
	ctx context.Context,
	sessionID string,
) error {
	if b.cfg.Compressor == nil {
		return errors.New("context compressor unavailable")
	}
	sess, err := b.getLiveSession(sessionID)
	if err != nil {
		return err
	}
	before := b.cfg.Compressor.CompressionCount.Load()
	messagesBefore := sess.Messages()
	if err := b.cfg.Compressor.Compress(ctx, sess, b.cfg.ModelInfo); err != nil {
		return fmt.Errorf("compact session: %w", err)
	}
	if b.cfg.Compressor.CompressionCount.Load() > before &&
		messagesWereCompacted(messagesBefore, sess.Messages()) {
		return b.publish(backend.Event{
			Type:      backend.EventContextCompacted,
			SessionID: sessionID,
		})
	}

	return nil
}

// Lists durable approvals when the store supports it.
func (b *InProcess) ListApprovals(
	ctx context.Context,
	filter backend.ApprovalFilter,
) ([]backend.Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lister, ok := b.cfg.SessionStore.(interface {
		ListApprovals(session.ApprovalQuery) ([]session.ApprovalRecord, error)
	})
	if !ok {
		return nil, nil
	}
	return lister.ListApprovals(session.ApprovalQuery{
		SessionID: filter.SessionID,
		Status:    filter.Status,
		Limit:     filter.Limit,
	})
}

// Resolves an active in-process approval prompt.
func (b *InProcess) DecideApproval(
	ctx context.Context,
	id string,
	decision backend.Decision,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.cfg.Approvals == nil {
		return ErrApprovalUnavailable
	}
	return b.cfg.Approvals.Decide(id, approval.Decision(decision))
}

// Lists durable executions.
func (b *InProcess) ListExecutions(
	ctx context.Context,
	filter backend.ExecutionFilter,
) ([]backend.Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.cfg.Executions == nil {
		return nil, ErrExecutionUnavailable
	}
	return b.cfg.Executions.List(ctx, execution.Filter{
		SessionID: filter.SessionID,
		Statuses:  filter.Statuses,
		Limit:     filter.Limit,
	})
}

// Returns one durable execution.
func (b *InProcess) GetExecution(
	ctx context.Context,
	id string,
) (backend.Execution, error) {
	if err := ctx.Err(); err != nil {
		return backend.Execution{}, err
	}
	if b.cfg.Executions == nil {
		return backend.Execution{}, ErrExecutionUnavailable
	}
	return b.cfg.Executions.Get(ctx, id)
}

// Cancels one active execution.
func (b *InProcess) CancelExecution(
	ctx context.Context,
	id string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.cfg.Executions == nil {
		return ErrExecutionUnavailable
	}
	return b.cfg.Executions.Cancel(id)
}

// Returns one page of persisted execution output.
func (b *InProcess) ExecutionOutput(
	ctx context.Context,
	id, cursor string,
) (backend.OutputChunk, error) {
	if err := ctx.Err(); err != nil {
		return backend.OutputChunk{}, err
	}
	if b.cfg.Executions == nil {
		return backend.OutputChunk{}, ErrExecutionUnavailable
	}
	return b.cfg.Executions.Output(ctx, id, cursor)
}

// Returns a filtered stream of backend events.
func (b *InProcess) Subscribe(
	ctx context.Context,
	filter backend.EventFilter,
) (<-chan backend.Event, error) {
	return b.hub.Subscribe(ctx, filter)
}

// Returns events discarded for slow subscribers.
func (b *InProcess) DroppedEvents() uint64 {
	return b.hub.DroppedEvents()
}

func (b *InProcess) publish(event backend.Event) error {
	return b.hub.Publish(event)
}

func messagesWereCompacted(
	before, after []session.Message,
) bool {
	if len(after) < len(before) {
		return true
	}

	return !reflect.DeepEqual(before, after[:len(before)])
}

func (b *InProcess) metadataFor(id string) session.Meta {
	b.mu.Lock()
	defer b.mu.Unlock()
	if meta, ok := b.metadata[id]; ok {
		return meta
	}
	meta := b.defaultSessionMeta(id)
	b.metadata[id] = meta
	return meta
}

func (b *InProcess) defaultSessionMeta(id string) session.Meta {
	now := time.Now().UTC()
	return session.Meta{
		ID:        id,
		Directory: b.cfg.Directory,
		AgentMode: b.cfg.Mode,
		Provider:  b.cfg.ProviderName,
		Model:     b.cfg.Model,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (b *InProcess) currentProvider() llm.Provider {
	if b.cfg.ProviderRef != nil {
		return *b.cfg.ProviderRef
	}

	return b.cfg.Provider
}

func (b *InProcess) getLiveSession(id string) (*session.Session, error) {
	b.mu.Lock()
	sess := b.sessions[id]
	b.mu.Unlock()
	if sess != nil {
		return sess, nil
	}
	if b.cfg.SessionStore == nil {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	sess, meta, err := b.cfg.SessionStore.Load(id)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.BindStore(b.cfg.SessionStore, meta)
	b.mu.Lock()
	b.sessions[id] = sess
	b.metadata[id] = meta
	b.mu.Unlock()
	return sess, nil
}

func (b *InProcess) sessionTurnLock(id string) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	lock := b.turnLocks[id]
	if lock == nil {
		lock = &sync.Mutex{}
		b.turnLocks[id] = lock
	}
	return lock
}

func (b *InProcess) publishAgentEvent(
	sessionID, inputID string,
	event agent.AgentEvent,
) {
	data := backend.ToolEventData{
		Tool: event.Name, Command: event.Info,
		Status: event.Status, Output: event.Output,
	}
	base := backend.Event{
		SessionID:   sessionID,
		InputID:     inputID,
		ExecutionID: event.ExecutionID,
		ApprovalID:  event.ApprovalID,
		Data:        mustJSON(data),
	}
	switch event.Type {
	case "tool-start":
		base.Type = backend.EventToolStarted
		_ = b.publish(base)
		base.Type = backend.EventExecutionStarted
		_ = b.publish(base)
	case "tool-status":
		base.Type = backend.EventToolProgress
		_ = b.publish(base)
	case "tool-end":
		base.Type = backend.EventExecutionOutput
		_ = b.publish(base)
		base.Type = backend.EventToolEnded
		_ = b.publish(base)
		base.Type = backend.EventExecutionEnded
		_ = b.publish(base)
	case "usage-updated":
		base.Type = backend.EventUsageUpdated
		base.Data = mustJSON(backend.UsageEventData{
			Model:            event.Model,
			PromptTokens:     event.PromptTokens,
			CompletionTokens: event.CompletionTokens,
			TotalTokens:      event.TotalTokens,
		})
		_ = b.publish(base)
	}
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return data
}

func inputOverride(input backend.Input, fallback string) string {
	if input.Override != "" {
		return input.Override
	}

	return fallback
}
