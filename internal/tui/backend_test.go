package tui

import (
	"encoding/json"
	"testing"

	"shmorby/internal/backend"
)

func TestBackendMessageCreated_FinalizesStreamOnce(t *testing.T) {
	model := NewModel(Config{})
	model.running = true
	model.activeInputID = "input-1"
	reasoning, err := json.Marshal(backend.DeltaEventData{
		Delta: "thinking",
	})
	if err != nil {
		t.Fatalf("Marshal reasoning: %v", err)
	}
	updated, _ := model.Update(backendEventMsg{event: backend.Event{
		Type: backend.EventReasoningDelta,
		Data: reasoning,
	}})
	model = updated.(Model)
	if !model.thinking.Active() {
		t.Fatal("reasoning block is not active")
	}
	textDelta, err := json.Marshal(backend.DeltaEventData{Delta: "hello"})
	if err != nil {
		t.Fatalf("Marshal text: %v", err)
	}
	updated, _ = model.Update(backendEventMsg{event: backend.Event{
		Type: backend.EventTextDelta,
		Data: textDelta,
	}})
	model = updated.(Model)
	if model.thinking.Active() {
		t.Fatal("reasoning block is still active after text")
	}
	data, err := json.Marshal(backend.MessageEventData{
		Role: "assistant", Content: "hello",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	updated, _ = model.Update(backendEventMsg{event: backend.Event{
		Type:    backend.EventMessageCreated,
		InputID: "input-1",
		Data:    data,
	}})
	got := updated.(Model)
	if got.running {
		t.Fatal("turn is still running")
	}
	if len(got.output) != 1 || got.output[0].text != "hello" {
		t.Fatalf("want one streamed reply, got %+v", got.output)
	}
}
