package platform

import (
	"sync"

	"github.com/agently/api/internal/domain"
)

// In-process event bus — the realtime backbone, abstracted behind an interface
// so it can be swapped for Postgres LISTEN/NOTIFY, Redis pub/sub, etc. without
// touching producers or consumers. Mirrors
// packages/core/src/platform/events/bus.ts.
//
// It keeps a bounded ring buffer of recent events so a reconnecting SSE client
// can replay anything it missed since its lastEventId.

type EventHandler func(event domain.DomainEvent)

type EventBus interface {
	Publish(event domain.DomainEvent)
	// Subscribe to all events; returns an unsubscribe function.
	Subscribe(handler EventHandler) func()
	// ReplayAfter returns events buffered after the given event id (exclusive),
	// oldest first. An empty id replays the whole buffer.
	ReplayAfter(eventID string) []domain.DomainEvent
}

const defaultBufferSize = 500

type memoryBus struct {
	mu         sync.RWMutex
	handlers   map[int]EventHandler
	nextID     int
	buffer     []domain.DomainEvent
	bufferSize int
}

// NewEventBus creates an in-memory event bus.
func NewEventBus() EventBus {
	return &memoryBus{
		handlers:   make(map[int]EventHandler),
		buffer:     make([]domain.DomainEvent, 0, defaultBufferSize),
		bufferSize: defaultBufferSize,
	}
}

func (b *memoryBus) Publish(event domain.DomainEvent) {
	b.mu.Lock()
	b.buffer = append(b.buffer, event)
	if len(b.buffer) > b.bufferSize {
		b.buffer = b.buffer[len(b.buffer)-b.bufferSize:]
	}
	// Snapshot handlers under the lock so we can invoke them after releasing it.
	handlers := make([]EventHandler, 0, len(b.handlers))
	for _, h := range b.handlers {
		handlers = append(handlers, h)
	}
	b.mu.Unlock()

	for _, h := range handlers {
		// A failing subscriber must never break the producer or its peers.
		func() {
			defer func() { _ = recover() }()
			h(event)
		}()
	}
}

func (b *memoryBus) Subscribe(handler EventHandler) func() {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.handlers[id] = handler
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		delete(b.handlers, id)
		b.mu.Unlock()
	}
}

func (b *memoryBus) ReplayAfter(eventID string) []domain.DomainEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if eventID == "" {
		out := make([]domain.DomainEvent, len(b.buffer))
		copy(out, b.buffer)
		return out
	}
	for i, e := range b.buffer {
		if string(e.EventID()) == eventID {
			out := make([]domain.DomainEvent, len(b.buffer)-i-1)
			copy(out, b.buffer[i+1:])
			return out
		}
	}
	// Unknown id — replay everything (same fallback as the TS bus).
	out := make([]domain.DomainEvent, len(b.buffer))
	copy(out, b.buffer)
	return out
}
