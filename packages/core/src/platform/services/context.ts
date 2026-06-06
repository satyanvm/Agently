import type { DomainEvent, WorkflowStats, WorkspaceStats, WorkspaceId } from "@agently/contracts";
import { ids } from "@agently/contracts";
import type { Repositories } from "../repositories/types.js";
import type { EventBus } from "../events/bus.js";
import type { Clock } from "../clock.js";
import type { Logger } from "../logger.js";

/** Everything the services need, injected once at platform construction. */
export interface PlatformDeps {
  repos: Repositories;
  bus: EventBus;
  clock: Clock;
  logger: Logger;
  /** Materialized rollups from the seed (a stand-in for a future stats table). */
  seedStats: {
    workflowStats: Record<string, WorkflowStats>;
    workspaceStats: WorkspaceStats;
  };
}

/** Distributive Omit so each union member keeps its own payload fields. */
type DistributiveOmit<T, K extends keyof never> = T extends unknown ? Omit<T, K> : never;
export type EmitInput = DistributiveOmit<DomainEvent, "id" | "occurredAt" | "workspaceId">;

/** Build a publisher that stamps every event with id/time/tenant. */
export function createEmitter(bus: EventBus, clock: Clock, workspaceId: WorkspaceId) {
  return (event: EmitInput): void => {
    bus.publish({
      id: ids.event(),
      occurredAt: clock.iso(),
      workspaceId,
      ...event,
    } as DomainEvent);
  };
}

export type Emitter = ReturnType<typeof createEmitter>;
// re-exported for convenience at call sites
export { ids };
