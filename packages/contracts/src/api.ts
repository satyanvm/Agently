import { z } from "zod";
import {
  TriggerType,
  AgentRole,
  NotificationType,
} from "./enums.js";
import { PageQuery, pageOf } from "./primitives.js";
import {
  WorkflowSummary,
  Run,
  RunDetail,
  RunAgent,
  AgentMessage,
  Artifact,
  LogEntry,
  BrowserSessionDetail,
  AgentDefinition,
  Notification,
  ActivityEvent,
  WorkspaceStats,
} from "./entities.js";

/**
 * The HTTP contract. Each operation declares its method, path template, and the
 * zod schemas for its query/params/body/response. Servers validate against
 * these; a future typed client can be generated from them; tests assert against
 * them. The contract is the interface, not the implementation — it outlives any
 * backend rewrite.
 */

/* ----------------------------- inputs ---------------------------- */

export const LaunchRunInput = z.object({
  /** Optional structured input handed to the workflow at launch. */
  input: z.record(z.unknown()).optional(),
  trigger: TriggerType.default("manual"),
  region: z.string().optional(),
});
export type LaunchRunInput = z.infer<typeof LaunchRunInput>;

export const CreateWorkflowInput = z.object({
  name: z.string().min(1).max(120),
  description: z.string().max(2000).default(""),
  trigger: TriggerType.default("manual"),
  schedule: z.string().nullable().default(null),
  tags: z.array(z.string()).default([]),
});
export type CreateWorkflowInput = z.infer<typeof CreateWorkflowInput>;

export const CreateAgentInput = z.object({
  name: z.string().min(1).max(120),
  role: AgentRole,
  model: z.string().min(1),
  description: z.string().max(2000).default(""),
  tools: z.array(z.string()).default([]),
  config: z.record(z.unknown()).default({}),
});
export type CreateAgentInput = z.infer<typeof CreateAgentInput>;

export const ListRunsQuery = PageQuery.extend({
  status: z.string().optional(),
  workflowId: z.string().optional(),
});
export type ListRunsQuery = z.infer<typeof ListRunsQuery>;

export const ListLogsQuery = PageQuery.extend({
  /** Filter to lines at or above a severity. */
  severity: z.enum(["all", "warn", "error"]).default("all"),
  channel: z.string().optional(),
  source: z.string().optional(),
  q: z.string().optional(),
  /** Resume streaming/paging after this seq. */
  afterSeq: z.coerce.number().int().nonnegative().optional(),
});
export type ListLogsQuery = z.infer<typeof ListLogsQuery>;

export const ListNotificationsQuery = PageQuery.extend({
  unread: z.coerce.boolean().optional(),
  type: NotificationType.optional(),
});
export type ListNotificationsQuery = z.infer<typeof ListNotificationsQuery>;

/* --------------------------- catalog ----------------------------- */

export type HttpMethod = "GET" | "POST" | "PATCH" | "DELETE";

export interface EndpointDef<
  P extends z.ZodTypeAny = z.ZodTypeAny,
  Q extends z.ZodTypeAny = z.ZodTypeAny,
  B extends z.ZodTypeAny = z.ZodTypeAny,
  R extends z.ZodTypeAny = z.ZodTypeAny,
> {
  method: HttpMethod;
  /** Path template with `:param` segments. */
  path: string;
  summary: string;
  params?: P;
  query?: Q;
  body?: B;
  response: R;
}

function endpoint<
  P extends z.ZodTypeAny,
  Q extends z.ZodTypeAny,
  B extends z.ZodTypeAny,
  R extends z.ZodTypeAny,
>(def: EndpointDef<P, Q, B, R>): EndpointDef<P, Q, B, R> {
  return def;
}

const slugParam = z.object({ slug: z.string() });
const idParam = z.object({ id: z.string() });

/**
 * The endpoint registry. Keys are stable operation ids (`runs.launch`) used by
 * clients, tests, and telemetry.
 */
