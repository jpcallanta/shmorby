package audit

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Schema for the audit database.
const schema = `
CREATE TABLE IF NOT EXISTS audit_entries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id    TEXT    NOT NULL,
    tool          TEXT    NOT NULL,
    args          TEXT    NOT NULL,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    exit_code     INTEGER,
    error         TEXT,
    output_hash   TEXT,
    captured_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_entries_session ON audit_entries(session_id);
CREATE INDEX IF NOT EXISTS idx_entries_tool ON audit_entries(tool);
CREATE INDEX IF NOT EXISTS idx_entries_captured ON audit_entries(captured_at);

CREATE TABLE IF NOT EXISTS audit_permissions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id    TEXT    NOT NULL,
    entry_id      INTEGER REFERENCES audit_entries(id),
    tool          TEXT    NOT NULL,
    command       TEXT    NOT NULL,
    rule_pattern  TEXT,
    rule_action   TEXT,
    decision      TEXT    NOT NULL,
    reason        TEXT,
    decided_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_perms_session ON audit_permissions(session_id);
CREATE INDEX IF NOT EXISTS idx_perms_entry ON audit_permissions(entry_id);

CREATE TABLE IF NOT EXISTS audit_output (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id      INTEGER NOT NULL REFERENCES audit_entries(id),
    session_id    TEXT    NOT NULL,
    stdout        TEXT,
    stderr        TEXT,
    stdout_size   INTEGER NOT NULL DEFAULT 0,
    stderr_size   INTEGER NOT NULL DEFAULT 0,
    checksum      TEXT,
    UNIQUE(entry_id)
);

CREATE TABLE IF NOT EXISTS audit_subagents (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_session_id   TEXT    NOT NULL,
    child_session_id    TEXT    NOT NULL UNIQUE,
    parent_entry_id     INTEGER REFERENCES audit_entries(id),
    tool                TEXT    NOT NULL,
    status              TEXT    NOT NULL DEFAULT 'pending',
    duration_ms         INTEGER,
    started_at          TEXT    NOT NULL DEFAULT (datetime('now')),
    completed_at        TEXT
);

CREATE INDEX IF NOT EXISTS idx_sub_parent ON audit_subagents(parent_session_id);
CREATE INDEX IF NOT EXISTS idx_sub_child ON audit_subagents(child_session_id);
`

// AuditEntry records a single tool invocation.
type AuditEntry struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Tool       string `json:"tool"`
	Args       string `json:"args"`
	DurationMs int64  `json:"duration_ms"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
	OutputHash string `json:"output_hash,omitempty"`
	CapturedAt string `json:"captured_at"`
}

// PermissionAudit records a permission decision.
type PermissionAudit struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	EntryID     *int64 `json:"entry_id,omitempty"`
	Tool        string `json:"tool"`
	Command     string `json:"command"`
	RulePattern string `json:"rule_pattern,omitempty"`
	RuleAction  string `json:"rule_action,omitempty"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason,omitempty"`
	DecidedAt   string `json:"decided_at"`
}

