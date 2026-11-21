package approval

import (
	"context"
	"errors"
	"testing"
	"time"

	"shmorby/internal/session"
)

type decisionFailureStore struct{}

func (decisionFailureStore) CreateApproval(session.ApprovalRecord) error {
	return nil
}

func (decisionFailureStore) ResolveApproval(session.ApprovalRecord) error {
	return errors.New("storage unavailable")
}

func (decisionFailureStore) ExpireApproval(string, string, time.Time) error {
	return nil
}

func TestServiceDecide_ResolvesActiveRequest(t *testing.T) {
	service := NewService(nil, time.Second)
	requested := make(chan session.ApprovalRecord, 1)
	result := make(chan Result, 1)
	go func() {
		result <- service.Request(
			context.Background(),
			session.ApprovalRecord{SessionID: "session-1"},
			func(record session.ApprovalRecord) Prompt {
				requested <- record
				return Prompt{}
			},
		)
	}()

	var record session.ApprovalRecord
	select {
	case record = <-requested:
	case <-time.After(time.Second):
		t.Fatal("request was not made")
	}
	if err := service.Decide(record.ID, DecisionAlways); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	select {
	case got := <-result:
		if got.Decision != DecisionAlways ||
			got.DecisionSource != "backend" || got.Scope != "turn" {
			t.Fatalf("unexpected result: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not resolve")
	}
}

func TestServiceDecide_ReturnsPersistenceFailure(t *testing.T) {
	service := NewService(decisionFailureStore{}, time.Second)
	requested := make(chan session.ApprovalRecord, 1)
	go service.Request(
		context.Background(),
		session.ApprovalRecord{SessionID: "session-1"},
		func(record session.ApprovalRecord) Prompt {
			requested <- record
			return Prompt{}
		},
	)

	var record session.ApprovalRecord
	select {
	case record = <-requested:
	case <-time.After(time.Second):
		t.Fatal("request was not made")
	}
	if err := service.Decide(record.ID, DecisionOnce); !errors.Is(
		err, ErrDecisionPersistence,
	) {
		t.Fatalf("want persistence error, got %v", err)
	}
}

func TestServiceDecide_RejectsUnavailableRequest(t *testing.T) {
	service := NewService(nil, time.Second)
	if err := service.Decide("missing", DecisionOnce); !errors.Is(
		err, ErrDecisionUnavailable,
	) {
		t.Fatalf("want unavailable error, got %v", err)
	}
	if err := service.Decide("missing", Decision("other")); !errors.Is(
		err, ErrInvalidDecision,
	) {
		t.Fatalf("want invalid decision error, got %v", err)
	}
}