export const contract = {
  "health.check": endpoint({
    method: "GET",
    path: "/api/health",
    summary: "Liveness + build info",
    response: z.object({ ok: z.boolean(), service: z.string(), time: z.string() }),
  }),

  "dashboard.stats": endpoint({
    method: "GET",
    path: "/api/dashboard",
    summary: "Workspace dashboard aggregates",
    response: WorkspaceStats,
  }),

  "workflows.list": endpoint({
    method: "GET",
    path: "/api/workflows",
    summary: "List workflows with rollup stats",
    query: PageQuery,
    response: pageOf(WorkflowSummary),
  }),
  "workflows.get": endpoint({
    method: "GET",
    path: "/api/workflows/:slug",
    summary: "Get a single workflow",
    params: slugParam,
    response: WorkflowSummary,
  }),
  "workflows.create": endpoint({
    method: "POST",
    path: "/api/workflows",
    summary: "Create a workflow",
    body: CreateWorkflowInput,
    response: WorkflowSummary,
  }),

  "runs.list": endpoint({
    method: "GET",
    path: "/api/runs",
    summary: "List runs across the workspace",
    query: ListRunsQuery,
    response: pageOf(Run),
  }),
  "runs.get": endpoint({
    method: "GET",
    path: "/api/runs/:id",
    summary: "Get a run with its agents, messages, and artifacts",
    params: idParam,
    response: RunDetail,
  }),
  "runs.launch": endpoint({
    method: "POST",
    path: "/api/workflows/:slug/runs",
    summary: "Launch a new run of a workflow",
    params: slugParam,
    body: LaunchRunInput,
    response: RunDetail,
  }),
  "runs.cancel": endpoint({
    method: "POST",
    path: "/api/runs/:id/cancel",
    summary: "Request cancellation of a run",
    params: idParam,
    response: Run,
  }),
  "runs.agents": endpoint({
    method: "GET",
    path: "/api/runs/:id/agents",
    summary: "Agents + messages for a run (graph)",
    params: idParam,
    response: z.object({ agents: z.array(RunAgent), messages: z.array(AgentMessage) }),
  }),
  "runs.artifacts": endpoint({
    method: "GET",
    path: "/api/runs/:id/artifacts",
    summary: "Artifacts produced by a run",
    params: idParam,
    response: z.array(Artifact),
  }),
  "runs.logs": endpoint({
    method: "GET",
    path: "/api/runs/:id/logs",
    summary: "Paged, filterable run logs",
    params: idParam,
    query: ListLogsQuery,
    response: pageOf(LogEntry),
  }),
  "runs.logs.stream": endpoint({
    method: "GET",
    path: "/api/runs/:id/logs/stream",
    summary: "Server-Sent Events stream of new log lines",
    params: idParam,
    response: z.unknown(), // SSE; framed as text/event-stream
  }),
  "runs.browser": endpoint({
    method: "GET",
    path: "/api/runs/:id/browser",
    summary: "Browser session for a run",
    params: idParam,
    response: BrowserSessionDetail,
  }),

  "agents.list": endpoint({
    method: "GET",
    path: "/api/agents",
    summary: "List reusable agent definitions",
    query: PageQuery,
    response: pageOf(AgentDefinition),
  }),
  "agents.create": endpoint({
    method: "POST",
    path: "/api/agents",
    summary: "Create an agent definition",
    body: CreateAgentInput,
    response: AgentDefinition,
  }),

  "notifications.list": endpoint({
    method: "GET",
    path: "/api/notifications",
    summary: "List notifications",
    query: ListNotificationsQuery,
    response: pageOf(Notification),
  }),
  "notifications.read": endpoint({
    method: "POST",
    path: "/api/notifications/:id/read",
    summary: "Mark a notification read",
    params: idParam,
    response: Notification,
  }),
  "notifications.readAll": endpoint({
    method: "POST",
    path: "/api/notifications/read-all",
    summary: "Mark all notifications read",
    response: z.object({ updated: z.number().int().nonnegative() }),
  }),

  "activity.list": endpoint({
    method: "GET",
    path: "/api/activity",
    summary: "Recent workspace activity feed",
    query: PageQuery,
    response: pageOf(ActivityEvent),
  }),

  "events.stream": endpoint({
    method: "GET",
    path: "/api/events/stream",
    summary: "Server-Sent Events stream of workspace domain events",
    response: z.unknown(),
  }),
} as const;

export type OperationId = keyof typeof contract;
