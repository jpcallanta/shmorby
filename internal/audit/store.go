package audit

import (
	"context"
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
    input_id      TEXT,
    execution_id  TEXT,
    message_id    TEXT    NOT NULL DEFAULT '',
    tool_call_id  TEXT    NOT NULL DEFAULT '',
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
    input_id      TEXT,
    execution_id  TEXT,
    approval_id   TEXT    NOT NULL DEFAULT '',
    entry_id      INTEGER REFERENCES audit_entries(id),
    tool          TEXT    NOT NULL,
    command       TEXT    NOT NULL,
    rule_pattern  TEXT,
    rule_action   TEXT,
    decision      TEXT    NOT NULL,
    reason        TEXT,
    decided_by    TEXT    NOT NULL DEFAULT '',
    decision_source TEXT NOT NULL DEFAULT '',
    scope         TEXT    NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS audit_chain_state (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    seq           INTEGER NOT NULL,
    last_hash     TEXT    NOT NULL
);
INSERT OR IGNORE INTO audit_chain_state (id, seq, last_hash)
VALUES (1, 0, '');

CREATE TABLE IF NOT EXISTS audit_chain (
    seq           INTEGER PRIMARY KEY,
    record_type   TEXT    NOT NULL,
    subject_id    TEXT    NOT NULL DEFAULT '',
    payload       TEXT    NOT NULL,
    prev_hash     TEXT    NOT NULL,
    entry_hash    TEXT    NOT NULL,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_audit_chain_subject
    ON audit_chain(record_type, subject_id);
`

// AuditEntry records a single tool invocation.
type AuditEntry struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	InputID     string `json:"input_id,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	Tool        string `json:"tool"`
	Args        string `json:"args"`
	DurationMs  int64  `json:"duration_ms"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Error       string `json:"error,omitempty"`
	OutputHash  string `json:"output_hash,omitempty"`
	CapturedAt  string `json:"captured_at"`
}

// PermissionAudit records a permission decision.
type PermissionAudit struct {
	ID             int64  `json:"id"`
	SessionID      string `json:"session_id"`
	InputID        string `json:"input_id,omitempty"`
	ExecutionID    string `json:"execution_id,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"`
	EntryID        *int64 `json:"entry_id,omitempty"`
	Tool           string `json:"tool"`
	Command        string `json:"command"`
	RulePattern    string `json:"rule_pattern,omitempty"`
	RuleAction     string `json:"rule_action,omitempty"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason,omitempty"`
	DecidedBy      string `json:"decided_by,omitempty"`
	DecisionSource string `json:"decision_source,omitempty"`
	Scope          string `json:"scope,omitempty"`
	DecidedAt      string `json:"decided_at"`
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

type outputChainRecord struct {
	ID         int64  `json:"id"`
	EntryID    int64  `json:"entry_id"`
	SessionID  string `json:"session_id"`
	StdoutHash string `json:"stdout_hash"`
	StderrHash string `json:"stderr_hash"`
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
	SessionID   string
	Tool        string
	ExitCode    *int
	Decision    string
	ExecutionID string
	ApprovalID  string
	Since       time.Time
	Before      time.Time
	Limit       int
}

// AuditStore is a SQLite-backed audit log store.
type AuditStore struct {
	db *sql.DB
}

// Defines a query surface shared by a database handle and a
// transaction so chain verification reads one snapshot.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
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

	// Add newer audit columns to existing databases in place.
	for _, col := range []struct {
		table string
		name  string
		decl  string
	}{
		{"audit_entries", "input_id", "TEXT"},
		{"audit_entries", "execution_id", "TEXT"},
		{"audit_entries", "message_id", "TEXT NOT NULL DEFAULT ''"},
		{"audit_entries", "tool_call_id", "TEXT NOT NULL DEFAULT ''"},
		{"audit_permissions", "input_id", "TEXT"},
		{"audit_permissions", "execution_id", "TEXT"},
		{"audit_permissions", "approval_id", "TEXT NOT NULL DEFAULT ''"},
		{"audit_permissions", "decided_by", "TEXT NOT NULL DEFAULT ''"},
		{"audit_permissions", "decision_source", "TEXT NOT NULL DEFAULT ''"},
		{"audit_permissions", "scope", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(
			db, col.table, col.name, col.decl,
		); err != nil {
			db.Close()

			return nil, err
		}
	}

	for _, index := range []string{
		`CREATE INDEX IF NOT EXISTS idx_entries_execution
			ON audit_entries(execution_id)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_message
			ON audit_entries(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_perms_execution
			ON audit_permissions(execution_id)`,
		`CREATE INDEX IF NOT EXISTS idx_perms_approval
			ON audit_permissions(approval_id)`,
	} {
		if _, err := db.Exec(index); err != nil {
			db.Close()

			return nil, fmt.Errorf("create audit index: %w", err)
		}
	}

	return &AuditStore{db: db}, nil
}

// Adds column to table when an older audit database lacks it.
func ensureColumn(
	db *sql.DB, table, name, decl string,
) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}

	found := false
	for rows.Next() {
		var (
			cid     int
			cname   string
			ctype   string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(
			&cid, &cname, &ctype, &notNull, &dflt, &pk,
		); err != nil {
			rows.Close()

			return fmt.Errorf("inspect %s: %w", table, err)
		}
		if cname == name {
			found = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if found {
		return nil
	}

	_, err = db.Exec(fmt.Sprintf(
		`ALTER TABLE %s ADD COLUMN %s %s`, table, name, decl,
	))
	if err != nil {
		return fmt.Errorf("add %s.%s: %w", table, name, err)
	}

	return nil
}

// Releases the database connection.
func (s *AuditStore) Close() error {
	return s.db.Close()
}

// LifecycleEvent records an approval or execution boundary.
type LifecycleEvent struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id,omitempty"`
	InputID     string `json:"input_id,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	ApprovalID  string `json:"approval_id,omitempty"`
	Tool        string `json:"tool,omitempty"`
	Command     string `json:"command,omitempty"`
	Decision    string `json:"decision,omitempty"`
	Status      string `json:"status,omitempty"`
	Error       string `json:"error,omitempty"`
	OccurredAt  string `json:"occurred_at"`
}

// Appends a lifecycle record to the durable hash chain.
func (s *AuditStore) AppendLifecycleEvent(event LifecycleEvent) error {
	if event.Type == "" {
		return fmt.Errorf("lifecycle event type is empty")
	}
	if event.OccurredAt == "" {
		event.OccurredAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin lifecycle event: %w", err)
	}
	defer tx.Rollback()

	if err := appendChainRecord(
		tx, event.Type, event.ExecutionID, event,
	); err != nil {
		return fmt.Errorf("append lifecycle event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lifecycle event: %w", err)
	}

	return nil
}

// Reports whether an execution has a matching lifecycle record.
func (s *AuditStore) HasLifecycleEvent(
	executionID, eventType string,
) (bool, error) {
	var found int
	err := s.db.QueryRow(
		`SELECT 1 FROM audit_chain
		 WHERE subject_id = ? AND record_type = ? LIMIT 1`,
		executionID, eventType,
	).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query lifecycle event: %w", err)
	}

	return true, nil
}

// VerificationResult summarizes a verified hash-chain range.
type VerificationResult struct {
	From     int64
	To       int64
	Verified int64
}

// ChainVerificationError identifies the first invalid chain row.
type ChainVerificationError struct {
	Sequence int64
	Reason   string
}

func (e *ChainVerificationError) Error() string {
	return fmt.Sprintf(
		"audit chain invalid at row %d: %s", e.Sequence, e.Reason,
	)
}

// Verifies the hash chain over the requested inclusive sequence range.
func (s *AuditStore) VerifyChain(
	from, to int64,
) (VerificationResult, error) {
	var result VerificationResult

	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return result, fmt.Errorf("begin chain verification: %w", err)
	}
	defer tx.Rollback()

	var latest int64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM audit_chain`,
	).Scan(&latest); err != nil {
		return result, fmt.Errorf("read audit chain size: %w", err)
	}
	if from < 1 {
		return result, fmt.Errorf("from sequence must be >= 1")
	}
	if to == 0 {
		to = latest
	}
	if latest == 0 && to == 0 {
		return VerificationResult{}, nil
	}
	if to < from {
		return result, fmt.Errorf("to sequence must be >= from sequence")
	}
	if to > latest {
		return result, fmt.Errorf(
			"to sequence %d exceeds latest audit row %d", to, latest,
		)
	}

	prevHash := ""
	if from > 1 {
		err := tx.QueryRow(
			`SELECT entry_hash FROM audit_chain WHERE seq = ?`, from-1,
		).Scan(&prevHash)
		if err == sql.ErrNoRows {
			return result, &ChainVerificationError{
				Sequence: from,
				Reason:   "previous row is missing",
			}
		}
		if err != nil {
			return result, fmt.Errorf("read audit chain anchor: %w", err)
		}
	}

	result.From = from
	result.To = to
	expected := from
	for expected <= to {
		var row struct {
			seq        int64
			recordType string
			subjectID  string
			payload    string
			prevHash   string
			entryHash  string
		}
		err := tx.QueryRow(
			`SELECT seq, record_type, subject_id, payload, prev_hash,
			        entry_hash
			 FROM audit_chain WHERE seq = ?`, expected,
		).Scan(
			&row.seq, &row.recordType, &row.subjectID, &row.payload,
			&row.prevHash, &row.entryHash,
		)
		if err == sql.ErrNoRows {
			return result, &ChainVerificationError{
				Sequence: expected,
				Reason:   "row is missing",
			}
		}
		if err != nil {
			return result, fmt.Errorf("read audit chain row: %w", err)
		}
		if row.prevHash != prevHash {
			return result, &ChainVerificationError{
				Sequence: row.seq,
				Reason:   "previous hash does not match",
			}
		}
		var canonical struct {
			RecordType string          `json:"record_type"`
			SubjectID  string          `json:"subject_id"`
			Data       json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(row.payload), &canonical); err != nil ||
			canonical.RecordType != row.recordType ||
			canonical.SubjectID != row.subjectID {
			return result, &ChainVerificationError{
				Sequence: row.seq,
				Reason:   "canonical record metadata does not match",
			}
		}
		got := ComputeChainHash(row.payload, row.prevHash)
		if got != row.entryHash {
			return result, &ChainVerificationError{
				Sequence: row.seq,
				Reason:   "entry hash does not match",
			}
		}
		if err := verifySourceRow(
			tx, row.seq, row.recordType, row.subjectID,
			canonical.Data,
		); err != nil {
			return result, &ChainVerificationError{
				Sequence: row.seq,
				Reason:   err.Error(),
			}
		}
		prevHash = row.entryHash
		expected++
		result.Verified++
	}
	if from == 1 && to == latest {
		var stateSeq int64
		var stateHash string
		if err := tx.QueryRow(
			`SELECT seq, last_hash FROM audit_chain_state WHERE id = 1`,
		).Scan(&stateSeq, &stateHash); err != nil {
			return result, fmt.Errorf("read audit chain state: %w", err)
		}
		if stateSeq != latest || stateHash != prevHash {
			return result, &ChainVerificationError{
				Sequence: latest,
				Reason:   "chain state does not match final row",
			}
		}
	}

	return result, nil
}

// Cross-checks a chain row against its source table while the row
// still exists.
func verifySourceRow(
	q querier, seq int64, recordType, subjectID string,
	data json.RawMessage,
) error {
	switch recordType {
	case "tool_run":
		var expected AuditEntry
		if err := json.Unmarshal(data, &expected); err != nil {
			return fmt.Errorf("decode tool record: %w", err)
		}
		actual, err := getEntry(q, expected.ID)
		if err != nil {
			return err
		}
		if actual != nil && !sameJSON(expected, *actual) {
			return fmt.Errorf("tool audit row does not match chain payload")
		}
	case "permission_decided":
		var expected PermissionAudit
		if err := json.Unmarshal(data, &expected); err != nil {
			return fmt.Errorf("decode permission record: %w", err)
		}
		var actual PermissionAudit
		err := q.QueryRow(
			`SELECT id, session_id, COALESCE(input_id, ''),
			        COALESCE(execution_id, ''), COALESCE(approval_id, ''),
			        entry_id, tool, command, COALESCE(rule_pattern, ''),
			        COALESCE(rule_action, ''), decision,
			        COALESCE(reason, ''), decided_by, decision_source,
		        scope, decided_at
		 FROM audit_permissions WHERE id = ?`, expected.ID,
		).Scan(
			&actual.ID, &actual.SessionID, &actual.InputID,
			&actual.ExecutionID, &actual.ApprovalID, &actual.EntryID,
			&actual.Tool, &actual.Command, &actual.RulePattern,
			&actual.RuleAction, &actual.Decision, &actual.Reason,
			&actual.DecidedBy, &actual.DecisionSource, &actual.Scope,
			&actual.DecidedAt,
		)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("read permission row: %w", err)
		}
		if err == nil && !sameJSON(expected, actual) {
			return fmt.Errorf(
				"permission audit row does not match " +
					"chain payload",
			)
		}
	case "output_capture":
		var expected outputChainRecord
		if err := json.Unmarshal(data, &expected); err != nil {
			return fmt.Errorf("decode output record: %w", err)
		}
		actual, err := getOutput(q, expected.EntryID)
		if err != nil {
			return err
		}
		if actual != nil && (actual.ID != expected.ID ||
			actual.EntryID != expected.EntryID ||
			actual.SessionID != expected.SessionID ||
			ComputeChecksum(actual.Stdout) != expected.StdoutHash ||
			ComputeChecksum(actual.Stderr) != expected.StderrHash ||
			actual.StdoutSize != expected.StdoutSize ||
			actual.StderrSize != expected.StderrSize ||
			actual.Checksum != expected.Checksum) {
			return fmt.Errorf("output audit row does not match chain payload")
		}
	case "subagent_started", "subagent_finished":
		var expected SubagentAudit
		if err := json.Unmarshal(data, &expected); err != nil {
			return fmt.Errorf("decode subagent record: %w", err)
		}
		if recordType == "subagent_started" {
			var later int
			if err := q.QueryRow(
				`SELECT COUNT(*) FROM audit_chain
				 WHERE seq > ? AND record_type = 'subagent_finished'
				   AND subject_id = ?`, seq, subjectID,
			).Scan(&later); err != nil {
				return fmt.Errorf("check subagent history: %w", err)
			}
			if later > 0 {
				return nil
			}
		}
		actualRows, err := getSubagents(q, expected.ParentSessionID)
		if err != nil {
			return err
		}
		for _, actual := range actualRows {
			if actual.ChildSessionID == expected.ChildSessionID &&
				!sameJSON(expected, actual) {
				return fmt.Errorf("subagent row does not match chain payload")
			}
		}
	}

	return nil
}

