package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"shmorby/internal/llm"
	"shmorby/internal/redact"
	"shmorby/internal/util"
	"shmorby/internal/xdg"
)

// ErrNotFound is returned by Load when no session row exists for the
// requested id.
var ErrNotFound = errors.New("session not found")

// schemaVersion is the sessions.db schema version tracked via
// PRAGMA user_version. Bump when the schema changes and add a
// migration step in migrate().
const schemaVersion = 1

// activeGuardWindow is how recently a session must have been
// written for Prune to consider it in use by a live process and
// skip it. Write-through bumps updated_at on every append, so any
// session being actively used falls inside this window (AC9).
const activeGuardWindow = 5 * time.Minute

// Meta describes a persisted session row.
type Meta struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Directory    string    `json:"directory"`
	AgentMode    string    `json:"agent_mode"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	ParentID     string    `json:"parent_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ArchivedAt   time.Time `json:"archived_at,omitempty"`
	LastSeq      int       `json:"last_seq"`
	MessageCount int       `json:"message_count"`
}

// Query filters the result of Store.List.
type Query struct {
	// Directory restricts results to one launch directory.
	// Empty means all directories.
	Directory string

	// IncludeArchived adds soft-archived sessions to the result.
	IncludeArchived bool

	// Limit caps the number of rows; 0 means no limit.
	Limit int
}

// Store persists sessions and their messages. The SQLite-backed
// implementation lives in this package; the interface keeps the
// write-through path in Session testable.
type Store interface {
	// Save upserts the session metadata row.
	Save(m Meta) error

	// Append writes msgs after the session's last_seq and bumps
	// last_seq/updated_at (write-through path).
	Append(id string, msgs []Message) error

	// ReplaceFrom deletes messages with seq >= seq and inserts
	// msgs in order, renumbering from seq. Used by compression
	// rewrites and history resets so the DB mirrors the live
	// window exactly.
	ReplaceFrom(id string, seq int, msgs []Message) error

	// Load reads a session and its messages. Trailing incomplete
	// tool-call turns are truncated from both the returned
	// session and the store (AC6).
	Load(id string) (*Session, Meta, error)

	// Latest returns the most recently updated root session for
	// dir. The bool is false when none exists.
	Latest(dir string) (Meta, bool)

	// List returns root sessions matching q, newest first.
	List(q Query) ([]Meta, error)

	// Rename sets the session title.
	Rename(id, title string) error

	// Archive soft-deletes a session (reversible, excluded from
	// List by default).
	Archive(id string) error

	// Delete hard-deletes a session and cascades to child
	// sessions and all message rows.
	Delete(id string) error

	// Prune removes archived/inactive sessions older than
	// olderThan and trims to maxKeep, never touching sessions
	// written within activeGuardWindow. Returns the number
	// deleted.
	Prune(olderThan time.Duration, maxKeep int) (int, error)

	// Close releases the underlying database handle.
	Close() error
}

// Store schema. v1; tracked via PRAGMA user_version.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    directory   TEXT NOT NULL,
    agent_mode  TEXT NOT NULL,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    parent_id   TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    archived_at INTEGER,
    last_seq    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_dir_upd
    ON sessions(directory, updated_at DESC);

