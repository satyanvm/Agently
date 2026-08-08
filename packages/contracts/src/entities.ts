import { z } from "zod";
import * as Id from "./ids.js";
import {
  RunStatus,
  AgentStatus,
  AgentRole,
  LogLevel,
  LogChannel,
  TriggerType,
  ArtifactKind,
  BrowserActionType,
  NotificationType,
  Severity,
  ActivityKind,
  MemberRole,
  WorkspacePlan,
} from "./enums.js";
import {
  Timestamp,
  Usage,
  Viewport,
  StepProgress,
  AgentMetrics,
  Principal,
} from "./primitives.js";

/**
 * The canonical domain model. Each schema is the single source of truth for an
 * entity's shape; the matching TypeScript type is inferred from it, so runtime
 * validation and compile-time types can never drift.
 *
 * The model is derived directly from what the frontend renders today and is
 * intentionally free of execution concerns (no runner, orchestrator, browser
 * driver, or provider details) so it survives those decisions.
 */

/* ============================ Tenancy ============================ */

export const Workspace = z.object({
  id: Id.WorkspaceId,
  name: z.string(),
  slug: z.string(),
  plan: WorkspacePlan,
  defaultRegion: z.string(),
  createdAt: Timestamp,
});
export type Workspace = z.infer<typeof Workspace>;

export const Member = z.object({
  id: Id.MemberId,
  workspaceId: Id.WorkspaceId,
  name: z.string(),
  email: z.string().email(),
  initials: z.string().min(1).max(3),
  role: MemberRole,
  createdAt: Timestamp,
});
export type Member = z.infer<typeof Member>;

export const ApiKey = z.object({
  id: Id.ApiKeyId,
  workspaceId: Id.WorkspaceId,
  name: z.string(),
  /** Display prefix only; the secret is never returned after creation. */
  maskedKey: z.string(),
  scopes: z.array(z.string()),
  lastUsedAt: Timestamp.nullable(),
  createdAt: Timestamp,
});
export type ApiKey = z.infer<typeof ApiKey>;

/* ======================= Agent definitions ====================== */

/** A reusable agent (the building block shown on /agents). Definitional only —
 *  live status/metrics live on a RunAgent. */
export const AgentDefinition = z.object({
  id: Id.AgentDefinitionId,
  workspaceId: Id.WorkspaceId,
  name: z.string(),
  role: AgentRole,
  model: z.string(),
  description: z.string(),
  tools: z.array(z.string()),
  /** Runner-specific config; shape intentionally open until a framework lands. */
  config: z.record(z.unknown()).default({}),
  createdAt: Timestamp,
  updatedAt: Timestamp,
});
export type AgentDefinition = z.infer<typeof AgentDefinition>;

/* ========================== Workflows =========================== */

/** A node in a workflow's versioned graph (definition-time placement + deps). */
export const GraphNode = z.object({
  /** Stable key within the version; becomes a RunAgent at launch. */
  key: z.string(),
  agentDefinitionId: Id.AgentDefinitionId.nullable(),
  name: z.string(),
  role: AgentRole,
  model: z.string(),
  col: z.number().int().nonnegative(),
  row: z.number().int().nonnegative(),
  dependsOn: z.array(z.string()),
  /**
   * Catalog id from the visual builder's node palette (e.g. "agent.llm",
   * "tool.http", "logic.branch"). Defaults to "agent.llm" so graphs compiled
   * from a prompt — which predate the builder and carry only agent nodes —
   * keep behaving exactly as before.
   */
  type: z.string().default("agent.llm"),
  /**
   * Per-node configuration, shape determined by `type` (e.g. {url, method} for
   * tool.http, {condition} for logic.branch). Open by design so new node types
   * don't require a schema change; the reasoner's executor for each type reads
   * the keys it needs.
   */
  config: z.record(z.unknown()).default({}),
});
export type GraphNode = z.infer<typeof GraphNode>;

export const WorkflowVersion = z.object({
  id: Id.WorkflowVersionId,
  workflowId: Id.WorkflowId,
  version: z.number().int().positive(),
  nodes: z.array(GraphNode),
  note: z.string().optional(),
  createdBy: Id.MemberId.nullable(),
  createdAt: Timestamp,
});
export type WorkflowVersion = z.infer<typeof WorkflowVersion>;

