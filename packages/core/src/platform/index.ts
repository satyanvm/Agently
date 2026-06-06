/**
 * @agently/core/platform — the storage-agnostic application core.
 *
 * Assembles repositories, the event bus, services, a clock, and a logger into a
 * single `Platform` object. The default factory uses the in-memory store seeded
 * with the canonical dataset, which is exactly what the mock backend, local dev,
 * and tests need. Swapping in a Postgres-backed `Repositories` later changes
 * only `createPlatform`'s wiring — every service, route, and the worker stay put.
 */
import type { WorkspaceId } from "@agently/contracts";

import { systemClock, fixedClock, type Clock } from "./clock.js";
import { createLogger, type Logger } from "./logger.js";
import { createEventBus, type EventBus } from "./events/bus.js";
import { createMemoryRepositories } from "./repositories/memory.js";
import type { Repositories } from "./repositories/types.js";
import { buildSeed, NOW_ISO, type Seed } from "./seed.js";

import { createEmitter, type PlatformDeps } from "./services/context.js";
import { createWorkflowService, type WorkflowService } from "./services/workflowService.js";
import { createAgentService, type AgentService } from "./services/agentService.js";
import { createRunService, type RunService } from "./services/runService.js";
import { createLogService, type LogService } from "./services/logService.js";
import { createBrowserService, type BrowserService } from "./services/browserService.js";
import { createNotificationService, type NotificationService } from "./services/notificationService.js";
import { createActivityService, type ActivityService } from "./services/activityService.js";
import { createStatsService, type StatsService } from "./services/statsService.js";

export interface Platform {
  repos: Repositories;
  bus: EventBus;
  clock: Clock;
  logger: Logger;
  workspaceId: WorkspaceId;
  workflows: WorkflowService;
  agents: AgentService;
  runs: RunService;
  logs: LogService;
  browser: BrowserService;
  notifications: NotificationService;
  activity: ActivityService;
  stats: StatsService;
}

export interface CreatePlatformOptions {
  clock?: Clock;
  logger?: Logger;
  bus?: EventBus;
  /** Provide a custom store/seed; defaults to the in-memory seeded store. */
  repositories?: Repositories;
  seed?: Seed;
}

export function createPlatform(opts: CreatePlatformOptions = {}): Platform {
  // Seed timestamps are deterministic; the live clock drives new writes.
  const clock = opts.clock ?? systemClock;
  const logger = opts.logger ?? createLogger({ base: { svc: "platform" } });
  const bus = opts.bus ?? createEventBus();
  const seed = opts.seed ?? buildSeed(fixedClock(NOW_ISO));
  const repos = opts.repositories ?? createMemoryRepositories(seed.data);

  const workspaceId = repos.workspace.get().id;
  const deps: PlatformDeps = {
    repos,
    bus,
    clock,
    logger,
    seedStats: { workflowStats: seed.workflowStats, workspaceStats: seed.workspaceStats },
  };
  const emit = createEmitter(bus, clock, workspaceId);

  const logs = createLogService(deps, emit);

  return {
    repos,
    bus,
    clock,
    logger,
    workspaceId,
    workflows: createWorkflowService(deps),
    agents: createAgentService(deps),
    runs: createRunService(deps, emit, logs),
    logs,
    browser: createBrowserService(deps, emit),
    notifications: createNotificationService(deps, emit),
    activity: createActivityService(deps),
    stats: createStatsService(deps),
  };
}

export { systemClock, fixedClock } from "./clock.js";
export { createLogger } from "./logger.js";
export { createEventBus } from "./events/bus.js";
export { createMemoryRepositories } from "./repositories/memory.js";
export { buildSeed } from "./seed.js";
export type { Clock } from "./clock.js";
export type { Logger } from "./logger.js";
export type { EventBus, EventHandler } from "./events/bus.js";
export type { Repositories } from "./repositories/types.js";
export type { MemoryData } from "./repositories/memory.js";
export type { Seed } from "./seed.js";
export * from "./services/index.js";