CREATE TABLE IF NOT EXISTS session_messages (
    session_id   TEXT NOT NULL,
    seq          INTEGER NOT NULL,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL,
    tool_name    TEXT,
    tool_call_id TEXT,
    tool_calls   TEXT,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (session_id, seq)
);
`

// sqliteStore is the SQLite-backed Store.
type sqliteStore struct {
	db *sql.DB
}

// NewStore opens or creates the session database at dbPath and runs
// the migration ladder. Pass ":memory:" for an in-memory store.
func NewStore(dbPath string) (Store, error) {
	dbPath = util.ExpandPath(dbPath)
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create session dir: %w", err)
		}
		// Pre-create the file 0600: sessions hold raw (redacted)
		// operational history, same policy as audit.db content.
		f, err := os.OpenFile(dbPath,
			os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create session db: %w", err)
		}
		f.Close()
	}

	dsn := dbPath
	if dbPath != ":memory:" {
		dsn += "?_journal_mode=WAL&_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open session db: %w", err)
	}

	// In-memory databases require a single connection to share
	// state.
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &sqliteStore{db: db}, nil
}

// Reads PRAGMA user_version, refuses newer schemas, and applies the
// migration ladder up to schemaVersion.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf(
			"session db schema version %d is newer than this "+
				"shmorby supports (v%d); upgrade shmorby",
			version, schemaVersion,
		)
	}
	steps := map[int]string{1: schemaV1}
	for v := version + 1; v <= schemaVersion; v++ {
		step, ok := steps[v]
		if !ok {
			return fmt.Errorf(
				"missing session schema migration step for v%d", v,
			)
		}
		if _, err := db.Exec(step); err != nil {
			return fmt.Errorf("apply schema v%d: %w", v, err)
		}
		if _, err := db.Exec(
			fmt.Sprintf(`PRAGMA user_version = %d`, v),
		); err != nil {
			return fmt.Errorf("set schema version v%d: %w", v, err)
		}
	}
	return nil
}

// Releases the database handle.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// Upserts a session metadata row. Title is only written on insert;
// later saves never clobber a title set via Rename. Directory is
// normalized so rows written from symlinked paths (e.g. /tmp vs
// /private/tmp) still match Latest/List lookups.
func (s *sqliteStore) Save(m Meta) error {
	m.Directory = NormalizeDir(m.Directory)
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	var parent any
	if m.ParentID != "" {
		parent = m.ParentID
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions
		 (id, title, directory, agent_mode, provider, model,
		  parent_id, created_at, updated_at, last_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		  title      = COALESCE(NULLIF(excluded.title, ''),
		                        sessions.title),
		  directory  = excluded.directory,
		  agent_mode = excluded.agent_mode,
		  provider   = excluded.provider,
		  model      = excluded.model,
		  parent_id  = COALESCE(excluded.parent_id,
		                        sessions.parent_id),
		  updated_at = excluded.updated_at`,
		m.ID, m.Title, m.Directory, m.AgentMode, m.Provider,
		m.Model, parent,
		m.CreatedAt.Unix(), now.Unix(), m.LastSeq,
	)
	if err != nil {
		return fmt.Errorf("save session %s: %w", m.ID, err)
	}
	return nil
}

// Appends msgs after the row's current last_seq in one transaction,
// updating last_seq and updated_at.
func (s *sqliteStore) Append(id string, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}

	var lastSeq int
	err := s.db.QueryRow(
		`SELECT last_seq FROM sessions WHERE id = ?`, id,
	).Scan(&lastSeq)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("read last_seq: %w", err)
	}

	return s.replaceFrom(id, lastSeq, msgs)
}

// Deletes messages at or after seq, inserts msgs renumbered from
// seq, and syncs last_seq/updated_at — all in one transaction.
func (s *sqliteStore) ReplaceFrom(id string, seq int, msgs []Message) error {
	if seq < 0 {
		seq = 0
	}
	return s.replaceFrom(id, seq, msgs)
}

// Runs the shared replace+renumber transaction for Append and
// ReplaceFrom. Callers supply a non-negative start seq.
func (s *sqliteStore) replaceFrom(
	id string, seq int, msgs []Message,
) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin replace: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM session_messages
		 WHERE session_id = ? AND seq >= ?`, id, seq,
	); err != nil {
		return fmt.Errorf("delete trailing messages: %w", err)
	}

	now := time.Now().UTC().Unix()
	stmt, err := tx.Prepare(
		`INSERT INTO session_messages
		 (session_id, seq, role, content, tool_name,
		  tool_call_id, tool_calls, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert message: %w", err)
	}
	defer stmt.Close()

	i := seq
	for _, m := range msgs {
		toolCalls, err := encodeToolCalls(m.ToolCalls)
		if err != nil {
			return err
		}
		var toolName, toolCallID any
		if m.ToolName != "" {
			toolName = m.ToolName
		}
		if m.ToolCallID != "" {
			toolCallID = m.ToolCallID
		}
		if _, err := stmt.Exec(
			id, i, m.Role, redact.SecretString(m.Content),
			toolName, toolCallID, toolCalls, now,
		); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		i++
	}

	if _, err := tx.Exec(
		`UPDATE sessions
		 SET last_seq = ?, updated_at = ?
		 WHERE id = ?`, i, now, id,
	); err != nil {
		return fmt.Errorf("update last_seq: %w", err)
	}

	return tx.Commit()
}

