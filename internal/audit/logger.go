package audit

import (
	"log/slog"
	"sync"
	"time"

	"shmorby/internal/redact"
)

// Logger buffers audit writes and flushes them to the store
// asynchronously. Safe for concurrent use.
type Logger struct {
	store    *AuditStore
	maxBytes int
	flushCh  chan auditEvent
	done     chan struct{}
	once     sync.Once
	flushWg  sync.WaitGroup
}

type auditEvent struct {
	entry       *AuditEntry
	toolRun     *toolRunEvent
	permission  *PermissionAudit
	subagent    *SubagentAudit
	completeSub *subagentComplete
}

// toolRunEvent pairs an entry with its captured output so the
// flush loop can insert the entry first, capture its auto-incremented
// ID, and then insert the output with the correct FK.
type toolRunEvent struct {
	entry  AuditEntry
	output *OutputCapture
}

type subagentComplete struct {
	childSessionID string
	durationMs     int64
	status         string
}

// NewLogger creates an async audit logger. bufferSize controls the
// channel capacity; flushInterval controls the periodic flush.
// maxBytes is the per-output truncation limit.
func NewLogger(store *AuditStore, bufferSize int, flushInterval time.Duration, maxBytes int) *Logger {
	if bufferSize < 1 {
		bufferSize = 100
	}
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond
	}

	l := &Logger{
		store:    store,
		maxBytes: maxBytes,
		flushCh:  make(chan auditEvent, bufferSize),
		done:     make(chan struct{}),
	}

	l.flushWg.Add(1)
	go l.flushLoop(flushInterval)

	return l
}

// flushLoop drains the buffer channel into batched store writes.
func (l *Logger) flushLoop(interval time.Duration) {
	defer l.flushWg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		toolRuns  []toolRunEvent
		perms     []PermissionAudit
		subagents []SubagentAudit
		completes []subagentComplete
	)

	flush := func() {
		// Insert tool runs: entry first, then output with correct FK.
		for _, tr := range toolRuns {
			id, err := l.store.InsertEntry(tr.entry)
			if err != nil {
				slog.Warn("audit entry insert failed", "err", err)
				continue
			}
			if tr.output != nil {
				tr.output.EntryID = id
				if err := l.store.InsertOutput(*tr.output); err != nil {
					slog.Warn("audit output insert failed", "err", err)
				}
			}
		}
		toolRuns = toolRuns[:0]

		for _, p := range perms {
			if _, err := l.store.InsertPermission(p); err != nil {
				slog.Warn("audit permission insert failed", "err", err)
			}
		}
		perms = perms[:0]

		for _, sa := range subagents {
			if _, err := l.store.InsertSubagent(sa); err != nil {
				slog.Warn("audit subagent insert failed", "err", err)
			}
		}
		subagents = subagents[:0]

		for _, c := range completes {
			if err := l.store.CompleteSubagent(c.childSessionID, c.durationMs, c.status); err != nil {
				slog.Warn("audit subagent complete failed", "err", err)
			}
		}
		completes = completes[:0]
	}

	for {
		select {
		case ev := <-l.flushCh:
			switch {
			case ev.toolRun != nil:
				toolRuns = append(toolRuns, *ev.toolRun)
				if len(toolRuns) >= 100 {
					flush()
				}
			case ev.permission != nil:
				perms = append(perms, *ev.permission)
			case ev.subagent != nil:
				subagents = append(subagents, *ev.subagent)
			case ev.completeSub != nil:
				completes = append(completes, *ev.completeSub)
			}
		case <-ticker.C:
			flush()
		case <-l.done:
			// Drain remaining events before flushing.
		drainLoop:
			for {
				select {
				case ev := <-l.flushCh:
					switch {
					case ev.toolRun != nil:
						toolRuns = append(toolRuns, *ev.toolRun)
					case ev.permission != nil:
						perms = append(perms, *ev.permission)
					case ev.subagent != nil:
						subagents = append(subagents, *ev.subagent)
					case ev.completeSub != nil:
						completes = append(completes, *ev.completeSub)
					}
				default:
					break drainLoop
				}
			}
			flush()
			return
		}
	}
}

// LogToolRun enqueues a tool execution audit entry with optional
// captured output. Non-blocking: drops the event if the buffer is full.
// Output is redacted before storage to prevent secrets from persisting
// in the audit log (issue #45).
func (l *Logger) LogToolRun(entry AuditEntry, output *OutputCapture) {
	if output != nil {
		// CRITICAL FIX: Redact secrets BEFORE truncation and checksum
		// computation. Previously the checksum was computed on unredacted
		// output, so it never matched the stored (redacted) bytes.
		output.Stdout = redact.SecretString(output.Stdout)
		output.Stderr = redact.SecretString(output.Stderr)

		// Then truncate to configured limit.
		if l.maxBytes > 0 {
			output.Stdout = TruncateOutput(output.Stdout, l.maxBytes)
			output.Stderr = TruncateOutput(output.Stderr, l.maxBytes)
		}

		// Compute sizes and checksum on the FINAL stored bytes.
		output.StdoutSize = len(output.Stdout)
		output.StderrSize = len(output.Stderr)
		output.Checksum = ComputeChecksum(output.Stdout + output.Stderr)
	}

	select {
	case l.flushCh <- auditEvent{toolRun: &toolRunEvent{entry: entry, output: output}}:
	default:
		slog.Warn("audit buffer full, dropping tool run entry",
			"tool", entry.Tool, "session", entry.SessionID)
	}
}

// LogPermission enqueues a permission decision audit entry.
func (l *Logger) LogPermission(p PermissionAudit) {
	select {
	case l.flushCh <- auditEvent{permission: &p}:
	default:
		slog.Warn("audit buffer full, dropping permission entry",
			"tool", p.Tool, "session", p.SessionID)
	}
}

// LogSubagent enqueues a subagent dispatch audit entry.
func (l *Logger) LogSubagent(sa SubagentAudit) {
	select {
	case l.flushCh <- auditEvent{subagent: &sa}:
	default:
		slog.Warn("audit buffer full, dropping subagent entry",
			"child", sa.ChildSessionID)
	}
}

// CompleteSubagent enqueues a subagent completion update.
func (l *Logger) CompleteSubagent(childSessionID string, durationMs int64, status string) {
	select {
	case l.flushCh <- auditEvent{completeSub: &subagentComplete{
		childSessionID: childSessionID,
		durationMs:     durationMs,
		status:         status,
	}}:
	default:
		slog.Warn("audit buffer full, dropping subagent complete",
			"child", childSessionID)
	}
}

// Close flushes all pending events and stops the background goroutine.
func (l *Logger) Close() {
	l.once.Do(func() {
		close(l.done)
	})
	l.flushWg.Wait()
}

// TruncateOutput caps data at maxBytes and returns the result.
// When maxBytes is 0, data is returned unchanged.
func TruncateOutput(data string, maxBytes int) string {
	if maxBytes <= 0 || len(data) <= maxBytes {
		return data
	}
	return data[:maxBytes]
}