// OutputCapture stores captured stdout/stderr for an entry.
type OutputCapture struct {
	ID         int64  `json:"id"`
	EntryID    int64  `json:"entry_id"`
	SessionID  string `json:"session_id"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	StdoutSize int    `json:"stdout_size"`
	StderrSize int    `json:"stderr_size"`
	Checksum   string `json:"checksum,omitempty"`
}

// SubagentAudit records a subagent dispatch/completion.
type SubagentAudit struct {
	ID              int64   `json:"id"`
	ParentSessionID string  `json:"parent_session_id"`
	ChildSessionID  string  `json:"child_session_id"`
	ParentEntryID   *int64  `json:"parent_entry_id,omitempty"`
	Tool            string  `json:"tool"`
	Status          string  `json:"status"`
	DurationMs      *int64  `json:"duration_ms,omitempty"`
	StartedAt       string  `json:"started_at"`
	CompletedAt     *string `json:"completed_at,omitempty"`
}

// QueryFilter specifies optional filters for audit queries.
type QueryFilter struct {
	SessionID string
	Tool      string
	ExitCode  *int
	Decision  string
	Since     time.Time
	Before    time.Time
	Limit     int
}

// AuditStore is a SQLite-backed audit log store.
type AuditStore struct {
	db *sql.DB
}

// Opens or creates an audit database at the given path.
// Pass ":memory:" for an in-memory store (testing).
func NewAuditStore(dbPath string) (*AuditStore, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create audit dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}

	// In-memory databases require a single connection to share state.
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &AuditStore{db: db}, nil
}

// Releases the database connection.
func (s *AuditStore) Close() error {
	return s.db.Close()
}

// Inserts a single audit entry and returns its ID.
func (s *AuditStore) InsertEntry(e AuditEntry) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO audit_entries
		 (session_id, tool, args, duration_ms, exit_code, error, output_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, e.Tool, e.Args, e.DurationMs,
		e.ExitCode, e.Error, e.OutputHash,
	)
	if err != nil {
		return 0, fmt.Errorf("insert entry: %w", err)
	}
	return res.LastInsertId()
}

// Inserts multiple entries in a single transaction.
func (s *AuditStore) BatchInsert(entries []AuditEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin batch: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO audit_entries
		 (session_id, tool, args, duration_ms, exit_code, error, output_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.Exec(
			e.SessionID, e.Tool, e.Args, e.DurationMs,
			e.ExitCode, e.Error, e.OutputHash,
		); err != nil {
			return fmt.Errorf("insert entry in batch: %w", err)
		}
	}

	return tx.Commit()
}

// Records a permission decision.
func (s *AuditStore) InsertPermission(p PermissionAudit) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO audit_permissions
		 (session_id, entry_id, tool, command, rule_pattern, rule_action, decision, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SessionID, p.EntryID, p.Tool, p.Command,
		p.RulePattern, p.RuleAction, p.Decision, p.Reason,
	)
	if err != nil {
		return 0, fmt.Errorf("insert permission: %w", err)
	}
	return res.LastInsertId()
}

// Stores captured stdout/stderr for an entry.
func (s *AuditStore) InsertOutput(o OutputCapture) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_output
		 (entry_id, session_id, stdout, stderr, stdout_size, stderr_size, checksum)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		o.EntryID, o.SessionID, o.Stdout, o.Stderr,
		o.StdoutSize, o.StderrSize, o.Checksum,
	)
	if err != nil {
		return fmt.Errorf("insert output: %w", err)
	}
	return nil
}

// Records a subagent dispatch.
func (s *AuditStore) InsertSubagent(sa SubagentAudit) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO audit_subagents
		 (parent_session_id, child_session_id, parent_entry_id, tool, status)
		 VALUES (?, ?, ?, ?, ?)`,
		sa.ParentSessionID, sa.ChildSessionID,
		sa.ParentEntryID, sa.Tool, sa.Status,
	)
	if err != nil {
		return 0, fmt.Errorf("insert subagent: %w", err)
	}
	return res.LastInsertId()
}

// Updates a subagent record with completion info.
func (s *AuditStore) CompleteSubagent(
	childSessionID string, durationMs int64, status string,
) error {
	_, err := s.db.Exec(
		`UPDATE audit_subagents
		 SET status = ?, duration_ms = ?, completed_at = datetime('now')
		 WHERE child_session_id = ?`,
		status, durationMs, childSessionID,
	)
	if err != nil {
		return fmt.Errorf("complete subagent: %w", err)
	}
	return nil
}