// Reports whether two values encode to identical JSON.
func sameJSON(a, b any) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)

	return aErr == nil && bErr == nil && string(aJSON) == string(bJSON)
}

// Inserts a single audit entry and returns its ID.
func (s *AuditStore) InsertEntry(e AuditEntry) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin entry insert: %w", err)
	}
	defer tx.Rollback()

	id, err := insertEntryTx(tx, e)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit entry: %w", err)
	}

	return id, nil
}

// Inserts an entry and its optional output in one durable transaction.
func (s *AuditStore) InsertToolRun(
	e AuditEntry, output *OutputCapture,
) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tool run: %w", err)
	}
	defer tx.Rollback()

	id, err := insertEntryTx(tx, e)
	if err != nil {
		return err
	}
	if output != nil {
		output.EntryID = id
		if err := insertOutputTx(tx, *output); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tool run: %w", err)
	}

	return nil
}

// Inserts an entry and its chain record inside an existing transaction.
func insertEntryTx(tx *sql.Tx, e AuditEntry) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO audit_entries
		 (session_id, input_id, execution_id, message_id, tool_call_id,
		  tool, args, duration_ms, exit_code, error, output_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, e.InputID, e.ExecutionID, e.MessageID,
		e.ToolCallID, e.Tool, e.Args, e.DurationMs, e.ExitCode,
		e.Error, e.OutputHash,
	)
	if err != nil {
		return 0, fmt.Errorf("insert entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get entry id: %w", err)
	}
	e.ID = id
	if err := tx.QueryRow(
		`SELECT captured_at FROM audit_entries WHERE id = ?`, id,
	).Scan(&e.CapturedAt); err != nil {
		return 0, fmt.Errorf("read entry timestamp: %w", err)
	}
	if err := appendChainRecord(
		tx, "tool_run", e.ExecutionID, e,
	); err != nil {
		return 0, fmt.Errorf("chain entry: %w", err)
	}

	return id, nil
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
		 (session_id, input_id, execution_id, message_id, tool_call_id,
		  tool, args, duration_ms, exit_code, error, output_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		res, err := stmt.Exec(
			e.SessionID, e.InputID, e.ExecutionID, e.MessageID,
			e.ToolCallID, e.Tool, e.Args, e.DurationMs,
			e.ExitCode, e.Error, e.OutputHash,
		)
		if err != nil {
			return fmt.Errorf("insert entry in batch: %w", err)
		}
		e.ID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get batch entry id: %w", err)
		}
		if err := tx.QueryRow(
			`SELECT captured_at FROM audit_entries WHERE id = ?`, e.ID,
		).Scan(&e.CapturedAt); err != nil {
			return fmt.Errorf("read batch entry timestamp: %w", err)
		}
		if err := appendChainRecord(tx, "tool_run", e.ExecutionID, e); err != nil {
			return fmt.Errorf("chain batch entry: %w", err)
		}
	}

	return tx.Commit()
}

