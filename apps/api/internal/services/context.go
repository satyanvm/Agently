// Package services holds the durable business logic (use-cases). Each service
// depends only on repository interfaces, the event bus, the clock, and the
// logger — never on a concrete store — mirroring
// packages/core/src/platform/services.
package services

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// SeedStats are the materialized rollups from the seed (a stand-in for a future
// stats table).
type SeedStats struct {
	WorkflowStats  map[string]domain.WorkflowStats
	WorkspaceStats domain.WorkspaceStats
}

// Deps is everything the services need, injected once at platform construction.
type Deps struct {
	Repos     *platform.Repositories
	Bus       platform.EventBus
	Clock     platform.Clock
	Logger    platform.Logger
	SeedStats SeedStats
}

// Emitter publishes a domain event after stamping its envelope (id, occurredAt,
// workspaceId) and its discriminator type. Mirrors createEmitter in context.ts.
type Emitter func(event domain.DomainEvent)

// NewEmitter builds an emitter bound to the bus, clock, and workspace.
func NewEmitter(bus platform.EventBus, clock platform.Clock, workspaceID domain.WorkspaceId) Emitter {
	return func(event domain.DomainEvent) {
		stamped := stampEvent(event, domain.NewEventId(), domain.Timestamp(clock.ISO()), workspaceID)
		bus.Publish(stamped)
	}
}

// stampEvent fills the envelope + type discriminator on a partially-built event.
// Each event is built by the services with only its payload fields set; this
// fills id/occurredAt/workspaceId/type so the wire shape matches the contract.
func stampEvent(e domain.DomainEvent, id domain.EventId, at domain.Timestamp, ws domain.WorkspaceId) domain.DomainEvent {
	base := domain.EventBase{ID: id, OccurredAt_: at, WorkspaceID_: ws}
	switch ev := e.(type) {
	case domain.RunQueuedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunStartedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunProgressEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunAgentTransitionedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunLogAppendedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunBrowserActionEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunArtifactProducedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunCostThresholdEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.RunFinishedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	case domain.NotificationCreatedEvent:
		ev.EventBase, ev.Type = base, ev.EventType()
		return ev
	default:
		return e
	}
}
