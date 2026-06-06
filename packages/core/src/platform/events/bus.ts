import type { DomainEvent, DomainEventType } from "@agently/contracts";

/**
 * In-process event bus — the realtime backbone, deliberately abstracted behind
 * an interface so it can be swapped for Postgres LISTEN/NOTIFY, Supabase
 * Realtime, Redis pub/sub, or Kafka without touching producers or consumers.
 *
 * It keeps a bounded ring buffer of recent events so a reconnecting SSE/WebSocket
 * client can replay anything it missed since its `lastEventId` — the same
 * "resume from cursor" guarantee a durable broker gives, good enough for the
 * single-process dev/mock backend.
 */

export type EventHandler = (event: DomainEvent) => void;

export interface EventBus {
  publish(event: DomainEvent): void;
  /** Subscribe to all events. Returns an unsubscribe function. */
  subscribe(handler: EventHandler): () => void;
  /** Subscribe to a single event type with a narrowed handler. */
  on<T extends DomainEventType>(
    type: T,
    handler: (event: Extract<DomainEvent, { type: T }>) => void,
  ): () => void;
  /** Events buffered after the given event id (exclusive), oldest first. */
  replayAfter(eventId: string | null): DomainEvent[];
}

export interface EventBusOptions {
  /** Max events retained for replay. */
  bufferSize?: number;
}

export function createEventBus(opts: EventBusOptions = {}): EventBus {
  const bufferSize = opts.bufferSize ?? 500;
  const handlers = new Set<EventHandler>();
  const buffer: DomainEvent[] = [];

  return {
    publish(event) {
      buffer.push(event);
      if (buffer.length > bufferSize) buffer.shift();
      for (const h of handlers) {
        try {
          h(event);
        } catch {
          // A failing subscriber must never break the producer or its peers.
        }
      }
    },
    subscribe(handler) {
      handlers.add(handler);
      return () => handlers.delete(handler);
    },
    on(type, handler) {
      const wrapped: EventHandler = (e) => {
        if (e.type === type) handler(e as Extract<DomainEvent, { type: typeof type }>);
      };
      handlers.add(wrapped);
      return () => handlers.delete(wrapped);
    },
    replayAfter(eventId) {
      if (!eventId) return [...buffer];
      const idx = buffer.findIndex((e) => e.id === eventId);
      return idx === -1 ? [...buffer] : buffer.slice(idx + 1);
    },
  };
}