// Records a permission decision.
func (s *AuditStore) InsertPermission(p PermissionAudit) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin permission insert: %w", err)
	}
	defer tx.Rollback()

	id, err := insertPermissionTx(tx, p)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit permission: %w", err)
	}

	return id, nil
}

func insertPermissionTx(tx *sql.Tx, p PermissionAudit) (int64, error) {
	res, err := tx.Exec(
		`INSERT INTO audit_permissions
		 (session_id, input_id, execution_id, approval_id, entry_id,
		  tool, command, rule_pattern, rule_action, decision, reason,
		  decided_by, decision_source, scope)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SessionID, p.InputID, p.ExecutionID, p.ApprovalID,
		p.EntryID, p.Tool, p.Command, p.RulePattern, p.RuleAction,
		p.Decision, p.Reason, p.DecidedBy, p.DecisionSource,
		p.Scope,
	)
	if err != nil {
		return 0, fmt.Errorf("insert permission: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get permission id: %w", err)
	}
	p.ID = id
	if err := tx.QueryRow(
		`SELECT decided_at FROM audit_permissions WHERE id = ?`, id,
	).Scan(&p.DecidedAt); err != nil {
		return 0, fmt.Errorf("read permission timestamp: %w", err)
	}
	if err := appendChainRecord(
		tx, "permission_decided", p.ExecutionID, p,
	); err != nil {
		return 0, fmt.Errorf("chain permission: %w", err)
	}

	return id, nil
}

// Stores captured stdout/stderr for an entry.
func (s *AuditStore) InsertOutput(o OutputCapture) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin output insert: %w", err)
	}
	defer tx.Rollback()

	if err := insertOutputTx(tx, o); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit output: %w", err)
	}

	return nil
}

func insertOutputTx(tx *sql.Tx, o OutputCapture) error {
	res, err := tx.Exec(
		`INSERT INTO audit_output
		 (entry_id, session_id, stdout, stderr, stdout_size, stderr_size, checksum)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		o.EntryID, o.SessionID, o.Stdout, o.Stderr,
		o.StdoutSize, o.StderrSize, o.Checksum,
	)
	if err != nil {
		return fmt.Errorf("insert output: %w", err)
	}
	o.ID, err = res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get output id: %w", err)
	}
	if err := appendChainRecord(
		tx, "output_capture", fmt.Sprintf("%d", o.EntryID),
		outputChainRecord{
			ID: o.ID, EntryID: o.EntryID, SessionID: o.SessionID,
			StdoutHash: ComputeChecksum(o.Stdout),
			StderrHash: ComputeChecksum(o.Stderr),
			StdoutSize: o.StdoutSize, StderrSize: o.StderrSize,
			Checksum: o.Checksum,
		},
	); err != nil {
		return fmt.Errorf("chain output: %w", err)
	}

	return nil
}

