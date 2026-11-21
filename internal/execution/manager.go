package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"shmorby/internal/redact"
	"shmorby/internal/xuuid"
)

// ErrNotRunning is returned when cancelling an execution that is not
// active in this process.
var ErrNotRunning = errors.New("execution not running")

// KillFailedError reports a cancellation that did not terminate the
// process group.
type KillFailedError struct {
	ID         string
	PID        int
	Diagnostic string
}

// Returns a diagnostic string including the PID.
func (e *KillFailedError) Error() string {
	return fmt.Sprintf(
		"execution %s kill_failed (pid %d): %s",
		e.ID, e.PID, e.Diagnostic,
	)
}

// Manager persists and cancels in-process executions.
type Manager struct {
	rec        Recorder
	killWait   time.Duration
	ownerPID   int
	ownerStart string

	mu     sync.Mutex
	active map[string]*Handle
}

// NewManager returns a manager backed by rec. A nil recorder disables
// persistence but still allows cancellation tracking.
func NewManager(rec Recorder) *Manager {
	ownerPID := os.Getpid()

	return &Manager{
		rec:        rec,
		killWait:   2 * time.Second,
		ownerPID:   ownerPID,
		ownerStart: ProcessIdentity(ownerPID),
		active:     make(map[string]*Handle),
	}
}

// Begin persists a pending execution and registers it for
// cancellation before permission evaluation.
func (m *Manager) Begin(
	ctx context.Context, n New,
) (*Handle, error) {
	id, err := xuuid.New()
	if err != nil {
		return nil, fmt.Errorf("execution id: %w", err)
	}

	rec := Record{
		ID:           id,
		SessionID:    n.SessionID,
		MessageID:    n.MessageID,
		ToolCallID:   n.ToolCallID,
		Tool:         n.Tool,
		Command:      n.Command,
		ArgsRedacted: n.ArgsRedacted,
		ArgsHash:     n.ArgsHash,
		ApprovalID:   n.ApprovalID,
		Status:       StatusPendingApproval,
		OwnerPID:     m.ownerPID,
		OwnerStart:   m.ownerStart,
		CreatedAt:    time.Now().UTC(),
	}

	if m.rec != nil {
		if err := m.rec.CreateExecution(rec); err != nil {
			return nil, fmt.Errorf("create execution: %w", err)
		}
	}

	h := &Handle{mgr: m, rec: rec, done: make(chan struct{})}

	m.mu.Lock()
	m.active[id] = h
	m.mu.Unlock()

	return h, nil
}

// Cancel requests cancellation of an execution by ID and waits for
// confirmed termination or the kill timeout.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	h := m.active[id]
	m.mu.Unlock()

	if h == nil {
		return fmt.Errorf("%w: %s", ErrNotRunning, id)
	}

	return h.Cancel()
}

// CancelAll requests cancellation for every active execution and
// returns the kill failures.
func (m *Manager) CancelAll() []error {
	m.mu.Lock()
	handles := make([]*Handle, 0, len(m.active))
	for _, h := range m.active {
		handles = append(handles, h)
	}
	m.mu.Unlock()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for _, h := range handles {
		h := h
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := h.Cancel(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	return errs
}

// Get returns a persisted execution by id.
func (m *Manager) Get(
	ctx context.Context, id string,
) (Record, error) {
	if m.rec == nil {
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	rec, err := m.rec.GetExecution(id)
	if err != nil {
		return Record{}, fmt.Errorf("get execution %s: %w", id, err)
	}

	return rec, nil
}

// List returns persisted executions matching f.
func (m *Manager) List(
	ctx context.Context, f Filter,
) ([]Record, error) {
	if m.rec == nil {
		return nil, nil
	}

	rows, err := m.rec.ListExecutions(f)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}

	return rows, nil
}

// Output returns one page of persisted execution output. An empty
// cursor starts at the first chunk.
func (m *Manager) Output(
	ctx context.Context, id, cursor string,
) (OutputChunk, error) {
	if m.rec == nil {
		return OutputChunk{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	n := 0
	if cursor != "" {
		var err error
		n, err = strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return OutputChunk{}, fmt.Errorf(
				"invalid output cursor %q", cursor,
			)
		}
	}

	data, done, err := m.rec.ReadExecutionOutput(id, n)
	if err != nil {
		return OutputChunk{}, fmt.Errorf(
			"read execution output %s: %w", id, err,
		)
	}

	next := ""
	if !done {
		next = strconv.Itoa(n + 1)
	}

	return OutputChunk{
		ExecutionID: id,
		Cursor:      strconv.Itoa(n),
		Data:        data,
		Next:        next,
		Done:        done,
	}, nil
}

// remove drops a terminal handle from the active set.
func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.active, id)
	m.mu.Unlock()
}

// Handle is the live control surface for one execution.
type Handle struct {
	mgr *Manager

	mu       sync.Mutex
	rec      Record
	cancel   context.CancelFunc
	done     chan struct{}
	terminal bool
}

// Returns the durable execution ID.
func (h *Handle) ID() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.rec.ID
}

