package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"shmorby/internal/execution"
	"shmorby/internal/redact"
)

// Returns a persisted user input by idempotency key.
func (s *sqliteStore) FindInput(
	sessionID, inputID string,
) (Message, bool, error) {
	if inputID == "" {
		return Message{}, false, nil
	}

	row := s.db.QueryRow(
		`SELECT role, content, COALESCE(tool_name, ''),
		        COALESCE(tool_call_id, ''), COALESCE(tool_calls, ''),
		        COALESCE(images, ''), COALESCE(input_id, '')
		 FROM session_messages
		 WHERE session_id = ? AND input_id = ?
		 LIMIT 1`, sessionID, inputID,
	)

	var (
		m         Message
		toolCalls string
		images    string
	)
	err := row.Scan(
		&m.Role, &m.Content, &m.ToolName, &m.ToolCallID,
		&toolCalls, &images, &m.InputID,
	)
	if err == sql.ErrNoRows {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, fmt.Errorf("find input: %w", err)
	}

	if toolCalls != "" {
		if err := json.Unmarshal(
			[]byte(toolCalls), &m.ToolCalls,
		); err != nil {
			return Message{}, false, fmt.Errorf(
				"decode tool_calls for input %s: %w", inputID, err,
			)
		}
	}
	if images != "" {
		if err := json.Unmarshal(
			[]byte(images), &m.Images,
		); err != nil {
			slog.Warn("decode input images failed; skipping",
				"session", sessionID, "input", inputID,
				"err", err)
		}
	}

	return m, true, nil
}

// Inserts a durable execution record.
func (s *sqliteStore) CreateExecution(
	r execution.Record,
) error {
	r = redactExecution(r)

	_, err := s.db.Exec(
		`INSERT INTO executions
		 (id, session_id, message_id, tool_call_id, tool, command,
		  args_redacted, args_hash, status, pid, owner_pid,
		  owner_start, process_start, created_at, started_at,
		  ended_at, exit_code, error, output_ref, approval_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		         ?, ?, ?, ?)`,
		r.ID, r.SessionID, r.MessageID, r.ToolCallID, r.Tool,
		r.Command, r.ArgsRedacted, r.ArgsHash, string(r.Status),
		r.PID, r.OwnerPID, r.OwnerStart, r.ProcessStart,
		unixOrZero(r.CreatedAt), nullableUnix(r.StartedAt),
		nullableUnix(r.EndedAt), r.ExitCode, r.Error,
		r.OutputRef, r.ApprovalID,
	)
	if err != nil {
		return fmt.Errorf("create execution %s: %w", r.ID, err)
	}

	return nil
}

// Updates an existing execution record.
func (s *sqliteStore) UpdateExecution(
	r execution.Record,
) error {
	r = redactExecution(r)

	res, err := s.db.Exec(
		`UPDATE executions SET
		  status = ?, pid = ?, process_start = ?, started_at = ?,
		  ended_at = ?, exit_code = ?, error = ?, output_ref = ?
		 WHERE id = ?`,
		string(r.Status), r.PID, r.ProcessStart,
		nullableUnix(r.StartedAt), nullableUnix(r.EndedAt),
		r.ExitCode, r.Error, r.OutputRef, r.ID,
	)
	if err != nil {
		return fmt.Errorf("update execution %s: %w", r.ID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update execution %s: %w", r.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: execution %s", ErrNotFound, r.ID)
	}

	return nil
}

// Column projection shared by execution reads.
const executionColumns = `id, session_id, message_id, tool_call_id,
	tool, command, args_redacted, args_hash, status, pid, owner_pid,
	owner_start, process_start, created_at, started_at, ended_at,
	exit_code, error, output_ref, approval_id`

// Returns one execution by id.
func (s *sqliteStore) GetExecution(
	id string,
) (execution.Record, error) {
	row := s.db.QueryRow(
		`SELECT `+executionColumns+` FROM executions WHERE id = ?`,
		id,
	)

	r, err := scanExecution(row)
	if err == sql.ErrNoRows {
		return execution.Record{}, fmt.Errorf(
			"%w: execution %s", execution.ErrNotFound, id,
		)
	}
	if err != nil {
		return execution.Record{}, fmt.Errorf(
			"get execution %s: %w", id, err,
		)
	}

	return r, nil
}