// Records a subagent dispatch.
func (s *AuditStore) InsertSubagent(sa SubagentAudit) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin subagent insert: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO audit_subagents
		 (parent_session_id, child_session_id, parent_entry_id, tool, status)
		 VALUES (?, ?, ?, ?, ?)`,
		sa.ParentSessionID, sa.ChildSessionID,
		sa.ParentEntryID, sa.Tool, sa.Status,
	)
	if err != nil {
		return 0, fmt.Errorf("insert subagent: %w", err)
	}
	sa.ID, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get subagent id: %w", err)
	}
	if err := tx.QueryRow(
		`SELECT started_at FROM audit_subagents WHERE id = ?`, sa.ID,
	).Scan(&sa.StartedAt); err != nil {
		return 0, fmt.Errorf("read subagent timestamp: %w", err)
	}
	if err := appendChainRecord(
		tx, "subagent_started", sa.ChildSessionID, sa,
	); err != nil {
		return 0, fmt.Errorf("chain subagent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit subagent: %w", err)
	}

	return sa.ID, nil
}

// Updates a subagent record with completion info.
func (s *AuditStore) CompleteSubagent(
	childSessionID string, durationMs int64, status string,
) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin subagent completion: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE audit_subagents
		 SET status = ?, duration_ms = ?, completed_at = datetime('now')
		 WHERE child_session_id = ?`,
		status, durationMs, childSessionID,
	)
	if err != nil {
		return fmt.Errorf("complete subagent: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read subagent completion: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("subagent %q not found", childSessionID)
	}
	var sa SubagentAudit
	var completedAt string
	if err := tx.QueryRow(
		`SELECT id, parent_session_id, child_session_id, parent_entry_id,
		        tool, status, duration_ms, started_at, completed_at
		 FROM audit_subagents WHERE child_session_id = ?`, childSessionID,
	).Scan(
		&sa.ID, &sa.ParentSessionID, &sa.ChildSessionID,
		&sa.ParentEntryID, &sa.Tool, &sa.Status,
		&sa.DurationMs, &sa.StartedAt, &completedAt,
	); err != nil {
		return fmt.Errorf("read completed subagent: %w", err)
	}
	sa.CompletedAt = &completedAt
	if err := appendChainRecord(
		tx, "subagent_finished", sa.ChildSessionID, sa,
	); err != nil {
		return fmt.Errorf("chain subagent completion: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subagent completion: %w", err)
	}

	return nil
}