// Reads the session row and its messages, truncating a trailing
// incomplete tool-call turn from both store and result (AC6).
func (s *sqliteStore) Load(id string) (*Session, Meta, error) {
	m, found, err := s.metaByID(id)
	if err != nil {
		return nil, Meta{}, err
	}
	if !found {
		return nil, Meta{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	msgs, err := s.messages(id)
	if err != nil {
		return nil, Meta{}, err
	}

	complete := validateTurnPairing(msgs)
	if len(complete) < len(msgs) {
		// Drop the dangling rows so a future Load of the same id
		// sees exactly what the model will see.
		if err := s.ReplaceFrom(id, len(complete), nil); err != nil {
			slog.Warn("session load: truncating incomplete turn failed",
				"session", id, "err", err)
		}
		m.LastSeq = len(complete)
		m.MessageCount = len(complete)
	}

	return newLoaded(id, complete), m, nil
}

// Returns the row for id, or found=false when it does not exist.
func (s *sqliteStore) metaByID(id string) (Meta, bool, error) {
	row := s.db.QueryRow(
		`SELECT id, title, directory, agent_mode, provider, model,
		        COALESCE(parent_id, ''), created_at, updated_at,
		        COALESCE(archived_at, 0), last_seq,
		        (SELECT COUNT(*) FROM session_messages
		         WHERE session_messages.session_id = sessions.id)
		 FROM sessions WHERE id = ?`, id,
	)
	m, err := scanMeta(row)
	if err == sql.ErrNoRows {
		return Meta{}, false, nil
	}
	if err != nil {
		return Meta{}, false, fmt.Errorf("load session meta: %w", err)
	}
	return m, true, nil
}

// Reads all messages for id in seq order.
func (s *sqliteStore) messages(id string) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT role, content, COALESCE(tool_name, ''),
		        COALESCE(tool_call_id, ''), COALESCE(tool_calls, '')
		 FROM session_messages WHERE session_id = ?
		 ORDER BY seq ASC`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var (
			m         Message
			toolCalls string
		)
		if err := rows.Scan(
			&m.Role, &m.Content, &m.ToolName, &m.ToolCallID,
			&toolCalls,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if toolCalls != "" {
			if err := json.Unmarshal(
				[]byte(toolCalls), &m.ToolCalls,
			); err != nil {
				return nil, fmt.Errorf(
					"decode tool_calls for %s: %w", m.Role, err,
				)
			}
		}
		msgs = append(msgs, m)
	}

	return msgs, rows.Err()
}

// Returns the most recently updated, non-archived root session for
// dir (normalized, matching how Save stores directories).
func (s *sqliteStore) Latest(dir string) (Meta, bool) {
	rows, err := s.db.Query(
		`SELECT id, title, directory, agent_mode, provider, model,
		        COALESCE(parent_id, ''), created_at, updated_at,
		        COALESCE(archived_at, 0), last_seq,
		        (SELECT COUNT(*) FROM session_messages
		         WHERE session_messages.session_id = sessions.id)
		 FROM sessions
		 WHERE directory = ? AND archived_at IS NULL
		   AND parent_id IS NULL
		 ORDER BY updated_at DESC LIMIT 1`, NormalizeDir(dir),
	)
	if err != nil {
		slog.Warn("session latest query failed", "err", err)
		return Meta{}, false
	}
	defer rows.Close()
	if !rows.Next() {
		return Meta{}, false
	}
	m, err := scanMeta(rows)
	if err != nil {
		slog.Warn("session latest scan failed", "err", err)
		return Meta{}, false
	}
	return m, true
}

// Lists root sessions matching q, newest first. Child (subagent)
// sessions are never listed so the picker stays top-level only.
func (s *sqliteStore) List(q Query) ([]Meta, error) {
	query :=
		`SELECT id, title, directory, agent_mode, provider, model,
		        COALESCE(parent_id, ''), created_at, updated_at,
		        COALESCE(archived_at, 0), last_seq,
		        (SELECT COUNT(*) FROM session_messages
		         WHERE session_messages.session_id = sessions.id)
		 FROM sessions WHERE parent_id IS NULL`
	var args []any

	if !q.IncludeArchived {
		query += " AND archived_at IS NULL"
	}
	if q.Directory != "" {
		query += " AND directory = ?"
		args = append(args, NormalizeDir(q.Directory))
	}
	query += " ORDER BY updated_at DESC"
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []Meta
	for rows.Next() {
		m, err := scanMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, m)
	}

	return out, rows.Err()
}

// Sets the session title.
func (s *sqliteStore) Rename(id, title string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET title = ? WHERE id = ?`, title, id,
	)
	if err != nil {
		return fmt.Errorf("rename session %s: %w", id, err)
	}
	return nil
}