export const Workflow = z.object({
  id: Id.WorkflowId,
  workspaceId: Id.WorkspaceId,
  slug: z.string(),
  name: z.string(),
  description: z.string(),
  trigger: TriggerType,
  schedule: z.string().nullable(),
  tags: z.array(z.string()),
  ownerId: Id.MemberId.nullable(),
  agentCount: z.number().int().nonnegative(),
  currentVersionId: Id.WorkflowVersionId.nullable(),
  createdAt: Timestamp,
  updatedAt: Timestamp,
  archivedAt: Timestamp.nullable(),
});
export type Workflow = z.infer<typeof Workflow>;

/** Rollups attached to a workflow when listed (computed, not stored). */
export const WorkflowStats = z.object({
  successRate: z.number().min(0).max(1),
  avgRuntimeMs: z.number().int().nonnegative(),
  avgCostUsd: z.number().nonnegative(),
  totalRuns: z.number().int().nonnegative(),
  /** Last N runs, 1 = success 0 = fail, for the health bar. */
  recent: z.array(z.union([z.literal(0), z.literal(1)])),
  trend: z.array(z.number()),
});
export type WorkflowStats = z.infer<typeof WorkflowStats>;

/** Read model the workflows list/detail return: workflow + derived view fields. */
export const WorkflowSummary = Workflow.extend({
  status: RunStatus,
  owner: Principal.nullable(),
  lastRunAt: Timestamp.nullable(),
  stats: WorkflowStats,
});
export type WorkflowSummary = z.infer<typeof WorkflowSummary>;

/* ============================= Runs ============================= */

/** A run-scoped agent instance (a node in the live graph). */
export const RunAgent = z.object({
  id: Id.RunAgentId,
  runId: Id.RunId,
  agentDefinitionId: Id.AgentDefinitionId.nullable(),
  name: z.string(),
  role: AgentRole,
  model: z.string(),
  status: AgentStatus,
  dependsOn: z.array(Id.RunAgentId),
  col: z.number().int().nonnegative(),
  row: z.number().int().nonnegative(),
  summary: z.string(),
  metrics: AgentMetrics,
  startedAt: Timestamp.nullable(),
  finishedAt: Timestamp.nullable(),
});
export type RunAgent = z.infer<typeof RunAgent>;

/** A message/hand-off between two run agents (graph edge with payload). */
export const AgentMessage = z.object({
  id: Id.AgentMessageId,
  runId: Id.RunId,
  fromAgentId: Id.RunAgentId,
  toAgentId: Id.RunAgentId,
  label: z.string(),
  at: Timestamp,
});
export type AgentMessage = z.infer<typeof AgentMessage>;

export const Artifact = z.object({
  id: Id.ArtifactId,
  runId: Id.RunId,
  name: z.string(),
  kind: ArtifactKind,
  sizeBytes: z.number().int().nonnegative().nullable(),
  producedByAgentId: Id.RunAgentId.nullable(),
  producedByName: z.string(),
  /** Opaque storage pointer, resolved to a signed URL at read time later. */
  storageKey: z.string().nullable(),
  preview: z.string().nullable(),
  createdAt: Timestamp,
});
export type Artifact = z.infer<typeof Artifact>;

export const Run = z.object({
  id: Id.RunId,
  workspaceId: Id.WorkspaceId,
  workflowId: Id.WorkflowId,
  workflowVersionId: Id.WorkflowVersionId.nullable(),
  workflowName: z.string(),
  workflowSlug: z.string(),
  number: z.number().int().positive(),
  status: RunStatus,
  trigger: TriggerType,
  triggeredBy: Principal,
  region: z.string(),
  steps: StepProgress,
  currentStep: z.string(),
  costUsd: z.number().nonnegative(),
  usage: Usage,
  error: z.string().nullable(),
  browserSessionId: Id.BrowserSessionId.nullable(),
  /** Deep-link handle into self-hosted Langfuse; set by the reasoner. */
  langfuseTraceId: z.string().nullable().default(null),
  queuedAt: Timestamp,
  startedAt: Timestamp.nullable(),
  finishedAt: Timestamp.nullable(),
});
export type Run = z.infer<typeof Run>;