// Returns entries matching the given filter.
func (s *AuditStore) QueryEntries(f QueryFilter) ([]AuditEntry, error) {
	query := `SELECT id, session_id, COALESCE(input_id, ''),
	                 COALESCE(execution_id, ''), COALESCE(message_id, ''),
	                 tool_call_id, tool, args, duration_ms, exit_code,
	                 COALESCE(error, ''), COALESCE(output_hash, ''),
	                 captured_at
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
	if f.ExecutionID != "" {
		query += " AND execution_id = ?"
		args = append(args, f.ExecutionID)
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
			&e.ID, &e.SessionID, &e.InputID, &e.ExecutionID,
			&e.MessageID, &e.ToolCallID, &e.Tool, &e.Args,
			&e.DurationMs, &e.ExitCode, &e.Error, &e.OutputHash,
			&e.CapturedAt,
		); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, rows.Err()
}

// Returns permission decisions matching the filter.
func (s *AuditStore) QueryPermissions(f QueryFilter) ([]PermissionAudit, error) {
	query := `SELECT id, session_id, COALESCE(input_id, ''),
	                 COALESCE(execution_id, ''), COALESCE(approval_id, ''),
	                 entry_id, tool, command, COALESCE(rule_pattern, ''),
	                 COALESCE(rule_action, ''), decision,
	                 COALESCE(reason, ''), decided_by, decision_source,
	                 scope, decided_at
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
	if f.ExecutionID != "" {
		query += " AND execution_id = ?"
		args = append(args, f.ExecutionID)
	}
	if f.ApprovalID != "" {
		query += " AND approval_id = ?"
		args = append(args, f.ApprovalID)
	}
	if !f.Since.IsZero() {
		query += " AND decided_at >= ?"
		args = append(args, f.Since.UTC().Format("2006-01-02 15:04:05"))
	}
	if !f.Before.IsZero() {
		query += " AND decided_at <= ?"
		args = append(args, f.Before.UTC().Format("2006-01-02 15:04:05"))
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
			&p.ID, &p.SessionID, &p.InputID, &p.ExecutionID,
			&p.ApprovalID, &p.EntryID, &p.Tool, &p.Command,
			&p.RulePattern, &p.RuleAction, &p.Decision, &p.Reason,
			&p.DecidedBy, &p.DecisionSource, &p.Scope, &p.DecidedAt,
		); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}

	return perms, rows.Err()
}

