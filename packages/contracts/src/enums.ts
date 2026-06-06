import { z } from "zod";

/**
 * Closed vocabularies. These mirror the values the frontend already renders
 * (see apps/web/lib/types.ts) so the API and UI never disagree. They are pure
 * product concepts and are independent of how agents actually execute.
 */

export const RunStatus = z.enum([
  "queued",
  "running",
  "succeeded",
  "failed",
  "canceled",
  "paused",
]);
export type RunStatus = z.infer<typeof RunStatus>;

export const TERMINAL_RUN_STATUSES: readonly RunStatus[] = [
  "succeeded",
  "failed",
  "canceled",
];
export function isTerminalRunStatus(s: RunStatus): boolean {
  return TERMINAL_RUN_STATUSES.includes(s);
}

export const AgentStatus = z.enum([
  "idle",
  "running",
  "succeeded",
  "failed",
  "blocked",
  "waiting",
]);
export type AgentStatus = z.infer<typeof AgentStatus>;

export const AgentRole = z.enum([
  "orchestrator",
  "researcher",
  "browser",
  "coder",
  "analyst",
  "writer",
  "validator",
]);
export type AgentRole = z.infer<typeof AgentRole>;

export const LogLevel = z.enum(["debug", "info", "success", "warn", "error"]);
export type LogLevel = z.infer<typeof LogLevel>;

export const LogChannel = z.enum(["system", "agent", "tool", "browser", "model"]);
export type LogChannel = z.infer<typeof LogChannel>;

export const TriggerType = z.enum(["manual", "schedule", "webhook", "event"]);
export type TriggerType = z.infer<typeof TriggerType>;

export const ArtifactKind = z.enum([
  "file",
  "json",
  "image",
  "dataset",
  "report",
  "url",
  "code",
]);
export type ArtifactKind = z.infer<typeof ArtifactKind>;

export const BrowserActionType = z.enum([
  "navigate",
  "click",
  "type",
  "scroll",
  "extract",
  "wait",
  "screenshot",
  "submit",
]);
export type BrowserActionType = z.infer<typeof BrowserActionType>;

export const NotificationType = z.enum([
  "workflow.completed",
  "workflow.failed",
  "browser.error",
  "cost.alert",
  "agent.blocked",
  "run.started",
]);
export type NotificationType = z.infer<typeof NotificationType>;

export const Severity = z.enum(["info", "success", "warning", "error"]);
export type Severity = z.infer<typeof Severity>;

export const ActivityKind = z.enum([
  "deploy",
  "run",
  "complete",
  "fail",
  "comment",
  "scale",
]);
export type ActivityKind = z.infer<typeof ActivityKind>;

export const MemberRole = z.enum(["owner", "admin", "member"]);
export type MemberRole = z.infer<typeof MemberRole>;

export const WorkspacePlan = z.enum(["free", "pro", "enterprise"]);
export type WorkspacePlan = z.infer<typeof WorkspacePlan>;

/** Region identifiers the UI surfaces. Open-ended in practice; validated loosely. */
export const Region = z.enum([
  "us-east-1",
  "us-west-2",
  "eu-central-1",
  "ap-southeast-1",
]);
export type Region = z.infer<typeof Region>;
