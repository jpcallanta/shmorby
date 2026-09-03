package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"shmorby/internal/audit"
	"shmorby/internal/ledger"
	"shmorby/internal/redact"
)

// ── ledger_get ─────────────────────────────────────────────

var ledgerGetParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"section": {
			"type": "string",
			"description": "ledger section name (e.g. hosts, roles, incidents, tickets)"
		}
	},
	"required": ["section"]
}`)

type ledgerGetArgs struct {
	Section string `json:"section"`
}

// LedgerGetTool reads a section from the encrypted environment
// ledger. Read-only; available in diagnose mode.
type LedgerGetTool struct {
	perm        string
	auditLogger *audit.Logger
}

// Creates a LedgerGetTool with the given permission level.
func NewLedgerGetTool(permLevel string) *LedgerGetTool {

	return &LedgerGetTool{perm: permLevel}
}

// Returns the tool name.
func (t *LedgerGetTool) Name() string { return "ledger_get" }

// Returns the LLM description.
func (t *LedgerGetTool) Description() string {

	return "Read a section from the encrypted environment " +
		"ledger. Returns the section JSON or an error if " +
		"the section does not exist. Available in diagnose " +
		"mode for consulting known-good state."
}

// Returns the JSON schema for parameters.
func (t *LedgerGetTool) Parameters() json.RawMessage {

	return ledgerGetParams
}

// PermLevel returns the configured permission level.
func (t *LedgerGetTool) PermLevel() string { return t.perm }

// SetPerm updates the permission level at runtime.
func (t *LedgerGetTool) SetPerm(level string) { t.perm = level }

// SetAuditLogger sets the audit logger for this tool.
func (t *LedgerGetTool) SetAuditLogger(l *audit.Logger) { t.auditLogger = l }

// Opens the ledger, reads the section, and closes. Errors are
// non-fatal (returned as output text) so the agent continues.
func (t *LedgerGetTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {

	start := time.Now()

	var a ledgerGetArgs
	if err := json.Unmarshal(args, &a); err != nil {

		return "", fmt.Errorf(
			"invalid ledger_get args: %w - "+
				`expected {"section":"<name>"}`,
			err,
		)
	}

	if err := ledger.ValidateSection(a.Section); err != nil {

		return "", fmt.Errorf("invalid section: %w", err)
	}

	l, err := ledger.Open()
	if err != nil {

		// Non-fatal: agent continues.
		slog.Warn("ledger_get: open failed", "err", err)

		return "error: ledger unavailable: " + err.Error(), nil
	}
	defer l.Close()

	data, ok := l.Get(a.Section)
	if !ok {

		return fmt.Sprintf(
			"section %q not found", a.Section,
		), nil
	}

	// Pretty-print for LLM readability.
	var pretty json.RawMessage = data
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {

		return "", fmt.Errorf("format JSON: %w", err)
	}

	slog.Info("tool run",
		"tool", "ledger_get",
		"section", a.Section,
		"bytes", len(out),
	)

	if t.auditLogger != nil {
		exitCode := 0
		t.auditLogger.LogToolRun(
			audit.AuditEntry{
				SessionID:  SessionIDFrom(ctx),
				Tool:       "ledger_get",
				Args:       a.Section,
				DurationMs: time.Since(start).Milliseconds(),
				ExitCode:   &exitCode,
			},
			&audit.OutputCapture{
				SessionID:  SessionIDFrom(ctx),
				Stdout:     string(out),
				StdoutSize: len(out),
			},
		)
	}

	return string(out), nil
}

// ── ledger_set ─────────────────────────────────────────────

var ledgerSetParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"section": {
			"type": "string",
			"description": "ledger section name (e.g. hosts, roles, incidents, tickets)"
		},
		"data": {
			"type": "object",
			"description": "JSON object to store in the section"
		}
	},
	"required": ["section", "data"]
}`)

type ledgerSetArgs struct {
	Section string          `json:"section"`
	Data    json.RawMessage `json:"data"`
}

// LedgerSetTool writes a section to the encrypted environment
// ledger. Mutating; operate-level permission. Payloads are
// redacted before storage to prevent secret leakage.
type LedgerSetTool struct {
	perm        string
	auditLogger *audit.Logger
}