// Start marks the execution running and returns a context carrying
// the execution handle for PID reporting.
func (h *Handle) Start(ctx context.Context) context.Context {
	h.mu.Lock()

	if h.terminal || h.rec.Status == StatusCancelRequested {
		h.mu.Unlock()
		execCtx, cancel := context.WithCancel(ctx)
		cancel()

		return WithHandle(execCtx, h)
	}

	execCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.rec.Status = StatusRunning
	h.rec.StartedAt = time.Now().UTC()
	h.persistLocked()
	h.mu.Unlock()

	return WithHandle(execCtx, h)
}

// SetPID records the process-group leader PID so reconciliation can
// detect children that outlive the parent.
func (h *Handle) SetPID(pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.terminal || pid <= 0 {
		return
	}

	h.rec.PID = pid
	h.rec.ProcessStart = ProcessIdentity(pid)
	h.persistLocked()
}

// SetExitCode records the command exit code when the tool reports
// one.
func (h *Handle) SetExitCode(code int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.terminal {
		return
	}

	h.rec.ExitCode = &code
	h.persistLocked()
}

// Finish records the terminal status from the tool result. A
// cancellation is reported as cancelled only after termination is
// confirmed; a live process group becomes kill_failed.
func (h *Handle) Finish(result string, runErr error) {
	h.finish(result, func() (Status, error) {
		started := !h.rec.StartedAt.IsZero()

		if !started {
			if strings.HasPrefix(result, "error: permission denied") {
				return StatusDenied, errors.New(result)
			}
			if runErr != nil {
				return StatusFailed, runErr
			}
			if result != "" {
				return StatusFailed, errors.New(result)
			}

			return StatusFailed,
				errors.New("execution did not start")
		}

		cancelled := h.rec.Status == StatusCancelRequested ||
			errors.Is(runErr, context.Canceled)

		if cancelled {
			if h.rec.PID > 0 && ProcessGroupAlive(h.rec.PID) {
				return StatusKillFailed, fmt.Errorf(
					"process group %d still alive after cancellation",
					h.rec.PID,
				)
			}

			return StatusCancelled, context.Canceled
		}

		if errors.Is(runErr, context.DeadlineExceeded) {
			return StatusTimedOut, runErr
		}
		if runErr != nil {
			return StatusFailed, runErr
		}

		return StatusSucceeded, nil
	})
}

// Cancel requests cancellation and waits for confirmed termination.
func (h *Handle) Cancel() error {
	h.mu.Lock()

	if h.terminal {
		h.mu.Unlock()

		return nil
	}

	h.rec.Status = StatusCancelRequested
	started := !h.rec.StartedAt.IsZero()
	cancel := h.cancel
	h.persistLocked()
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if !started {
		h.finish("", func() (Status, error) {
			return StatusCancelled, context.Canceled
		})

		return nil
	}

	select {
	case <-h.done:
		return h.cancelResult()
	case <-time.After(h.mgr.killWait):
		var killErr *KillFailedError

		h.finish("", func() (Status, error) {
			pid := h.rec.PID
			if pid > 0 && ProcessGroupAlive(pid) {
				diag := fmt.Sprintf(
					"process group %d still alive after cancellation",
					pid,
				)
				killErr = &KillFailedError{
					ID:         h.rec.ID,
					PID:        pid,
					Diagnostic: diag,
				}

				return StatusKillFailed, errors.New(diag)
			}

			return StatusCancelled, context.Canceled
		})

		return killErr
	}
}

