/**
 * Frontend domain model for the Agently control plane.
 *
 * This mirrors the canonical vocabulary served by the Go API (apps/api —
 * Agent / Run / LogEntry) with the richer, UI-facing concepts the product surfaces: multi-agent
 * workflows, agent graphs, browser sessions, artifacts, cost, and notifications.
 * All data is mock — see lib/mock-data.ts.
 */

export type RunStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled"
  | "paused";

export type AgentStatus =
  | "idle"
  | "running"
  | "succeeded"
  | "failed"
  | "blocked"
  | "waiting";

export type LogLevel = "debug" | "info" | "warn" | "error" | "success";

/** Where a log line originated — drives the channel filter + glyphs. */
export type LogChannel = "system" | "agent" | "tool" | "browser" | "model";

export type AgentRole =
  | "orchestrator"
  | "researcher"
  | "browser"
  | "coder"
  | "analyst"
  | "writer"
  | "validator";

export interface RunStatsLite {
  successRate: number; // 0..1
  avgRuntimeMs: number;
  avgCostUsd: number;
  totalRuns: number;
  /** Last 14 runs, 1 = success 0 = fail — for the health bar. */
  recent: (0 | 1)[];
  /** Daily run counts for sparkline. */
  trend: number[];
}

export interface Workflow {
  id: string;
  slug: string;
  name: string;
  description: string;
  status: RunStatus;
  agentCount: number;
  trigger: "manual" | "schedule" | "webhook" | "event";
  schedule?: string;
  owner: { name: string; initials: string };
  tags: string[];
  createdAt: string;
  lastRunAt: string;
  stats: RunStatsLite;
}

export interface AgentNode {
  id: string;
  name: string;
  role: AgentRole;
  model: string;
  status: AgentStatus;
  dependsOn: string[];
  /** Layout column / row for the graph (0-indexed). */
  col: number;
  row: number;
  summary: string;
  metrics: {
    tokens: number;
    costUsd: number;
    runtimeMs: number | null;
    toolCalls: number;
    progress: number; // 0..1
  };
}

export interface AgentMessage {
  id: string;
  from: string; // agent id
  to: string; // agent id
  label: string;
  at: string;
}

export interface RunArtifact {
  id: string;
  name: string;
  kind: "file" | "json" | "image" | "dataset" | "report" | "url" | "code";
  sizeBytes?: number;
  producedBy: string; // agent name
  at: string;
  preview?: string;
}

export interface WorkflowRun {
  id: string;
  number: number; // #142
  workflowId: string;
  workflowName: string;
  workflowSlug: string;
  status: RunStatus;
  trigger: "manual" | "schedule" | "webhook" | "event";
  triggeredBy: { name: string; initials: string };
  region: string;
  startedAt: string | null;
  finishedAt: string | null;
  queuedAt: string;
  costUsd: number;
  tokensIn: number;
  tokensOut: number;
  /** completed / total steps. */
  steps: { done: number; total: number };
  currentStep: string;
  agents: AgentNode[];
  messages: AgentMessage[];
  artifacts: RunArtifact[];
  /** id of the primary browser session, if any. */
  browserSessionId?: string;
}

export interface LogEntry {
  id: string;
  runId: string;
  seq: number;
  ts: string;
  level: LogLevel;
  channel: LogChannel;
  source: string; // agent or subsystem name
  message: string;
  /** Optional structured detail rendered as a collapsible block. */
  detail?: string;
  /** For model channel: a reasoning trace snippet. */
  reasoning?: boolean;
  /** ms since run start, for timeline column. */
  offsetMs: number;
}

export type BrowserActionType =
  | "navigate"
  | "click"
  | "type"
  | "scroll"
  | "extract"
  | "wait"
  | "screenshot"
  | "submit";

export interface BrowserAction {
  id: string;
  ts: string;
  type: BrowserActionType;
  target: string;
  value?: string;
  status: "ok" | "error" | "pending";
  durationMs: number;
}

export interface BrowserShot {
  id: string;
  ts: string;
  url: string;
  title: string;
  /** Tailwind gradient classes used to fake a screenshot thumbnail. */
  tone: string;
  label: string;
}

export interface BrowserSession {
  id: string;
  runId: string;
  agentName: string;
  status: RunStatus;
  currentUrl: string;
  pageTitle: string;
  viewport: { w: number; h: number };
  startedAt: string;
  pagesVisited: number;
  actionsCount: number;
  shots: BrowserShot[];
  actions: BrowserAction[];
  console: { ts: string; level: LogLevel; text: string }[];
}

export type NotificationType =
  | "workflow.completed"
  | "workflow.failed"
  | "browser.error"
  | "cost.alert"
  | "agent.blocked"
  | "run.started";

export interface AppNotification {
  id: string;
  type: NotificationType;
  title: string;
  body: string;
  at: string;
  read: boolean;
  workflowSlug?: string;
  runNumber?: number;
}

export interface ActivityItem {
  id: string;
  kind: "deploy" | "run" | "complete" | "fail" | "comment" | "scale";
  actor: string;
  text: string;
  at: string;
  workflowSlug?: string;
}