// Soft-deletes a session.
func (s *sqliteStore) Archive(id string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET archived_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("archive session %s: %w", id, err)
	}
	return nil
}

// Hard-deletes a session and cascades to child session rows and
// their message rows in one transaction.
func (s *sqliteStore) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM session_messages WHERE session_id IN
		 (SELECT id FROM sessions
		  WHERE id = ? OR parent_id = ?)`, id, id,
	); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM sessions WHERE id = ? OR parent_id = ?`,
		id, id,
	); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}

	return tx.Commit()
}

// Deletes sessions older than olderThan (archived rows qualify via
// archived_at, live rows via updated_at) and, when maxKeep > 0,
// trims the oldest rows past that cap. Sessions written inside
// activeGuardWindow are never touched: they may belong to a live
// process. Both passes are collected first and applied in a single
// transaction, so a partial failure can never leave half the
// sweep committed. Returns the number of sessions deleted.
func (s *sqliteStore) Prune(
	olderThan time.Duration, maxKeep int,
) (int, error) {
	now := time.Now().UTC()
	guard := now.Add(-activeGuardWindow).Unix()

	seen := map[string]bool{}
	var ids []string
	collect := func(query string, args ...any) error {
		found, err := s.queryIDs(query, args...)
		if err != nil {
			return err
		}
		for _, id := range found {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		return nil
	}

	if olderThan > 0 {
		cutoff := now.Add(-olderThan).Unix()
		err := collect(
			`SELECT id FROM sessions
			 WHERE updated_at < ?
			   AND ((archived_at IS NOT NULL AND archived_at < ?)
			        OR (archived_at IS NULL AND updated_at < ?))`,
			guard, cutoff, cutoff,
		)
		if err != nil {
			return 0, fmt.Errorf("prune age pass: %w", err)
		}
	}

	if maxKeep > 0 {
		// Everything outside the newest maxKeep rows, guard
		// excluded. The cap is best-effort: more than maxKeep
		// recently active sessions are left alone.
		err := collect(
			`SELECT id FROM sessions
			 WHERE updated_at < ?
			   AND id NOT IN (
			       SELECT id FROM sessions
			       ORDER BY updated_at DESC LIMIT ?
			   )`,
			guard, maxKeep,
		)
		if err != nil {
			return 0, fmt.Errorf("prune cap pass: %w", err)
		}
	}

	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.deleteIDs(ids); err != nil {
		return 0, err
	}

	return len(ids), nil
}

// Runs an id-selecting query and returns the collected ids.
func (s *sqliteStore) queryIDs(query string, args ...any) (
	[]string, error,
) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// deleteIDs removes the given session rows and their messages in
// one transaction. Ids are deleted in chunks of parameterized IN
// lists (bounded well under SQLite's variable limit) so neither a
// huge sweep nor the SQLite parameter cap can break the batch.
func (s *sqliteStore) deleteIDs(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin prune: %w", err)
	}
	defer tx.Rollback()

	const chunk = 400
	for start := 0; start < len(ids); start += chunk {
		end := min(start+chunk, len(ids))
		batch := ids[start:end]

		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")

		if _, err := tx.Exec(
			`DELETE FROM session_messages
			 WHERE session_id IN (`+ph+`)`, args...,
		); err != nil {
			return fmt.Errorf("prune messages: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM sessions WHERE id IN (`+ph+`)`, args...,
		); err != nil {
			return fmt.Errorf("prune sessions: %w", err)
		}
	}

	return tx.Commit()
}

// Scans one row from the shared meta projection into a Meta.
func scanMeta(row interface {
	Scan(dest ...any) error
}) (Meta, error) {
	var (
		m          Meta
		createdAt  int64
		updatedAt  int64
		archivedAt int64
	)
	err := row.Scan(
		&m.ID, &m.Title, &m.Directory, &m.AgentMode,
		&m.Provider, &m.Model, &m.ParentID,
		&createdAt, &updatedAt, &archivedAt,
		&m.LastSeq, &m.MessageCount,
	)
	if err != nil {
		return Meta{}, err
	}
	m.CreatedAt = time.Unix(createdAt, 0).UTC()
	m.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if archivedAt != 0 {
		m.ArchivedAt = time.Unix(archivedAt, 0).UTC()
	}
	return m, nil
}

// Encodes tool calls for persistence, redacting each call's JSON
// args. Returns "" for an empty slice.
func encodeToolCalls(calls []llm.ToolCall) (string, error) {
	if len(calls) == 0 {
		return "", nil
	}
	cp := make([]llm.ToolCall, len(calls))
	for i, c := range calls {
		cp[i] = c
		if c.Args != "" {
			cp[i].Args = redactArgs(c.Args)
		}
	}
	b, err := json.Marshal(cp)
	if err != nil {
		return "", fmt.Errorf("encode tool calls: %w", err)
	}
	return string(b), nil
}

// Redacts a tool-call args payload: JSON-aware tree redaction keeps
// the payload valid for replay; anything that fails to parse falls
// back to plain string scrubbing.
func redactArgs(args string) string {
	if b, err := redact.JSONData([]byte(args)); err == nil {
		return string(b)
	}
	return redact.SecretString(args)
}

// Returns msgs with the trailing incomplete tool-call turn removed.
// A turn is incomplete when an assistant message carries tool calls
// whose results never follow in the slice.
func validateTurnPairing(msgs []Message) []Message {
	pending := map[string]bool{}
	complete := len(msgs)
	for i, m := range msgs {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				pending[tc.ID] = true
			}
		}
		if m.Role == "tool" && m.ToolCallID != "" {
			delete(pending, m.ToolCallID)
		}
		if len(pending) == 0 {
			complete = i + 1
		}
	}
	return msgs[:complete]
}

// Builds a session replayed from the store with the saved id; used
// by Store.Load. rowSaved is set because Load only succeeds for
// rows that exist, even when the message window is empty.
func newLoaded(id string, msgs []Message) *Session {
	s := NewWithID(id)
	if msgs != nil {
		s.messages = msgs
	}
	s.rowSaved = true

	return s
}

// Returns the default sessions database path under the xdg user
// data dir.
func DefaultDBPath() string {
	return filepath.Join(xdg.UserDataDir(), "sessions.db")
}

// Derives a v1 session title from the first user message: first
// line, capped at 60 characters.
func DeriveTitle(userText string) string {
	line, _, _ := strings.Cut(userText, "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return "New session"
	}
	r := []rune(line)
	if len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return line
}

// NormalizeDir resolves a directory to its canonical absolute form
// so session scoping survives symlinked paths (e.g. /tmp vs
// /private/tmp on macOS): stored and queried directories must pass
// through here to compare equal. Unresolvable paths fall back to
// the cleaned absolute form.
func NormalizeDir(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p)
}