// Returns the terminal cancellation outcome for a finished handle.
func (h *Handle) cancelResult() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rec.Status != StatusKillFailed {
		return nil
	}

	return &KillFailedError{
		ID:         h.rec.ID,
		PID:        h.rec.PID,
		Diagnostic: h.rec.Error,
	}
}

// finish applies a terminal status under the handle lock, then
// releases resources exactly once.
func (h *Handle) finish(
	result string, compute func() (Status, error),
) {
	h.mu.Lock()

	if h.terminal {
		h.mu.Unlock()

		return
	}

	status, err := compute()
	h.terminal = true
	h.rec.Status = status
	h.rec.EndedAt = time.Now().UTC()
	if err != nil {
		h.rec.Error = err.Error()
	}
	h.persistOutputLocked(result)
	h.persistLocked()

	cancel := h.cancel
	h.mu.Unlock()

	h.mgr.remove(h.rec.ID)
	close(h.done)

	if cancel != nil {
		cancel()
	}
}

// Persists captured output separately from the transcript. Callers
// hold h.mu.
func (h *Handle) persistOutputLocked(result string) {
	if h.mgr.rec == nil || result == "" {
		return
	}

	chunks := splitOutput(redact.SecretString(result))
	if len(chunks) == 0 {
		return
	}

	if err := h.mgr.rec.AppendExecutionOutput(
		h.rec.ID, chunks,
	); err != nil {
		slog.Warn("execution output persist failed",
			"execution", h.rec.ID, "err", err)

		return
	}
	h.rec.OutputRef = "execution_output:" + h.rec.ID
}

// Splits persisted output into bounded, rune-aligned chunks.
func splitOutput(data string) []string {
	if len(data) > MaxOutputBytes {
		data = data[:MaxOutputBytes]
		for !utf8.ValidString(data) {
			data = data[:len(data)-1]
		}
		data += "\n... (execution output truncated)"
	}

	var chunks []string
	for len(data) > OutputChunkBytes {
		cut := OutputChunkBytes
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		if cut == 0 {
			cut = OutputChunkBytes
		}
		chunks = append(chunks, data[:cut])
		data = data[cut:]
	}
	if data != "" {
		chunks = append(chunks, data)
	}

	return chunks
}

// Writes the current record; callers hold h.mu.
func (h *Handle) persistLocked() {
	if h.mgr.rec == nil {
		return
	}

	if err := h.mgr.rec.UpdateExecution(h.rec); err != nil {
		slog.Warn("execution persist failed",
			"execution", h.rec.ID, "err", err)
	}
}

type managerKey struct{}

type handleKey struct{}

// WithManager returns a context carrying the execution manager.
func WithManager(
	ctx context.Context, m *Manager,
) context.Context {
	return context.WithValue(ctx, managerKey{}, m)
}

// ManagerFrom returns the execution manager in ctx, or nil.
func ManagerFrom(ctx context.Context) *Manager {
	m, _ := ctx.Value(managerKey{}).(*Manager)

	return m
}

// WithHandle returns a context carrying the execution handle.
func WithHandle(
	ctx context.Context, h *Handle,
) context.Context {
	return context.WithValue(ctx, handleKey{}, h)
}

// HandleFrom returns the execution handle in ctx, or nil.
func HandleFrom(ctx context.Context) *Handle {
	h, _ := ctx.Value(handleKey{}).(*Handle)

	return h
}

// SetPID reports the process-group leader PID to the execution
// handle carried by ctx. It is a no-op when no handle is present.
func SetPID(ctx context.Context, pid int) {
	if h := HandleFrom(ctx); h != nil {
		h.SetPID(pid)
	}
}

// SetExitCode reports the command exit code to the execution handle
// carried by ctx. It is a no-op when no handle is present.
func SetExitCode(ctx context.Context, code int) {
	if h := HandleFrom(ctx); h != nil {
		h.SetExitCode(code)
	}
}