// Returns a single entry by ID.
func (s *AuditStore) GetEntry(id int64) (*AuditEntry, error) {
	return getEntry(s.db, id)
}

func getEntry(q querier, id int64) (*AuditEntry, error) {
	var e AuditEntry
	err := q.QueryRow(
		`SELECT id, session_id, COALESCE(input_id, ''),
		        COALESCE(execution_id, ''), COALESCE(message_id, ''),
		        tool_call_id, tool, args, duration_ms, exit_code,
		        COALESCE(error, ''), COALESCE(output_hash, ''), captured_at
		 FROM audit_entries WHERE id = ?`, id,
	).Scan(
		&e.ID, &e.SessionID, &e.InputID, &e.ExecutionID,
		&e.MessageID, &e.ToolCallID, &e.Tool, &e.Args,
		&e.DurationMs, &e.ExitCode, &e.Error, &e.OutputHash,
		&e.CapturedAt,
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
	return getOutput(s.db, entryID)
}

func getOutput(q querier, entryID int64) (*OutputCapture, error) {
	var o OutputCapture
	err := q.QueryRow(
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
	query := `SELECT id, session_id, COALESCE(input_id, ''),
	                 COALESCE(execution_id, ''), COALESCE(message_id, ''),
	                 tool_call_id, tool, args, duration_ms, exit_code,
	                 COALESCE(error, ''), COALESCE(output_hash, ''),
	                 captured_at
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
			&e.ID, &e.SessionID, &e.InputID, &e.ExecutionID,
			&e.MessageID, &e.ToolCallID, &e.Tool, &e.Args,
			&e.DurationMs, &e.ExitCode, &e.Error, &e.OutputHash,
			&e.CapturedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, rows.Err()
}

// Returns all subagent records for a parent session.
func (s *AuditStore) GetSubagents(parentSessionID string) ([]SubagentAudit, error) {
	return getSubagents(s.db, parentSessionID)
}

func getSubagents(
	q querier, parentSessionID string,
) ([]SubagentAudit, error) {
	rows, err := q.Query(
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
		"id", "session_id", "input_id", "message_id",
		"tool_call_id", "execution_id", "tool", "args",
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
			e.InputID,
			e.MessageID,
			e.ToolCallID,
			e.ExecutionID,
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

// Appends canonical payload data as the next row in the hash chain.
func appendChainRecord(
	tx *sql.Tx, recordType, subjectID string, data any,
) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode chain payload: %w", err)
	}
	payload, err := json.Marshal(struct {
		RecordType string          `json:"record_type"`
		SubjectID  string          `json:"subject_id"`
		Data       json.RawMessage `json:"data"`
	}{recordType, subjectID, dataJSON})
	if err != nil {
		return fmt.Errorf("encode canonical chain payload: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE audit_chain_state SET seq = seq WHERE id = 1`,
	); err != nil {
		return fmt.Errorf("lock audit chain state: %w", err)
	}

	var seq int64
	var prevHash string
	if err := tx.QueryRow(
		`SELECT seq, last_hash FROM audit_chain_state WHERE id = 1`,
	).Scan(&seq, &prevHash); err != nil {
		return fmt.Errorf("read audit chain state: %w", err)
	}
	seq++
	entryHash := ComputeChainHash(string(payload), prevHash)
	if _, err := tx.Exec(
		`INSERT INTO audit_chain
		 (seq, record_type, subject_id, payload, prev_hash, entry_hash)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		seq, recordType, subjectID, string(payload), prevHash, entryHash,
	); err != nil {
		return fmt.Errorf("insert audit chain row: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE audit_chain_state SET seq = ?, last_hash = ? WHERE id = 1`,
		seq, entryHash,
	); err != nil {
		return fmt.Errorf("update audit chain state: %w", err)
	}

	return nil
}

// Returns a SHA256 hex string of canonical payload plus its previous hash.
func ComputeChainHash(payload, prevHash string) string {
	h := sha256.New()
	h.Write([]byte(payload))
	h.Write([]byte(prevHash))

	return fmt.Sprintf("%x", h.Sum(nil))
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
