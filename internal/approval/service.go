// Package approval persists interactive permission requests and decisions.
package approval

import (
	"context"
	"errors"
	"log/slog"
	"os/user"
	"sync"
	"time"

	"shmorby/internal/session"
	"shmorby/internal/xuuid"
)

// Decision is a user's response to an approval request.
type Decision string

const (
	DecisionOnce   Decision = "once"
	DecisionAlways Decision = "always"
	DecisionReject Decision = "reject"
)

// Result describes the durable outcome of a permission request.
type Result struct {
	ApprovalID     string
	Decision       Decision
	DecidedBy      string
	DecisionSource string
	Scope          string
}

// Prompt carries the delivery channel and an optional timeout callback.
type Prompt struct {
	Decisions  <-chan Decision
	OnExpire   func()
	OnDecision func(Decision)
}

// Store is the durable portion of the session store used by approvals.
type Store interface {
	CreateApproval(session.ApprovalRecord) error
	ResolveApproval(session.ApprovalRecord) error
	ExpireApproval(id, source string, at time.Time) error
}

// DefaultTimeout bounds how long a request can remain pending.
const DefaultTimeout = 5 * time.Minute

var (
	ErrDecisionUnavailable = errors.New("approval decision unavailable")
	ErrDecisionPersistence = errors.New("persist approval decision")
	ErrInvalidDecision     = errors.New("invalid approval decision")
)

// Service persists requests before prompting, then waits for a
// response or denies the request when its deadline is reached.
type pendingApproval struct {
	decision chan Decision
	done     chan struct{}
	result   Result
}

type Service struct {
	store    Store
	timeout  time.Duration
	identity string

	mu          sync.Mutex
	pending     map[string]*pendingApproval
	onRequested func(session.ApprovalRecord)
}

// NewService creates an approval service. A nil store keeps prompts
// functional without durable session storage.
func NewService(store Store, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Service{
		store: store, timeout: timeout, identity: LocalIdentity(),
		pending: make(map[string]*pendingApproval),
	}
}

var (
	identityOnce sync.Once
	identity     string
)

// LocalIdentity returns the current OS username or a generic local ID.
func LocalIdentity() string {
	identityOnce.Do(func() {
		current, err := user.Current()
		if err == nil && current.Username != "" {
			identity = current.Username
		} else {
			identity = "local"
		}
	})

	return identity
}

