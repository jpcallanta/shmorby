package backend

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestHubPublish_UniqueStableEnvelope(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := hub.Subscribe(ctx, EventFilter{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	peerEvents, err := hub.Subscribe(ctx, EventFilter{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Subscribe peer: %v", err)
	}

	for range 2 {
		if err := hub.Publish(Event{
			Type:      EventExecutionStarted,
			SessionID: "s-1",
			Data:      json.RawMessage(`{"tool":"shell"}`),
		}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	first := receiveEvent(t, events)
	firstPeer := receiveEvent(t, peerEvents)
	second := receiveEvent(t, events)
	secondPeer := receiveEvent(t, peerEvents)
	if first.ID != firstPeer.ID || second.ID != secondPeer.ID {
		t.Fatal("subscribers received different IDs for one emission")
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("want unique non-empty event IDs, got %q and %q",
			first.ID, second.ID)
	}
	if first.Created.IsZero() || second.Created.IsZero() {
		t.Fatal("want created timestamps")
	}
	if first.Type != EventExecutionStarted || first.SessionID != "s-1" {
		t.Fatalf("unexpected envelope: %+v", first)
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("Unmarshal envelope: %v", err)
	}
	for _, key := range []string{
		"id", "type", "created", "session_id", "input_id",
		"execution_id", "approval_id", "data",
	} {
		if _, ok := envelope[key]; !ok {
			t.Errorf("event envelope missing %q", key)
		}
	}
}

func TestHubPublish_CountsDroppedEvents(t *testing.T) {
	hub := NewHub()
	if _, err := hub.Subscribe(context.Background(), EventFilter{}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	for i := 0; i <= eventBufferSize; i++ {
		if err := hub.Publish(Event{Type: EventInputAccepted}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	if got := hub.DroppedEvents(); got != 1 {
		t.Fatalf("dropped event count = %d, want 1", got)
	}
}

func TestHubPublish_RejectsInvalidData(t *testing.T) {
	hub := NewHub()
	if err := hub.Publish(Event{Data: json.RawMessage(`{`)}); err == nil {
		t.Fatal("want invalid event data error")
	}
}

func TestEventTypes_AreStable(t *testing.T) {
	want := map[EventType]string{
		EventSessionCreated:    "session.created",
		EventSessionUpdated:    "session.updated",
		EventSessionDeleted:    "session.deleted",
		EventInputAccepted:     "input.accepted",
		EventInputCancelled:    "input.cancelled",
		EventExecutionStarted:  "execution.started",
		EventExecutionOutput:   "execution.output",
		EventExecutionEnded:    "execution.ended",
		EventApprovalRequested: "approval.requested",
		EventApprovalDecided:   "approval.decided",
		EventApprovalExpired:   "approval.expired",
		EventMessageCreated:    "message.created",
		EventTextDelta:         "session.text.delta",
		EventReasoningDelta:    "session.reasoning.delta",
		EventToolStarted:       "tool.started",
		EventToolProgress:      "tool.progress",
		EventToolEnded:         "tool.ended",
		EventContextCompacted:  "context.compacted",
		EventUsageUpdated:      "usage.updated",
		EventConfigUpdated:     "config.updated",
	}
	for eventType, value := range want {
		if string(eventType) != value {
			t.Errorf("event type %q, want %q", eventType, value)
		}
	}
}

func TestHubSubscribe_FiltersAndCloses(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	events, err := hub.Subscribe(ctx, EventFilter{
		SessionID: "wanted",
		Types:     []EventType{EventMessageCreated},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := hub.Publish(Event{
		Type: EventMessageCreated, SessionID: "other",
	}); err != nil {
		t.Fatalf("Publish filtered session: %v", err)
	}
	if err := hub.Publish(Event{
		Type: EventToolStarted, SessionID: "wanted",
	}); err != nil {
		t.Fatalf("Publish filtered type: %v", err)
	}
	if err := hub.Publish(Event{
		Type: EventMessageCreated, SessionID: "wanted",
	}); err != nil {
		t.Fatalf("Publish matching event: %v", err)
	}
	got := receiveEvent(t, events)
	if got.SessionID != "wanted" || got.Type != EventMessageCreated {
		t.Fatalf("got unmatching event: %+v", got)
	}

	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("want subscription channel closed")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription channel did not close")
	}
}

func receiveEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed unexpectedly")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}
