package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"shmorby/internal/backend"
	"shmorby/internal/llm"
	"shmorby/internal/session"
)

type testProvider struct{}

func (testProvider) Name() string { return "test" }

func (testProvider) Chat(
	context.Context,
	llm.ChatRequest,
) (llm.ChatResponse, error) {
	return llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "hello"},
	}, nil
}

func (testProvider) ChatStream(
	ctx context.Context,
	_ llm.ChatRequest,
) (<-chan llm.StreamEvent, error) {
	stream := make(chan llm.StreamEvent, 4)
	stream <- llm.StreamEvent{Type: "reasoning", Delta: "thinking"}
	stream <- llm.StreamEvent{Type: "text", Delta: "hello"}
	stream <- llm.StreamEvent{
		Type:    "usage",
		Content: `{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`,
	}
	stream <- llm.StreamEvent{Done: true}
	close(stream)
	return stream, nil
}

func (testProvider) ModelInfo(
	context.Context,
	string,
) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

func TestInProcess_SubmitInputPublishesContractEvents(t *testing.T) {
	store, err := session.NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	impl := NewBackend(Config{
		Provider:      testProvider{},
		ProviderName:  "test",
		SessionStore:  store,
		Mode:          "chat",
		Model:         "model-test",
		MaxIterations: 2,
	})
	events, err := impl.Subscribe(context.Background(), backend.EventFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	created, err := impl.CreateSession(context.Background(), backend.SessionSpec{
		Directory: t.TempDir(),
		AgentMode: "chat",
		Provider:  "test",
		Model:     "model-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	createdEvent := receiveAppEvent(t, events)
	if createdEvent.Type != backend.EventSessionCreated ||
		createdEvent.SessionID != created.Meta.ID {
		t.Fatalf("unexpected creation event: %+v", createdEvent)
	}
	impl.mu.Lock()
	delete(impl.sessions, created.Meta.ID)
	impl.mu.Unlock()

	id, err := impl.SubmitInput(context.Background(), created.Meta.ID,
		backend.Input{
			ID: "input-1", Text: "say hello", Stream: true,
		})
	if err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if id != backend.InputID("input-1") {
		t.Fatalf("want input ID input-1, got %q", id)
	}

	seen := make(map[backend.EventType]bool)
	ids := map[string]bool{createdEvent.ID: true}
	for !seen[backend.EventMessageCreated] ||
		!seen[backend.EventSessionUpdated] {
		event := receiveAppEvent(t, events)
		if event.InputID != string(id) {
			t.Fatalf("event input ID = %q, want %q", event.InputID, id)
		}
		if ids[event.ID] {
			t.Fatalf("duplicate event ID %q", event.ID)
		}
		ids[event.ID] = true
		seen[event.Type] = true
		if event.Type == backend.EventMessageCreated {
			var data backend.MessageEventData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				t.Fatalf("decode message event: %v", err)
			}
			if data.Role != "assistant" || data.Content != "hello" {
				t.Fatalf("unexpected message data: %+v", data)
			}
		}
	}
	for _, eventType := range []backend.EventType{
		backend.EventInputAccepted,
		backend.EventTextDelta,
		backend.EventReasoningDelta,
		backend.EventUsageUpdated,
		backend.EventMessageCreated,
		backend.EventSessionUpdated,
	} {
		if !seen[eventType] {
			t.Errorf("missing %s event", eventType)
		}
	}

	got, err := impl.GetSession(context.Background(), created.Meta.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Meta.Provider != "test" || len(got.Messages) != 2 {
		t.Fatalf("unexpected session snapshot: %+v", got)
	}
	if got.Messages[0].InputID != string(id) ||
		got.Messages[1].Content != "hello" {
		t.Fatalf("unexpected transcript: %+v", got.Messages)
	}
	persisted, _, err := store.Load(created.Meta.ID)
	if err != nil {
		t.Fatalf("Load persisted session: %v", err)
	}
	if len(persisted.Messages()) != 2 {
		t.Fatalf("persisted message count = %d, want 2",
			len(persisted.Messages()))
	}

	rows, err := impl.ListSessions(context.Background(), backend.SessionFilter{
		Directory: created.Meta.Directory,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 1 || rows[0].Meta.ID != created.Meta.ID {
		t.Fatalf("unexpected sessions: %+v", rows)
	}
}

func TestInProcess_SubmitInput_NonStreamingEmitsFinalOnly(t *testing.T) {
	sess := session.New()
	impl := NewBackend(Config{
		Provider: testProvider{},
		Session:  sess,
		Mode:     "chat",
		Model:    "model-test",
	})
	events, err := impl.Subscribe(context.Background(), backend.EventFilter{
		SessionID: sess.ID(),
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, err := impl.SubmitInput(context.Background(), sess.ID(),
		backend.Input{ID: "input-2", Text: "say hello"}); err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}

	seenTextDelta := false
	for {
		event := receiveAppEvent(t, events)
		if event.Type == backend.EventTextDelta {
			seenTextDelta = true
		}
		if event.Type == backend.EventMessageCreated {
			var data backend.MessageEventData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				t.Fatalf("decode message event: %v", err)
			}
			if data.Content != "hello" {
				t.Fatalf("reply = %q, want hello", data.Content)
			}
			break
		}
	}
	if seenTextDelta {
		t.Fatal("non-streaming input emitted text deltas")
	}
}

func TestInProcess_InMemorySessionMetadataIsPerSession(t *testing.T) {
	impl := NewBackend(Config{
		Mode: "operate", Model: "default-model",
	})
	first, err := impl.CreateSession(context.Background(), backend.SessionSpec{
		Directory: "/one", AgentMode: "diagnose",
		Provider: "provider-one", Model: "model-one",
	})
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	_, err = impl.CreateSession(context.Background(), backend.SessionSpec{
		Directory: "/two", AgentMode: "chat",
		Provider: "provider-two", Model: "model-two",
	})
	if err != nil {
		t.Fatalf("CreateSession second: %v", err)
	}

	rows, err := impl.ListSessions(context.Background(), backend.SessionFilter{
		Directory: "/one",
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 1 || rows[0].Meta.ID != first.Meta.ID ||
		rows[0].Meta.AgentMode != "diagnose" ||
		rows[0].Meta.Model != "model-one" {
		t.Fatalf("unexpected filtered sessions: %+v", rows)
	}
	got, err := impl.GetSession(context.Background(), first.Meta.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Meta.Directory != "/one" || got.Meta.Provider != "provider-one" ||
		got.Meta.AgentMode != "diagnose" || got.Meta.Model != "model-one" {
		t.Fatalf("unexpected session metadata: %+v", got.Meta)
	}
}

func receiveAppEvent(t *testing.T, events <-chan backend.Event) backend.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed unexpectedly")
		}
		return event
	case <-time.After(time.Second * 2):
		t.Fatal("timed out waiting for backend event")
		return backend.Event{}
	}
}
