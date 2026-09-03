package session

import (
	"fmt"
	"log/slog"
	"sync"

	"shmorby/internal/llm"
	"shmorby/internal/xuuid"
)

// Message represents a single message in the conversation.
type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
}

// MaxSessionMessages bounds the in-memory history as a
// defense-in-depth ceiling on long-running sessions with compression
// disabled; context compression is the intended mechanism, this is
// the safety net against unbounded growth. Oldest messages are
// dropped past the cap.
const MaxSessionMessages = 1000

// Enforces MaxSessionMessages by dropping the oldest messages.
// Callers must hold s.mu.
func (s *Session) trimLocked() {
	if over := len(s.messages) - MaxSessionMessages; over > 0 {
		s.messages = s.messages[over:]
	}
}

// Session holds conversation messages for a single session.
// Safe for concurrent use; uses mutex for synchronization.
// When a Store is bound via BindStore, every mutation
// write-throughs to it.
type Session struct {
	id       string
	mu       sync.Mutex
	messages []Message

	// Persistence state, guarded by mu. store is nil for
	// in-memory sessions (subagents, session.enabled: false).
	store    Store
	meta     Meta
	rowSaved bool // a sessions row exists for id
}

// Returns a new empty Session with a generated UUID.
func New() *Session {
	id, _ := xuuid.New()

	return &Session{
		id:       id,
		messages: make([]Message, 0),
	}
}

// Returns a new empty Session with the given id. Used to continue
// a persisted session so audit entries keep sharing one session_id
// across restarts.
func NewWithID(id string) *Session {
	return &Session{
		id:       id,
		messages: make([]Message, 0),
	}
}

// Binds a store for write-through persistence. m supplies the
// metadata written on the lazy first-user-message row create;
// m.ID must match the session id. Call once at startup before any
// appends.
func (s *Session) BindStore(store Store, m Meta) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store = store
	s.meta = m

	// rowSaved may already be true when the session came from
	// Store.Load (newLoaded): an empty-but-existing row (e.g. a
	// resumed session that was later /reset) would otherwise look
	// un-saved by meta counters and silently drop writes.
	if !s.rowSaved && (m.LastSeq > 0 || m.MessageCount > 0) {
		s.rowSaved = true
	}
}

// Returns the current session title.
func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.meta.Title
}

// Renames the session, updating both the live metadata and the
// stored row. A no-op for in-memory sessions. Uses explicit
// Unlock (not defer) so the store round-trip does not run while
// the session mutex is held.
func (s *Session) SetTitle(title string) error {
	s.mu.Lock()

	s.meta.Title = title
	if s.store == nil || !s.rowSaved {
		s.mu.Unlock()

		return nil
	}
	err := s.store.Rename(s.id, title)
	s.mu.Unlock()

	if err != nil {
		return fmt.Errorf("rename session: %w", err)
	}

	return nil
}

// Touches the sessions row, creating it (with a derived title)
// when a user-role message first appears. added is the message
// batch being persisted. Returns whether the row exists after the
// call. Callers hold s.mu.
func (s *Session) ensureRowLocked(added []Message) bool {
	if s.rowSaved {
		// Touch on every write: keeps "most recent" ordering and
		// the stored agent/provider/model fresh.
		if err := s.store.Save(s.meta); err != nil {
			slog.Warn("session persist: touch failed",
				"session", s.id, "err", err)
		}

		return true
	}

	userText, ok := firstUserText(added)
	if !ok {
		return false
	}
	if s.meta.Title == "" {
		s.meta.Title = DeriveTitle(userText)
	}
	if err := s.store.Save(s.meta); err != nil {
		slog.Warn("session persist: save meta failed",
			"session", s.id, "err", err)

		return false
	}
	s.rowSaved = true

	return true
}

// Persists the appended batch: creates the row lazily when a user
// message first appears, touches it on every write, then appends
// the new messages. fullMirror forces a whole-window rewrite
// instead of an append because the live window no longer matches
// what an Append of this batch would produce (trim, or messages
// that predate row creation). Callers hold s.mu.
func (s *Session) persistLocked(added []Message, fullMirror bool) {
	if s.store == nil {
		return
	}

	if !s.ensureRowLocked(added) {
		return
	}

	if fullMirror {
		s.resyncLocked()

		return
	}

	if err := s.store.Append(s.id, added); err != nil {
		slog.Warn("session persist: append failed",
			"session", s.id, "err", err)
	}
}

