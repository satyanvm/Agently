/**
 * Shared domain types. These mirror the database schema (packages/db/migrations)
 * and are the common vocabulary used by web routes, services, the queue, and runners.
 */

export type AgentType = "llm-loop" | "task" | "code";

export type RunStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled";

export type LogLevel = "debug" | "info" | "warn" | "error";

export interface Agent {
  id: string;
  userId: string;
  name: string;
  type: AgentType;
  /** Runner-specific configuration. Shape depends on `type`. */
  config: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
}

export interface Run {
  id: string;
  agentId: string;
  userId: string;
  status: RunStatus;
  /** Optional per-run input passed to the runner. */
  input: Record<string, unknown> | null;
  /** Final result or error summary, set when the run finishes. */
  result: Record<string, unknown> | null;
  error: string | null;
  queuedAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface LogEntry {
  id: string;
  runId: string;
  level: LogLevel;
  message: string;
  /** Monotonic sequence within a run, for stable ordering of streamed logs. */
  seq: number;
  createdAt: string;
}

/** Derived helper: runtime in ms, or null if not started. */
export function runtimeMs(run: Pick<Run, "startedAt" | "finishedAt">): number | null {
  if (!run.startedAt) return null;
  const end = run.finishedAt ? Date.parse(run.finishedAt) : Date.now();
  return end - Date.parse(run.startedAt);
}

export const TERMINAL_STATUSES: readonly RunStatus[] = [
  "succeeded",
  "failed",
  "canceled",
];

export function isTerminal(status: RunStatus): boolean {
  return TERMINAL_STATUSES.includes(status);
}
