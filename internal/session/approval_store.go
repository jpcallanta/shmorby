package session

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"shmorby/internal/execution"
	"shmorby/internal/redact"
)

// ApprovalQuery restricts approval listings.
type ApprovalQuery struct {
	SessionID string
	Status    string
	Limit     int
}

// ApprovalRecord is a durable request and its decision.
type ApprovalRecord struct {
	ID              string
	SessionID       string
	ExecutionID     string
	Tool            string
	CommandRedacted string
	RulePattern     string
	RuleAction      string
	Reason          string
	Status          string
	Decision        string
	Scope           string
	DecidedBy       string
	DecisionSource  string
	RequestedAt     time.Time
	DecidedAt       time.Time
	ExpiresAt       time.Time
}

// ErrApprovalNotPending is returned when a decision races with a
// previously resolved or expired request.
var ErrApprovalNotPending = errors.New("approval is not pending")

// Inserts an approval request and links it to its execution atomically.
func (s *sqliteStore) CreateApproval(a ApprovalRecord) error {
	if a.ExecutionID == "" {
		return errors.New("approval execution ID is required")
	}
	a.CommandRedacted, a.Reason = redact.PermissionFields(
		a.CommandRedacted, a.Reason,
	)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin approval create: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO approvals
		 (id, session_id, execution_id, tool, command_redacted,
		  rule_pattern, rule_action, reason, status, decision, scope,
		  decided_by, decision_source, requested_at, decided_at,
		  expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.ExecutionID, a.Tool,
		a.CommandRedacted, a.RulePattern, a.RuleAction, a.Reason,
		a.Status, a.Decision, a.Scope, a.DecidedBy,
		a.DecisionSource, a.RequestedAt.UTC().Unix(),
		nullableUnix(a.DecidedAt), a.ExpiresAt.UTC().Unix(),
	)
	if err != nil {
		return fmt.Errorf("insert approval %s: %w", a.ID, err)
	}

	res, err := tx.Exec(
		`UPDATE executions SET approval_id = ? WHERE id = ?`,
		a.ID, a.ExecutionID,
	)
	if err != nil {
		return fmt.Errorf("link approval %s: %w", a.ID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("link approval %s: %w", a.ID, err)
	} else if n == 0 {
		return fmt.Errorf("link approval %s: execution %s not found",
			a.ID, a.ExecutionID)
	}

	return tx.Commit()
}

// Persists a decision and updates its execution in one transaction.
func (s *sqliteStore) ResolveApproval(a ApprovalRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin approval resolve: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE approvals SET status = ?, decision = ?, scope = ?,
		 decided_by = ?, decision_source = ?, decided_at = ?
		 WHERE id = ? AND status = 'pending'`,
		a.Status, a.Decision, a.Scope, a.DecidedBy,
		a.DecisionSource, a.DecidedAt.UTC().Unix(), a.ID,
	)
	if err != nil {
		return fmt.Errorf("resolve approval %s: %w", a.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve approval %s: %w", a.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrApprovalNotPending, a.ID)
	}

	status := "running"
	if a.Decision == "reject" {
		status = "denied"
	}
	_, err = tx.Exec(
		`UPDATE executions SET status = ?,
		 started_at = CASE WHEN ? = 'running' THEN COALESCE(started_at, ?)
		                   ELSE started_at END,
		 ended_at = CASE WHEN ? = 'denied' THEN ? ELSE ended_at END,
		 error = CASE WHEN ? = 'denied' THEN 'permission denied' ELSE error END
		 WHERE id = ?`,
		status, status, a.DecidedAt.UTC().Unix(), status,
		a.DecidedAt.UTC().Unix(), status, a.ExecutionID,
	)
	if err != nil {
		return fmt.Errorf("update execution for approval %s: %w", a.ID, err)
	}

	return tx.Commit()
}

// Expires one pending approval and denies its execution atomically.
func (s *sqliteStore) ExpireApproval(
	id, source string, at time.Time,
) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin approval expiry: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE approvals SET status = 'expired', decision = 'reject',
		 scope = 'call', decided_by = 'system', decision_source = ?,
		 decided_at = ? WHERE id = ? AND status = 'pending'`,
		source, at.UTC().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("expire approval %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("expire approval %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrApprovalNotPending, id)
	}

	_, err = tx.Exec(
		`UPDATE executions SET status = 'denied', ended_at = ?,
		 error = 'approval expired' WHERE approval_id = ?`,
		at.UTC().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("deny expired execution %s: %w", id, err)
	}

	return tx.Commit()
}