// Returns the content of the first user-role message in msgs and
// whether one was present.
func firstUserText(msgs []Message) (string, bool) {
	for _, m := range msgs {
		if m.Role == "user" {
			return m.Content, true
		}
	}

	return "", false
}

// Persists the full live window after a trim dropped messages, so
// the store mirrors exactly what the model sees (no drift).
// Callers hold s.mu.
func (s *Session) resyncLocked() {
	if s.store == nil || !s.rowSaved {
		return
	}
	if err := s.store.ReplaceFrom(s.id, 0, s.messages); err != nil {
		slog.Warn("session persist: resync failed",
			"session", s.id, "err", err)
	}
}

// Returns the session identifier.
func (s *Session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.id
}

// Replaces all messages. Used by context compression to rewrite history.
// When a store is bound, the replacement is persisted so the DB mirrors
// exactly what the model sees.
func (s *Session) SetMessages(msgs []Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = msgs
	s.trimLocked()

	if s.store == nil {
		return
	}

	if !s.ensureRowLocked(msgs) {
		return
	}

	if err := s.store.ReplaceFrom(s.id, 0, s.messages); err != nil {
		slog.Warn("session persist: replace failed",
			"session", s.id, "err", err)
	}
}

// Adds a message to the session history.
func (s *Session) Append(role, content string) {
	s.appendLocked(Message{Role: role, Content: content})
}

// Appends multiple messages in a single locked operation.
func (s *Session) AppendMessages(msgs []Message) {
	s.appendLocked(msgs...)
}

// Adds an assistant message with optional tool calls to the session history.
// toolCalls may be nil for text-only responses.
func (s *Session) AppendAssistant(content string, toolCalls []llm.ToolCall) {
	s.appendLocked(Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	})
}

// Adds a tool result message with correlation fields.
func (s *Session) AppendTool(role, content, toolName, callID string) {
	s.appendLocked(Message{
		Role:       role,
		Content:    content,
		ToolName:   toolName,
		ToolCallID: callID,
	})
}

// Shared append path: mutate in memory, trim, then write-through.
// A trim that dropped older messages triggers a full resync so the
// store keeps mirroring the live window.
func (s *Session) appendLocked(msgs ...Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil {
		s.messages = append(s.messages, msgs...)
		s.trimLocked()

		return
	}

	before := len(s.messages)
	s.messages = append(s.messages, msgs...)
	s.trimLocked()

	// The store must mirror the live window exactly: a trim
	// dropped older rows, and messages that predate lazy row
	// creation were never appended, so both cases need a
	// full-window rewrite instead of an Append of this batch.
	fullMirror := before+len(msgs) > len(s.messages) ||
		(!s.rowSaved && before > 0)

	s.persistLocked(msgs, fullMirror)
}

// Clears all messages from the session. With a bound store the
// empty window is mirrored (rows wiped, session row kept), so a
// later resume sees exactly what /reset left. Archive-and-rotate
// semantics are deferred to M2; see the audit-session-id reason.
func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = s.messages[:0]

	if s.store == nil || !s.rowSaved {
		return
	}

	if err := s.store.ReplaceFrom(s.id, 0, nil); err != nil {
		slog.Warn("session persist: reset mirror failed",
			"session", s.id, "err", err)
	}
}

// UpdateMeta refreshes the session's persisted metadata fields
// from the live config so they reflect runtime changes (/model,
// /agent, /platform) before the next Sync. Callers should invoke
// this immediately after a config override.
func (s *Session) UpdateMeta(provider, model, agentMode string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.meta.Provider = provider
	s.meta.Model = model
	s.meta.AgentMode = agentMode
}

// Sync flushes the session's in-memory state to the bound store.
// It ensures metadata is current so an abrupt exit cannot lose
// pending mutations. Safe to call multiple times; a
// no-op for in-memory sessions or when no row has been created.
func (s *Session) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil || !s.rowSaved {
		return nil
	}

	if err := s.store.Save(s.meta); err != nil {
		return fmt.Errorf("sync session %s: %w", s.id, err)
	}

	return nil
}

// Returns a deep copy of all messages in order.
func (s *Session) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Message, len(s.messages))
	copy(result, s.messages)

	// Deep-copy ToolCalls so returned messages cannot alias session state.
	for i, m := range s.messages {
		if len(m.ToolCalls) > 0 {
			result[i].ToolCalls = make([]llm.ToolCall, len(m.ToolCalls))
			copy(result[i].ToolCalls, m.ToolCalls)
		}
	}

	return result
}