/** Run + its expanded children, returned by the run detail endpoint. */
export const RunDetail = Run.extend({
  agents: z.array(RunAgent),
  messages: z.array(AgentMessage),
  artifacts: z.array(Artifact),
});
export type RunDetail = z.infer<typeof RunDetail>;

/* ============================= Logs ============================= */

export const LogEntry = z.object({
  id: Id.LogId,
  runId: Id.RunId,
  /** Monotonic per-run sequence; stable ordering even if timestamps collide. */
  seq: z.number().int().nonnegative(),
  ts: Timestamp,
  /** ms since run start, for the timeline column. */
  offsetMs: z.number().int().nonnegative(),
  level: LogLevel,
  channel: LogChannel,
  source: z.string(),
  message: z.string(),
  detail: z.string().nullable(),
  reasoning: z.boolean(),
});
export type LogEntry = z.infer<typeof LogEntry>;

/* ======================== Browser sessions ====================== */

export const BrowserAction = z.object({
  id: Id.BrowserActionId,
  sessionId: Id.BrowserSessionId,
  ts: Timestamp,
  type: BrowserActionType,
  target: z.string(),
  value: z.string().nullable(),
  status: z.enum(["ok", "error", "pending"]),
  durationMs: z.number().int().nonnegative(),
});
export type BrowserAction = z.infer<typeof BrowserAction>;

export const BrowserShot = z.object({
  id: Id.BrowserShotId,
  sessionId: Id.BrowserSessionId,
  ts: Timestamp,
  url: z.string(),
  title: z.string(),
  label: z.string(),
  /** Storage pointer for the captured image; signed at read time later. */
  storageKey: z.string().nullable(),
});
export type BrowserShot = z.infer<typeof BrowserShot>;

export const BrowserConsoleLine = z.object({
  ts: Timestamp,
  level: LogLevel,
  text: z.string(),
});
export type BrowserConsoleLine = z.infer<typeof BrowserConsoleLine>;

export const BrowserSession = z.object({
  id: Id.BrowserSessionId,
  runId: Id.RunId,
  agentName: z.string(),
  status: RunStatus,
  currentUrl: z.string(),
  pageTitle: z.string(),
  viewport: Viewport,
  pagesVisited: z.number().int().nonnegative(),
  actionsCount: z.number().int().nonnegative(),
  startedAt: Timestamp,
  finishedAt: Timestamp.nullable(),
});
export type BrowserSession = z.infer<typeof BrowserSession>;

export const BrowserSessionDetail = BrowserSession.extend({
  shots: z.array(BrowserShot),
  actions: z.array(BrowserAction),
  console: z.array(BrowserConsoleLine),
});
export type BrowserSessionDetail = z.infer<typeof BrowserSessionDetail>;

/* ====================== Notifications & feed ==================== */

export const Notification = z.object({
  id: Id.NotificationId,
  workspaceId: Id.WorkspaceId,
  recipientId: Id.MemberId.nullable(),
  type: NotificationType,
  severity: Severity,
  title: z.string(),
  body: z.string(),
  workflowSlug: z.string().nullable(),
  runId: Id.RunId.nullable(),
  runNumber: z.number().int().positive().nullable(),
  readAt: Timestamp.nullable(),
  createdAt: Timestamp,
});
export type Notification = z.infer<typeof Notification>;

export const ActivityEvent = z.object({
  id: Id.ActivityId,
  workspaceId: Id.WorkspaceId,
  kind: ActivityKind,
  actor: z.string(),
  text: z.string(),
  workflowSlug: z.string().nullable(),
  runId: Id.RunId.nullable(),
  at: Timestamp,
});
export type ActivityEvent = z.infer<typeof ActivityEvent>;

/* ========================= Dashboard ========================== */

export const WorkspaceStats = z.object({
  activeRuns: z.number().int().nonnegative(),
  runsToday: z.number().int().nonnegative(),
  successRate: z.number().min(0).max(1),
  spendTodayUsd: z.number().nonnegative(),
  spendBudgetUsd: z.number().nonnegative(),
  computeHours: z.number().nonnegative(),
  tokensToday: z.number().int().nonnegative(),
  /** 24×1h buckets. */
  runVolume: z.array(z.number()),
  spendSeries: z.array(z.number()),
});
export type WorkspaceStats = z.infer<typeof WorkspaceStats>;
