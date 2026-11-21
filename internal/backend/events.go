package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"shmorby/internal/xuuid"
)

const eventBufferSize = 128

// Hub publishes in-process events to filtered subscribers.
type Hub struct {
	mu          sync.Mutex
	subscribers map[uint64]*subscriber
	nextID      uint64
	dropped     atomic.Uint64
}

type subscriber struct {
	filter EventFilter
	ch     chan Event
}

// Creates an event hub.
func NewHub() *Hub {
	return &Hub{subscribers: make(map[uint64]*subscriber)}
}

// Assigns an emission ID and timestamp, then broadcasts the event. If a
// subscriber is full, its oldest queued event is discarded to keep publishers
// non-blocking.
func (h *Hub) Publish(event Event) error {
	if len(event.Data) == 0 {
		event.Data = json.RawMessage(`{}`)
	} else if !json.Valid(event.Data) {
		return fmt.Errorf("invalid event data")
	}
	id, err := xuuid.New()
	if err != nil {
		return fmt.Errorf("generate event ID: %w", err)
	}
	event.ID = id
	event.Created = time.Now().UTC()

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if !matches(sub.filter, event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			select {
			case <-sub.ch:
				h.dropped.Add(1)
			default:
			}
			select {
			case sub.ch <- event:
			default:
			}
		}
	}

	return nil
}

// Returns events matching filter until ctx is cancelled.
func (h *Hub) Subscribe(
	ctx context.Context,
	filter EventFilter,
) (<-chan Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.Lock()
	id := h.nextID
	h.nextID++
	sub := &subscriber{
		filter: filter,
		ch:     make(chan Event, eventBufferSize),
	}
	h.subscribers[id] = sub
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.mu.Lock()
		if current, ok := h.subscribers[id]; ok && current == sub {
			delete(h.subscribers, id)
			close(sub.ch)
		}
		h.mu.Unlock()
	}()

	return sub.ch, nil
}

// Returns the total number of buffered subscriber events discarded.
func (h *Hub) DroppedEvents() uint64 {
	return h.dropped.Load()
}

// Returns whether event matches the subscription filters.
func matches(filter EventFilter, event Event) bool {
	if filter.SessionID != "" && filter.SessionID != event.SessionID {
		return false
	}
	if filter.InputID != "" && filter.InputID != event.InputID {
		return false
	}
	if filter.ExecutionID != "" &&
		filter.ExecutionID != event.ExecutionID {
		return false
	}
	if filter.ApprovalID != "" &&
		filter.ApprovalID != event.ApprovalID {
		return false
	}
	if len(filter.Types) > 0 {
		for _, eventType := range filter.Types {
			if eventType == event.Type {
				return true
			}
		}
		return false
	}

	return true
}
