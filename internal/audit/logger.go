package audit

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"shmorby/internal/redact"
)

// Logger buffers audit writes and flushes them to the store
// asynchronously. Safe for concurrent use.
type Logger struct {
	store    *AuditStore
	maxBytes int
	required bool
	flushCh  chan auditEvent
	done     chan struct{}
	once     sync.Once
	flushWg  sync.WaitGroup
	mu       sync.RWMutex
	closed   bool
	dropped  atomic.Uint64
}

type auditEvent struct {
	toolRun     *toolRunEvent
	subagent    *SubagentAudit
	completeSub *subagentComplete
}

type toolRunEvent struct {
	entry  AuditEntry
	output *OutputCapture
}

type subagentComplete struct {
	childSessionID string
	durationMs     int64
	status         string
}

// NewLogger creates a best-effort async audit logger.
func NewLogger(
	store *AuditStore, bufferSize int, flushInterval time.Duration,
	maxBytes int,
) *Logger {
	return NewLoggerWithDelivery(
		store, bufferSize, flushInterval, maxBytes, "best_effort",
	)
}

// NewLoggerWithDelivery creates an audit logger for the selected mode.
func NewLoggerWithDelivery(
	store *AuditStore, bufferSize int, flushInterval time.Duration,
	maxBytes int, delivery string,
) *Logger {
	if bufferSize < 1 {
		bufferSize = 100
	}
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond
	}

	l := &Logger{
		store:    store,
		maxBytes: maxBytes,
		required: delivery == "required",
		flushCh:  make(chan auditEvent, bufferSize),
		done:     make(chan struct{}),
	}

	l.flushWg.Add(1)
	go l.flushLoop(flushInterval)

	return l
}

// flushLoop drains buffered events into store writes.
func (l *Logger) flushLoop(interval time.Duration) {
	defer l.flushWg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		toolRuns  []toolRunEvent
		subagents []SubagentAudit
		completes []subagentComplete
	)

	flush := func() {
		for _, run := range toolRuns {
			if err := l.store.InsertToolRun(run.entry, run.output); err != nil {
				l.reportDrop("tool run", err)
			}
		}
		toolRuns = toolRuns[:0]

		for _, subagent := range subagents {
			if _, err := l.store.InsertSubagent(subagent); err != nil {
				l.reportDrop("subagent", err)
			}
		}
		subagents = subagents[:0]

		for _, complete := range completes {
			if err := l.store.CompleteSubagent(
				complete.childSessionID,
				complete.durationMs,
				complete.status,
			); err != nil {
				l.reportDrop("subagent completion", err)
			}
		}
		completes = completes[:0]
	}

	drain := func() {
		for {
			select {
			case event := <-l.flushCh:
				l.collect(event, &toolRuns, &subagents, &completes)
			default:
				return
			}
		}
	}

	for {
		select {
		case event := <-l.flushCh:
			l.collect(event, &toolRuns, &subagents, &completes)
			if len(toolRuns) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-l.done:
			drain()
			flush()
			return
		}
	}
}

// Appends a buffered event to the matching pending slice.
func (l *Logger) collect(
	event auditEvent,
	toolRuns *[]toolRunEvent,
	subagents *[]SubagentAudit,
	completes *[]subagentComplete,
) {
	switch {
	case event.toolRun != nil:
		*toolRuns = append(*toolRuns, *event.toolRun)
	case event.subagent != nil:
		*subagents = append(*subagents, *event.subagent)
	case event.completeSub != nil:
		*completes = append(*completes, *event.completeSub)
	}
}

// LogToolRun writes a tool invocation and optional captured output.
func (l *Logger) LogToolRun(
	entry AuditEntry, output *OutputCapture,
) error {
	output = l.prepareOutput(output)
	entry.Args = redact.SecretString(entry.Args)
	entry.Error = redact.SecretString(entry.Error)

	if l.required {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.closed {
			return l.recordFailure("tool run", fmt.Errorf("logger closed"))
		}
		if err := l.store.InsertToolRun(entry, output); err != nil {
			return l.recordFailure("tool run", err)
		}
		return nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		l.reportDrop("tool run", fmt.Errorf("logger closed"))
		return nil
	}
	select {
	case l.flushCh <- auditEvent{
		toolRun: &toolRunEvent{entry: entry, output: output},
	}:
		return nil
	default:
		l.reportDrop("tool run", fmt.Errorf("buffer full"))
		return nil
	}
}