// Returns entries matching the given filter.
func (s *AuditStore) QueryEntries(f QueryFilter) ([]AuditEntry, error) {
	query := `SELECT id, session_id, tool, args, duration_ms, exit_code, error, output_hash, captured_at
	           FROM audit_entries WHERE 1=1`
	args := []any{}

	if f.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, f.SessionID)
	}
	if f.Tool != "" {
		query += " AND tool = ?"
		args = append(args, f.Tool)
	}
	if f.ExitCode != nil {
		query += " AND exit_code = ?"
		args = append(args, *f.ExitCode)
	}
	if !f.Since.IsZero() {
		query += " AND captured_at >= ?"
		args = append(args, f.Since.UTC().Format("2006-01-02 15:04:05"))
	}
	if !f.Before.IsZero() {
		query += " AND captured_at <= ?"
		args = append(args, f.Before.UTC().Format("2006-01-02 15:04:05"))
	}

	query += " ORDER BY id DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.Tool, &e.Args,
			&e.DurationMs, &e.ExitCode, &e.Error,
			&e.OutputHash, &e.CapturedAt,
		); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, rows.Err()
}

// Returns permission decisions matching the filter.
func (s *AuditStore) QueryPermissions(f QueryFilter) ([]PermissionAudit, error) {
	query := `SELECT id, session_id, entry_id, tool, command, rule_pattern, rule_action, decision, reason, decided_at
	           FROM audit_permissions WHERE 1=1`
	args := []any{}

	if f.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, f.SessionID)
	}
	if f.Tool != "" {
		query += " AND tool = ?"
		args = append(args, f.Tool)
	}
	if f.Decision != "" {
		query += " AND decision = ?"
		args = append(args, f.Decision)
	}
	if !f.Since.IsZero() {
		query += " AND decided_at >= ?"
		args = append(args, f.Since.UTC().Format("2006-01-02 15:04:05"))
	}

	query += " ORDER BY id DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()

	var perms []PermissionAudit
	for rows.Next() {
		var p PermissionAudit
		if err := rows.Scan(
			&p.ID, &p.SessionID, &p.EntryID, &p.Tool,
			&p.Command, &p.RulePattern, &p.RuleAction,
			&p.Decision, &p.Reason, &p.DecidedAt,
		); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}

	return perms, rows.Err()
}

// Returns a single entry by ID.
func (s *AuditStore) GetEntry(id int64) (*AuditEntry, error) {
	var e AuditEntry
	err := s.db.QueryRow(
		`SELECT id, session_id, tool, args, duration_ms, exit_code, error, output_hash, captured_at
		 FROM audit_entries WHERE id = ?`, id,
	).Scan(
		&e.ID, &e.SessionID, &e.Tool, &e.Args,
		&e.DurationMs, &e.ExitCode, &e.Error,
		&e.OutputHash, &e.CapturedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get entry: %w", err)
	}
	return &e, nil
}

// Returns the captured output for an entry.
func (s *AuditStore) GetOutput(entryID int64) (*OutputCapture, error) {
	var o OutputCapture
	err := s.db.QueryRow(
		`SELECT id, entry_id, session_id, stdout, stderr, stdout_size, stderr_size, checksum
		 FROM audit_output WHERE entry_id = ?`, entryID,
	).Scan(
		&o.ID, &o.EntryID, &o.SessionID, &o.Stdout,
		&o.Stderr, &o.StdoutSize, &o.StderrSize, &o.Checksum,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get output: %w", err)
	}
	return &o, nil
}