// Creates a LedgerSetTool with the given permission level.
func NewLedgerSetTool(permLevel string) *LedgerSetTool {

	return &LedgerSetTool{perm: permLevel}
}

// Returns the tool name.
func (t *LedgerSetTool) Name() string { return "ledger_set" }

// Returns the LLM description.
func (t *LedgerSetTool) Description() string {

	return "Write a section to the encrypted environment " +
		"ledger. The data is redacted for secrets before " +
		"storage. Use this to record discovered facts " +
		"(hosts, roles, incidents, tickets) so they " +
		"persist across sessions. Mutating operation; " +
		"subject to permission rules."
}

// Returns the JSON schema for parameters.
func (t *LedgerSetTool) Parameters() json.RawMessage {

	return ledgerSetParams
}

// PermLevel returns the configured permission level.
func (t *LedgerSetTool) PermLevel() string { return t.perm }

// SetPerm updates the permission level at runtime.
func (t *LedgerSetTool) SetPerm(level string) { t.perm = level }

// SetAuditLogger sets the audit logger for this tool.
func (t *LedgerSetTool) SetAuditLogger(l *audit.Logger) { t.auditLogger = l }

// Validates, redacts, enforces size caps, then opens the ledger,
// writes, and closes. The lock is held only for the duration of
// this call so concurrent CLI access is not blocked.
func (t *LedgerSetTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {

	start := time.Now()

	var a ledgerSetArgs
	if err := json.Unmarshal(args, &a); err != nil {

		return "", fmt.Errorf(
			"invalid ledger_set args: %w - "+
				`expected {"section":"<name>","data":{...}}`,
			err,
		)
	}

	if err := ledger.ValidateSection(a.Section); err != nil {

		return "", fmt.Errorf("invalid section: %w", err)
	}

	// Validate JSON.
	if !json.Valid(a.Data) {

		return "", fmt.Errorf("invalid JSON in data field")
	}

	// Redact secrets before storage by walking the decoded JSON
	// tree: values under secret-named keys (password, api_key,
	// secret, token, ...) are replaced with "[REDACTED]" and
	// remaining string values are pattern-redacted. Unlike
	// regex-on-text redaction the output stays valid JSON, and
	// numeric precision is preserved. Defense in depth: the
	// ledger is encrypted at rest, but redaction ensures that
	// even if the key is compromised, secrets are not stored.
	redacted, err := redact.JSONData(a.Data)
	if err != nil {

		return "", fmt.Errorf("redact data: %w", err)
	}

	canonical := json.RawMessage(redacted)

	// Open the ledger to check section count for cap enforcement.
	l, err := ledger.Open()
	if err != nil {

		slog.Warn("ledger_set: open failed", "err", err)

		return "error: ledger unavailable: " + err.Error(), nil
	}
	defer l.Close()

	// Check size caps. If the section already exists, we are
	// replacing it, so the section count does not increase.
	existingCount := len(l.Sections())
	_, exists := l.Get(a.Section)
	countForCheck := existingCount
	if exists {

		// Replacing existing section; count stays the same.
		countForCheck = existingCount - 1
	}

	if err := ledger.ValidateData(canonical, countForCheck); err != nil {

		return "", fmt.Errorf("ledger cap exceeded: %w", err)
	}

	l.Set(a.Section, canonical)

	slog.Info("tool run",
		"tool", "ledger_set",
		"section", a.Section,
		"bytes", len(canonical),
	)

	// Audit the mutation. Args carry the already-redacted payload,
	// so no plaintext secret reaches the audit store.
	if t.auditLogger != nil {
		exitCode := 0
		t.auditLogger.LogToolRun(
			audit.AuditEntry{
				SessionID:  SessionIDFrom(ctx),
				Tool:       "ledger_set",
				Args:       fmt.Sprintf("section=%s data=%s", a.Section, redacted),
				DurationMs: time.Since(start).Milliseconds(),
				ExitCode:   &exitCode,
			},
			nil,
		)
	}

	return fmt.Sprintf(
		"section %q written (%d bytes)", a.Section, len(canonical),
	), nil
}