// Request records the approval before calling prompt. Persistence or
// prompt failures fail closed.
func (s *Service) Request(
	ctx context.Context,
	record session.ApprovalRecord,
	prompt func(session.ApprovalRecord) Prompt,
) (result Result) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return Result{
			Decision: DecisionReject, DecidedBy: "system",
			DecisionSource: "unavailable", Scope: "call",
		}
	}

	id, err := xuuid.New()
	if err != nil {
		slog.Warn("generate approval id failed", "err", err)

		return Result{
			Decision: DecisionReject, DecidedBy: "system",
			DecisionSource: "persistence_error", Scope: "call",
		}
	}
	now := time.Now().UTC()
	record.ID = id
	record.Status = "pending"
	record.RequestedAt = now
	record.ExpiresAt = now.Add(s.timeout)
	if s.store != nil {
		if err := s.store.CreateApproval(record); err != nil {
			slog.Warn("persist approval request failed",
				"execution", record.ExecutionID, "err", err)

			return Result{
				ApprovalID:     record.ID,
				Decision:       DecisionReject,
				DecidedBy:      "system",
				DecisionSource: "persistence_error",
				Scope:          "call",
			}
		}
	}

	pending := &pendingApproval{
		decision: make(chan Decision, 1),
		done:     make(chan struct{}),
	}
	s.mu.Lock()
	s.pending[record.ID] = pending
	onRequested := s.onRequested
	s.mu.Unlock()
	if onRequested != nil {
		onRequested(record)
	}
	defer func() {
		s.mu.Lock()
		pending.result = result
		delete(s.pending, record.ID)
		close(pending.done)
		s.mu.Unlock()
	}()

	var response Prompt
	if prompt != nil {
		response = prompt(record)
	}
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()

	var decision Decision
	var decidedBy, source string
	select {
	case received, ok := <-response.Decisions:
		decision = received
		if !ok {
			decision = DecisionReject
			decidedBy = "system"
			source = "disconnected"
		} else {
			decidedBy = s.identity
			source = "interactive"
		}
	case decision = <-pending.decision:
		decidedBy = s.identity
		source = "backend"
		if response.OnDecision != nil {
			response.OnDecision(decision)
		}
	case <-timer.C:
		s.mu.Lock()
		delete(s.pending, record.ID)
		s.mu.Unlock()
		select {
		case received, ok := <-response.Decisions:
			decision = received
			if !ok {
				decision = DecisionReject
				decidedBy = "system"
				source = "disconnected"
			} else {
				decidedBy = s.identity
				source = "interactive"
			}
		case decision = <-pending.decision:
			decidedBy = s.identity
			source = "backend"
			if response.OnDecision != nil {
				response.OnDecision(decision)
			}
		default:
			if response.OnExpire != nil {
				response.OnExpire()
			}
			if s.store != nil {
				if err := s.store.ExpireApproval(
					record.ID, "timeout", time.Now().UTC(),
				); err != nil {
					slog.Warn("expire approval failed",
						"approval", record.ID, "err", err)
				}
			}

			return Result{
				ApprovalID:     record.ID,
				Decision:       DecisionReject,
				DecidedBy:      "system",
				DecisionSource: "timeout", Scope: "call",
			}
		}
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, record.ID)
		s.mu.Unlock()
		decision = DecisionReject
		decidedBy = "system"
		source = "cancelled"
	}

	if decision != DecisionOnce && decision != DecisionAlways {
		decision = DecisionReject
	}
	record.Decision = string(decision)
	record.Scope = "call"
	record.DecidedBy = decidedBy
	record.DecisionSource = source
	record.DecidedAt = time.Now().UTC()
	if decision == DecisionAlways {
		record.Scope = "turn"
	}
	if decision == DecisionReject {
		record.Status = "denied"
	} else {
		record.Status = "approved"
	}

	if s.store != nil {
		if err := s.store.ResolveApproval(record); err != nil {
			slog.Warn("persist approval decision failed",
				"approval", record.ID, "err", err)

			return Result{
				ApprovalID:     record.ID,
				Decision:       DecisionReject,
				DecidedBy:      "system",
				DecisionSource: "persistence_error",
				Scope:          "call",
			}
		}
	}

	return Result{
		ApprovalID:     record.ID,
		Decision:       decision,
		DecidedBy:      decidedBy,
		DecisionSource: source, Scope: record.Scope,
	}
}

// Registers a callback after the request is persisted and before the
// frontend is prompted.
func (s *Service) SetRequestObserver(
	observer func(session.ApprovalRecord),
) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onRequested = observer
	s.mu.Unlock()
}

// Resolves an active request from another in-process client.
func (s *Service) Decide(id string, decision Decision) error {
	if decision != DecisionOnce && decision != DecisionAlways &&
		decision != DecisionReject {
		return ErrInvalidDecision
	}
	if s == nil {
		return ErrDecisionUnavailable
	}

	s.mu.Lock()
	pending := s.pending[id]
	if pending == nil {
		s.mu.Unlock()
		return ErrDecisionUnavailable
	}
	select {
	case pending.decision <- decision:
		s.mu.Unlock()
	default:
		s.mu.Unlock()
		return ErrDecisionUnavailable
	}

	<-pending.done
	if pending.result.DecisionSource == "persistence_error" {
		return ErrDecisionPersistence
	}
	if pending.result.Decision != decision {
		return ErrDecisionUnavailable
	}

	return nil
}