// Returns all entries for a session, including child sessions.
func (s *AuditStore) GetSessionEntries(sessionID string) ([]AuditEntry, error) {
	query := `SELECT id, session_id, tool, args, duration_ms, exit_code, error, output_hash, captured_at
	           FROM audit_entries
	           WHERE session_id IN (
	             SELECT child_session_id FROM audit_subagents WHERE parent_session_id = ?
	             UNION SELECT ?
	           )
	           ORDER BY id ASC`

	rows, err := s.db.Query(query, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session entries: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.Tool, &e.Args,
			&e.DurationMs, &e.ExitCode, &e.Error,
			&e.OutputHash, &e.CapturedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, rows.Err()
}

// Returns all subagent records for a parent session.
func (s *AuditStore) GetSubagents(parentSessionID string) ([]SubagentAudit, error) {
	rows, err := s.db.Query(
		`SELECT id, parent_session_id, child_session_id, parent_entry_id, tool, status, duration_ms, started_at, completed_at
		 FROM audit_subagents WHERE parent_session_id = ?
		 ORDER BY id ASC`, parentSessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query subagents: %w", err)
	}
	defer rows.Close()

	var subs []SubagentAudit
	for rows.Next() {
		var sa SubagentAudit
		var completedAt sql.NullString
		if err := rows.Scan(
			&sa.ID, &sa.ParentSessionID, &sa.ChildSessionID,
			&sa.ParentEntryID, &sa.Tool, &sa.Status,
			&sa.DurationMs, &sa.StartedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subagent: %w", err)
		}
		if completedAt.Valid {
			sa.CompletedAt = &completedAt.String
		}
		subs = append(subs, sa)
	}

	return subs, rows.Err()
}

// Writes matching entries as JSON to the writer.
func (s *AuditStore) ExportJSON(w io.Writer, f QueryFilter) error {
	entries, err := s.QueryEntries(f)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode entry: %w", err)
		}
	}
	return nil
}

// Writes matching entries as CSV to the writer.
func (s *AuditStore) ExportCSV(w io.Writer, f QueryFilter) error {
	entries, err := s.QueryEntries(f)
	if err != nil {
		return err
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write([]string{
		"id", "session_id", "tool", "args",
		"duration_ms", "exit_code", "error", "captured_at",
	}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for _, e := range entries {
		exitCode := ""
		if e.ExitCode != nil {
			exitCode = fmt.Sprintf("%d", *e.ExitCode)
		}
		if err := cw.Write([]string{
			fmt.Sprintf("%d", e.ID),
			e.SessionID,
			e.Tool,
			e.Args,
			fmt.Sprintf("%d", e.DurationMs),
			exitCode,
			e.Error,
			e.CapturedAt,
		}); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}

	return nil
}

// Removes entries older than the given duration.
func (s *AuditStore) Vacuum(before time.Duration) (entries, permissions, output, subagents int64, err error) {
	cutoff := time.Now().UTC().Add(-before).Format("2006-01-02 15:04:05")

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("begin vacuum: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`DELETE FROM audit_output WHERE entry_id IN
		 (SELECT id FROM audit_entries WHERE captured_at < ?)`, cutoff,
	)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("vacuum output: %w", err)
	}
	output, _ = res.RowsAffected()

	res, err = tx.Exec(
		`DELETE FROM audit_permissions WHERE decided_at < ?`, cutoff,
	)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("vacuum permissions: %w", err)
	}
	permissions, _ = res.RowsAffected()

	res, err = tx.Exec(
		`DELETE FROM audit_subagents WHERE started_at < ?`, cutoff,
	)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("vacuum subagents: %w", err)
	}
	subagents, _ = res.RowsAffected()

	res, err = tx.Exec(
		`DELETE FROM audit_entries WHERE captured_at < ?`, cutoff,
	)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("vacuum entries: %w", err)
	}
	entries, _ = res.RowsAffected()

	return entries, permissions, output, subagents, tx.Commit()
}

// Stats returns row counts and database file size.
type AuditStats struct {
	Entries     int64 `json:"entries"`
	Permissions int64 `json:"permissions"`
	Output      int64 `json:"output"`
	Subagents   int64 `json:"subagents"`
	DBSize      int64 `json:"db_size_bytes"`
}

func (s *AuditStore) Stats() (*AuditStats, error) {
	st := &AuditStats{}

	s.db.QueryRow(`SELECT COUNT(*) FROM audit_entries`).Scan(&st.Entries)
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_permissions`).Scan(&st.Permissions)
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_output`).Scan(&st.Output)
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_subagents`).Scan(&st.Subagents)

	var pageSize, pageCount int64
	s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount)
	s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	st.DBSize = pageSize * pageCount

	return st, nil
}

// Returns a SHA256 hex string of the input.
func ComputeChecksum(data string) string {
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

// Returns the default audit database path.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", "shmorby", "audit.db")
	}
	return filepath.Join(home, ".local", "share", "shmorby", "audit.db")
}
