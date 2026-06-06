import { z } from "zod";
import * as Id from "./ids.js";
import { Timestamp } from "./primitives.js";
import { RunStatus, AgentStatus } from "./enums.js";
import { LogEntry, BrowserAction } from "./entities.js";

/**
 * Domain events — the spine of the realtime and notification systems.
 *
 * Everything the UI shows "live" (a run advancing, a log line streaming, an
 * agent transitioning, a browser action, a cost threshold crossed) is a fact
 * that happened. Modeling those facts as events is the single most durable
 * decision here: it is independent of the runner, the transport (SSE/WebSocket/
 * polling), and the store. Notifications, the activity feed, the log stream, and
 * future audit/webhooks are all just projections of this stream.
 *
 * Each event is a discriminated union member keyed by `type`. The envelope adds
 * identity, ordering, and tenancy.
 */

const base = {
  id: Id.EventId,
  occurredAt: Timestamp,
  workspaceId: Id.WorkspaceId,
};

export const RunQueued = z.object({
  ...base,
  type: z.literal("run.queued"),
  runId: Id.RunId,
  workflowId: Id.WorkflowId,
});

export const RunStarted = z.object({
  ...base,
  type: z.literal("run.started"),
  runId: Id.RunId,
});

export const RunProgress = z.object({
  ...base,
  type: z.literal("run.progress"),
  runId: Id.RunId,
  stepsDone: z.number().int().nonnegative(),
  stepsTotal: z.number().int().nonnegative(),
  currentStep: z.string(),
});

export const RunAgentTransitioned = z.object({
  ...base,
  type: z.literal("run.agent.transitioned"),
  runId: Id.RunId,
  agentId: Id.RunAgentId,
  from: AgentStatus,
  to: AgentStatus,
});

export const RunLogAppended = z.object({
  ...base,
  type: z.literal("run.log.appended"),
  runId: Id.RunId,
  log: LogEntry,
});

export const RunBrowserAction = z.object({
  ...base,
  type: z.literal("run.browser.action"),
  runId: Id.RunId,
  sessionId: Id.BrowserSessionId,
  action: BrowserAction,
});

export const RunArtifactProduced = z.object({
  ...base,
  type: z.literal("run.artifact.produced"),
  runId: Id.RunId,
  artifactId: Id.ArtifactId,
});

export const RunCostThreshold = z.object({
  ...base,
  type: z.literal("run.cost.threshold"),
  runId: Id.RunId,
  costUsd: z.number().nonnegative(),
  thresholdUsd: z.number().nonnegative(),
});

export const RunFinished = z.object({
  ...base,
  type: z.literal("run.finished"),
  runId: Id.RunId,
  status: RunStatus,
  error: z.string().nullable(),
});

export const NotificationCreated = z.object({
  ...base,
  type: z.literal("notification.created"),
  notificationId: Id.NotificationId,
});

export const DomainEvent = z.discriminatedUnion("type", [
  RunQueued,
  RunStarted,
  RunProgress,
  RunAgentTransitioned,
  RunLogAppended,
  RunBrowserAction,
  RunArtifactProduced,
  RunCostThreshold,
  RunFinished,
  NotificationCreated,
]);
export type DomainEvent = z.infer<typeof DomainEvent>;

export type DomainEventType = DomainEvent["type"];

/** Narrow an event by type (typed filter for subscribers). */
export function isEvent<T extends DomainEventType>(
  e: DomainEvent,
  type: T,
): e is Extract<DomainEvent, { type: T }> {
  return e.type === type;
}

/** The full set of event type strings — handy for channel routing. */
export const DOMAIN_EVENT_TYPES = [
  "run.queued",
  "run.started",
  "run.progress",
  "run.agent.transitioned",
  "run.log.appended",
  "run.browser.action",
  "run.artifact.produced",
  "run.cost.threshold",
  "run.finished",
  "notification.created",
] as const satisfies readonly DomainEventType[];
