package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/services"
	"github.com/go-chi/chi/v5"
)

const heartbeatInterval = 15 * time.Second

// setSSEHeaders sets the standard SSE response headers.
func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
}

// sseFrame writes one SSE frame: optional id, event name, JSON data.
func sseFrame(w http.ResponseWriter, flusher http.Flusher, id, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	flusher.Flush()
}

// eventsStream is the workspace-wide domain event stream. A reconnecting client
// can pass Last-Event-ID to replay missed events from the bus buffer. Mirrors
// app/api/events/stream/route.ts.
func eventsStream(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			fail(w, domain.CodeNotImplemented, "streaming unsupported", nil)
			return
		}
		setSSEHeaders(w)

		emit := func(evt domain.DomainEvent) {
			sseFrame(w, flusher, string(evt.EventID()), evt.EventType(), evt)
		}

		// Replay anything missed since the client's last seen event.
		for _, evt := range p.Bus.ReplayAfter(r.Header.Get("Last-Event-ID")) {
			emit(evt)
		}

		// Deliver live events through a buffered channel so the bus handler
		// (which may run on another goroutine) never blocks on the HTTP writer.
		events := make(chan domain.DomainEvent, 64)
		unsub := p.Bus.Subscribe(func(evt domain.DomainEvent) {
			select {
			case events <- evt:
			default: // drop if the client can't keep up
			}
		})
		defer unsub()

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-events:
				emit(evt)
			case <-ticker.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	}
}

// streamLogs is the per-run log SSE stream. It emits `log` frames for appended
// log lines and a `finished` frame when the run ends. Mirrors
// app/api/runs/[id]/logs/stream/route.ts.
func streamLogs(p *services.Platform) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			fail(w, domain.CodeNotImplemented, "streaming unsupported", nil)
			return
		}
		runID := asRunID(chi.URLParam(r, "id"))
		setSSEHeaders(w)

		sseFrame(w, flusher, "", "ready", map[string]any{"runId": runID})

		events := make(chan domain.DomainEvent, 64)
		unsub := p.Bus.Subscribe(func(evt domain.DomainEvent) {
			select {
			case events <- evt:
			default:
			}
		})
		defer unsub()

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-events:
				switch e := evt.(type) {
				case domain.RunLogAppendedEvent:
					if e.RunID == runID {
						sseFrame(w, flusher, "", "log", e.Log)
					}
				case domain.RunFinishedEvent:
					if e.RunID == runID {
						sseFrame(w, flusher, "", "finished", map[string]any{"status": e.Status})
					}
				}
			case <-ticker.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	}
}