// Returns executions matching f, newest first.
func (s *sqliteStore) ListExecutions(
	f execution.Filter,
) ([]execution.Record, error) {
	query := `SELECT ` + executionColumns + ` FROM executions`
	var (
		args []any
		cond []string
	)

	if f.SessionID != "" {
		cond = append(cond, "session_id = ?")
		args = append(args, f.SessionID)
	}
	if len(f.Statuses) > 0 {
		placeholders := make([]string, len(f.Statuses))
		for i, status := range f.Statuses {
			placeholders[i] = "?"
			args = append(args, string(status))
		}
		cond = append(cond, "status IN ("+
			strings.Join(placeholders, ", ")+")")
	}
	if len(cond) > 0 {
		query += " WHERE " + strings.Join(cond, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	var out []execution.Record
	for rows.Next() {
		r, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		out = append(out, r)
	}

	return out, rows.Err()
}

// Scans one execution row.
func scanExecution(row interface {
	Scan(dest ...any) error
}) (execution.Record, error) {
	var (
		r         execution.Record
		status    string
		createdAt int64
		startedAt sql.NullInt64
		endedAt   sql.NullInt64
		exitCode  sql.NullInt64
	)

	err := row.Scan(
		&r.ID, &r.SessionID, &r.MessageID, &r.ToolCallID, &r.Tool,
		&r.Command, &r.ArgsRedacted, &r.ArgsHash, &status, &r.PID,
		&r.OwnerPID, &r.OwnerStart, &r.ProcessStart, &createdAt,
		&startedAt, &endedAt, &exitCode, &r.Error, &r.OutputRef,
		&r.ApprovalID,
	)
	if err != nil {
		return execution.Record{}, err
	}

	r.Status = execution.Status(status)
	r.Command = redact.SecretString(r.Command)
	r.ArgsRedacted = redact.SecretString(r.ArgsRedacted)
	r.Error = redact.SecretString(r.Error)
	r.CreatedAt = time.Unix(createdAt, 0).UTC()
	if startedAt.Valid {
		r.StartedAt = time.Unix(startedAt.Int64, 0).UTC()
	}
	if endedAt.Valid {
		r.EndedAt = time.Unix(endedAt.Int64, 0).UTC()
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		r.ExitCode = &code
	}

	return r, nil
}

// Replaces the paginated output for an execution.
func (s *sqliteStore) AppendExecutionOutput(
	id string, chunks []string,
) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin execution output: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM execution_output WHERE execution_id = ?`, id,
	); err != nil {
		return fmt.Errorf("clear execution output %s: %w", id, err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO execution_output
		 (execution_id, chunk, data) VALUES (?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare execution output: %w", err)
	}
	defer stmt.Close()

	for i, chunk := range chunks {
		if _, err := stmt.Exec(
			id, i, redact.SecretString(chunk),
		); err != nil {
			return fmt.Errorf(
				"insert execution output %s:%d: %w", id, i, err,
			)
		}
	}

	return tx.Commit()
}

// Returns one output chunk and whether it is the last.
func (s *sqliteStore) ReadExecutionOutput(
	id string, cursor int,
) (string, bool, error) {
	var exists int
	err := s.db.QueryRow(
		`SELECT 1 FROM executions WHERE id = ?`, id,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return "", false, fmt.Errorf(
			"%w: execution %s", execution.ErrNotFound, id,
		)
	}
	if err != nil {
		return "", false, fmt.Errorf(
			"read execution %s: %w", id, err,
		)
	}

	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM execution_output
		 WHERE execution_id = ?`, id,
	).Scan(&total); err != nil {
		return "", false, fmt.Errorf(
			"count execution output %s: %w", id, err,
		)
	}
	if cursor >= total {
		return "", true, nil
	}

	var data string
	err = s.db.QueryRow(
		`SELECT data FROM execution_output
		 WHERE execution_id = ? AND chunk = ?`, id, cursor,
	).Scan(&data)
	if err != nil {
		return "", false, fmt.Errorf(
			"read execution output %s:%d: %w", id, cursor, err,
		)
	}

	return data, cursor == total-1, nil
}

// Returns the assistant reply persisted after a user input.
func (s *sqliteStore) FindInputReply(
	sessionID, inputID string,
) (Message, bool, error) {
	if inputID == "" {
		return Message{}, false, nil
	}

	rows, err := s.db.Query(
		`SELECT role, content, COALESCE(tool_calls, '')
		 FROM session_messages
		 WHERE session_id = ? AND seq > (
		     SELECT seq FROM session_messages
		     WHERE session_id = ? AND input_id = ?
		     LIMIT 1)
		 ORDER BY seq`, sessionID, sessionID, inputID,
	)
	if err != nil {
		return Message{}, false, fmt.Errorf(
			"find input reply: %w", err,
		)
	}
	defer rows.Close()

	var (
		reply Message
		found bool
	)

	for rows.Next() {
		var (
			role      string
			content   string
			toolCalls string
		)
		if err := rows.Scan(
			&role, &content, &toolCalls,
		); err != nil {
			return Message{}, false, fmt.Errorf(
				"scan input reply: %w", err,
			)
		}
		if role == "user" {
			break
		}
		if role == "assistant" && toolCalls == "" &&
			content != "" {
			reply = Message{Role: role, Content: content}
			found = true
		}
	}

	return reply, found, rows.Err()
}

// Redacts secrets from persisted execution fields.
func redactExecution(r execution.Record) execution.Record {
	r.Command = redact.SecretString(r.Command)
	r.ArgsRedacted = redact.SecretString(r.ArgsRedacted)
	r.Error = redact.SecretString(r.Error)

	return r
}

// Converts t to Unix seconds, or 0 for the zero time.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UTC().Unix()
}

// Converts t to nullable Unix seconds.
func nullableUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}

	return t.UTC().Unix()
}