// Redacts and truncates captured output before persistence.
func (l *Logger) prepareOutput(output *OutputCapture) *OutputCapture {
	if output == nil {
		return nil
	}
	copy := *output
	copy.Stdout = redact.SecretString(copy.Stdout)
	copy.Stderr = redact.SecretString(copy.Stderr)
	if l.maxBytes > 0 {
		copy.Stdout = TruncateOutput(copy.Stdout, l.maxBytes)
		copy.Stderr = TruncateOutput(copy.Stderr, l.maxBytes)
	}
	copy.StdoutSize = len(copy.Stdout)
	copy.StderrSize = len(copy.Stderr)
	copy.Checksum = ComputeChecksum(copy.Stdout + copy.Stderr)

	return &copy
}

// LogLifecycleSync persists an approval or execution boundary first.
func (l *Logger) LogLifecycleSync(event LifecycleEvent) error {
	if event.Command != "" {
		event.Command = redact.SecretString(event.Command)
	}
	if event.Error != "" {
		event.Error = redact.SecretString(event.Error)
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return l.recordFailure("lifecycle event", fmt.Errorf("logger closed"))
	}
	if err := l.store.AppendLifecycleEvent(event); err != nil {
		return l.recordFailure("lifecycle event", err)
	}

	return nil
}

// LogPermissionSync persists a permission decision before returning.
func (l *Logger) LogPermissionSync(p PermissionAudit) error {
	p.Command, p.Reason = redact.PermissionFields(p.Command, p.Reason)

	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return l.recordFailure("permission", fmt.Errorf("logger closed"))
	}
	if _, err := l.store.InsertPermission(p); err != nil {
		return l.recordFailure("permission", err)
	}

	return nil
}

// LogSubagent writes a subagent dispatch record.
func (l *Logger) LogSubagent(sa SubagentAudit) error {
	if l.required {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.closed {
			return l.recordFailure("subagent", fmt.Errorf("logger closed"))
		}
		if _, err := l.store.InsertSubagent(sa); err != nil {
			return l.recordFailure("subagent", err)
		}
		return nil
	}

	return l.enqueue(auditEvent{subagent: &sa}, "subagent")
}

// CompleteSubagent writes a subagent completion update.
func (l *Logger) CompleteSubagent(
	childSessionID string, durationMs int64, status string,
) error {
	completion := &subagentComplete{
		childSessionID: childSessionID,
		durationMs:     durationMs,
		status:         status,
	}
	if l.required {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.closed {
			return l.recordFailure(
				"subagent completion",
				fmt.Errorf("logger closed"),
			)
		}
		if err := l.store.CompleteSubagent(
			childSessionID, durationMs, status,
		); err != nil {
			return l.recordFailure("subagent completion", err)
		}
		return nil
	}

	return l.enqueue(
		auditEvent{completeSub: completion}, "subagent completion",
	)
}

// Queues an event for asynchronous delivery in best-effort mode.
func (l *Logger) enqueue(event auditEvent, name string) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		l.reportDrop(name, fmt.Errorf("logger closed"))
		return nil
	}
	select {
	case l.flushCh <- event:
		return nil
	default:
		l.reportDrop(name, fmt.Errorf("buffer full"))
		return nil
	}
}

// Counts a failed required-mode write and returns its error.
func (l *Logger) recordFailure(name string, err error) error {
	l.dropped.Add(1)
	slog.Warn("audit record not persisted", "record", name,
		"delivery", l.deliveryMode(), "dropped", l.dropped.Load(),
		"err", err)
	if l.required {
		return fmt.Errorf("persist required audit %s: %w", name, err)
	}
	return nil
}

// Counts a dropped best-effort event without failing the caller.
func (l *Logger) reportDrop(name string, err error) {
	l.dropped.Add(1)
	slog.Warn("audit record dropped", "record", name,
		"delivery", l.deliveryMode(), "dropped", l.dropped.Load(),
		"err", err)
}

func (l *Logger) deliveryMode() string {
	if l.required {
		return "required"
	}
	return "best_effort"
}

// Returns the number of audit records that could not be saved.
func (l *Logger) Dropped() uint64 {
	return l.dropped.Load()
}

// Reports whether audit failures must fail the operation.
func (l *Logger) Required() bool {
	return l.required
}

// HasLifecycleEvent checks for a matching durable execution record.
func (l *Logger) HasLifecycleEvent(
	executionID, eventType string,
) (bool, error) {
	return l.store.HasLifecycleEvent(executionID, eventType)
}

// Close flushes all pending events and stops the background goroutine.
func (l *Logger) Close() {
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		close(l.done)
		l.mu.Unlock()
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