// Expires pending approvals whose owning process is no longer alive.
func (s *sqliteStore) ExpirePendingApprovals(at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin stale approval recovery: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT approvals.id, executions.id,
		        executions.owner_pid, executions.owner_start
		 FROM approvals
		 LEFT JOIN executions ON executions.id = approvals.execution_id
		 WHERE approvals.status = 'pending'`,
	)
	if err != nil {
		return fmt.Errorf("list pending approvals: %w", err)
	}

	type pendingApproval struct {
		id          string
		executionID string
		ownerPID    int
		ownerStart  string
	}
	var stale []pendingApproval
	for rows.Next() {
		var p pendingApproval
		if err := rows.Scan(
			&p.id, &p.executionID, &p.ownerPID, &p.ownerStart,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan pending approval: %w", err)
		}
		if p.ownerPID > 0 && p.ownerStart != "" &&
			execution.ProcessIdentity(p.ownerPID) == p.ownerStart {
			continue
		}
		stale = append(stale, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read pending approvals: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close pending approvals: %w", err)
	}

	for _, p := range stale {
		res, err := tx.Exec(
			`UPDATE approvals SET status = 'expired', decision = 'reject',
			 scope = 'call', decided_by = 'system',
			 decision_source = 'restart', decided_at = ?
			 WHERE id = ? AND status = 'pending'`,
			at.UTC().Unix(), p.id,
		)
		if err != nil {
			return fmt.Errorf("expire stale approval %s: %w", p.id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("check stale approval %s: %w", p.id, err)
		}
		if n == 0 {
			continue
		}
		if _, err := tx.Exec(
			`UPDATE executions SET status = 'denied', ended_at = ?,
			 error = 'approval expired after restart'
			 WHERE id = ? AND status = 'pending_approval'
			 AND approval_id = ?`,
			at.UTC().Unix(), p.executionID, p.id,
		); err != nil {
			return fmt.Errorf("deny stale approval execution %s: %w", p.id, err)
		}
	}

	return tx.Commit()
}

// Returns an approval by ID. Used by tests and approval listings.
func (s *sqliteStore) GetApproval(id string) (ApprovalRecord, error) {
	var a ApprovalRecord
	var requestedAt, decidedAt, expiresAt sql.NullInt64
	err := s.db.QueryRow(
		`SELECT id, session_id, execution_id, tool, command_redacted,
		 rule_pattern, rule_action, reason, status, decision, scope,
		 decided_by, decision_source, requested_at, decided_at,
		 expires_at FROM approvals WHERE id = ?`, id,
	).Scan(
		&a.ID, &a.SessionID, &a.ExecutionID, &a.Tool,
		&a.CommandRedacted, &a.RulePattern, &a.RuleAction, &a.Reason,
		&a.Status, &a.Decision, &a.Scope, &a.DecidedBy,
		&a.DecisionSource, &requestedAt, &decidedAt, &expiresAt,
	)
	if err != nil {
		return ApprovalRecord{}, fmt.Errorf("get approval %s: %w", id, err)
	}
	a.RequestedAt = timeFromNull(requestedAt)
	a.DecidedAt = timeFromNull(decidedAt)
	a.ExpiresAt = timeFromNull(expiresAt)

	return a, nil
}

// Returns approvals matching query, newest first.
func (s *sqliteStore) ListApprovals(
	query ApprovalQuery,
) ([]ApprovalRecord, error) {
	sqlQuery := `SELECT id, session_id, execution_id, tool,
		command_redacted, rule_pattern, rule_action, reason, status,
		decision, scope, decided_by, decision_source, requested_at,
		decided_at, expires_at FROM approvals WHERE 1 = 1`
	var args []any
	if query.SessionID != "" {
		sqlQuery += " AND session_id = ?"
		args = append(args, query.SessionID)
	}
	if query.Status != "" {
		sqlQuery += " AND status = ?"
		args = append(args, query.Status)
	}
	sqlQuery += " ORDER BY requested_at DESC"
	if query.Limit > 0 {
		sqlQuery += " LIMIT ?"
		args = append(args, query.Limit)
	}
	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()

	var out []ApprovalRecord
	for rows.Next() {
		var a ApprovalRecord
		var requestedAt, decidedAt, expiresAt sql.NullInt64
		if err := rows.Scan(
			&a.ID, &a.SessionID, &a.ExecutionID, &a.Tool,
			&a.CommandRedacted, &a.RulePattern, &a.RuleAction, &a.Reason,
			&a.Status, &a.Decision, &a.Scope, &a.DecidedBy,
			&a.DecisionSource, &requestedAt, &decidedAt, &expiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan approval: %w", err)
		}
		a.RequestedAt = timeFromNull(requestedAt)
		a.DecidedAt = timeFromNull(decidedAt)
		a.ExpiresAt = timeFromNull(expiresAt)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read approvals: %w", err)
	}

	return out, nil
}

// Converts an optional Unix timestamp to a UTC time.
func timeFromNull(v sql.NullInt64) time.Time {
	if !v.Valid || v.Int64 == 0 {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0).UTC()
}
