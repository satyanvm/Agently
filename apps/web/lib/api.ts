/**
 * Real API client for the Go backend (apps/api), reached via the same-origin
 * /api/* proxy configured in next.config.ts.
 *
 * Every page consumes the live Go API through this module — there is no mock
 * data anymore. The backend's JSON is almost identical to the frontend's UI types
 * (lib/types.ts); the only shape gaps are normalized here (e.g. the API nests
 * token usage under `usage`, the UI wants it flat), so components keep consuming
 * the types they already know.
 */

import type { WorkflowRun, LogEntry, AgentNode, AgentMessage, BrowserSession } from "./types";

/** Cursor-paginated envelope returned by list endpoints. */
interface Page<T> {
  items: T[];
  nextCursor: string | null;
  total?: number;
}

/** The run as the API serves it (usage nested; no expanded children on list). */
interface ApiRun {
  id: string;
  number: number;
  workflowId: string;
  workflowName: string;
  workflowSlug: string;
  status: WorkflowRun["status"];
  trigger: WorkflowRun["trigger"];
  triggeredBy: { name: string; initials: string };
  region: string;
  startedAt: string | null;
  finishedAt: string | null;
  queuedAt: string;
  costUsd: number;
  usage: { tokensIn: number; tokensOut: number };
  steps: { done: number; total: number };
  currentStep: string;
  browserSessionId: string | null;
}

/** Normalize an API run into the UI's WorkflowRun (flatten usage, default lists). */
function toWorkflowRun(r: ApiRun): WorkflowRun {
  return {
    id: r.id,
    number: r.number,
    workflowId: r.workflowId,
    workflowName: r.workflowName,
    workflowSlug: r.workflowSlug,
    status: r.status,
    trigger: r.trigger,
    triggeredBy: r.triggeredBy,
    region: r.region,
    startedAt: r.startedAt,
    finishedAt: r.finishedAt,
    queuedAt: r.queuedAt,
    costUsd: r.costUsd,
    tokensIn: r.usage?.tokensIn ?? 0,
    tokensOut: r.usage?.tokensOut ?? 0,
    steps: r.steps,
    currentStep: r.currentStep,
    agents: [],
    messages: [],
    artifacts: [],
    browserSessionId: r.browserSessionId ?? undefined,
  };
}

async function getJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "content-type": "application/json", ...(init?.headers ?? {}) },
    cache: "no-store",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`API ${res.status} on ${path}: ${body.slice(0, 200)}`);
  }
  return res.json() as Promise<T>;
}

/** List runs (newest first), normalized to UI types. */
export async function fetchRuns(): Promise<WorkflowRun[]> {
  const page = await getJSON<Page<ApiRun>>("/api/runs");
  return page.items.map(toWorkflowRun);
}

/**
 * The run detail endpoint returns the run plus expanded children (agents,
 * messages, artifacts). We reuse ApiRun's fields and pick the extras loosely —
 * the detail page only needs artifacts + agents shapes the UI already has.
 */
/** The API's agent shape (a superset of the UI's AgentNode). */
interface ApiAgent {
  id: string;
  name: string;
  role: AgentNode["role"];
  model: string;
  status: AgentNode["status"];
  dependsOn: string[];
  col: number;
  row: number;
  summary: string;
  metrics: AgentNode["metrics"];
}

interface ApiMessage {
  id: string;
  fromAgentId: string;
  toAgentId: string;
  label: string;
  at: string;
}

interface ApiRunDetail extends ApiRun {
  agents?: ApiAgent[];
  messages?: ApiMessage[];
  artifacts?: {
    id: string;
    name: string;
    kind: string;
    producedByName?: string;
    preview?: string | null;
    createdAt?: string;
  }[];
}

/** Fetch one run (detail), normalized. Returns null on 404. */
export async function fetchRun(runId: string): Promise<WorkflowRun | null> {
  try {
    const r = await getJSON<ApiRunDetail>(`/api/runs/${runId}`);
    const run = toWorkflowRun(r);

    // Agents: the API shape is a superset of AgentNode — pick the fields the graph
    // + inspector use. This is what drives the live Agents tab and dependency graph.
    run.agents = (r.agents ?? []).map(
      (a): AgentNode => ({
        id: a.id,
        name: a.name,
        role: a.role,
        model: a.model,
        status: a.status,
        dependsOn: a.dependsOn ?? [],
        col: a.col,
        row: a.row,
        summary: a.summary ?? "",
        metrics: a.metrics,
      }),
    );

    // Messages: API uses fromAgentId/toAgentId; the UI wants from/to.
    run.messages = (r.messages ?? []).map(
      (m): AgentMessage => ({ id: m.id, from: m.fromAgentId, to: m.toAgentId, label: m.label, at: m.at }),
    );

    // Artifacts → RunArtifact (best-effort; optional fields).
    run.artifacts = (r.artifacts ?? []).map((a) => ({
      id: a.id,
      name: a.name,
      kind: (a.kind as WorkflowRun["artifacts"][number]["kind"]) ?? "file",
      preview: a.preview ?? undefined,
      producedBy: a.producedByName ?? "",
      at: a.createdAt ?? new Date().toISOString(),
    }));
    return run;
  } catch (e) {
    if (e instanceof Error && e.message.includes("API 404")) return null;
    throw e;
  }
}

/** Cursor/seq-filtered log list envelope (the endpoint supports afterSeq). */
export async function fetchRunLogs(runId: string): Promise<LogEntry[]> {
  const page = await getJSON<Page<LogEntry>>(`/api/runs/${runId}/logs`);
  return page.items;
}

/** Shapes returned by /api/runs/{id}/browser (close to the UI BrowserSession). */
interface ApiBrowserSession {
  id: string;
  runId: string;
  agentName: string;
  status: BrowserSession["status"];
  currentUrl: string;
  pageTitle: string;
  viewport: { width: number; height: number };
  startedAt: string;
  pagesVisited: number;
  actionsCount: number;
  shots: { id: string; ts: string; url: string; title: string; label: string }[];
  actions: BrowserSession["actions"];
  console: BrowserSession["console"];
}

// A few gradient tones so the (screenshot-less) filmstrip thumbnails look varied.
const SHOT_TONES = [
  "from-indigo-500/20 to-violet-500/10",
  "from-sky-500/20 to-cyan-500/10",
  "from-emerald-500/20 to-teal-500/10",
  "from-amber-500/20 to-orange-500/10",
];

/** Fetch a run's browser session (or null if it has none). */
export async function fetchBrowserSession(runId: string): Promise<BrowserSession | null> {
  try {
    const s = await getJSON<ApiBrowserSession>(`/api/runs/${runId}/browser`);
    return {
      id: s.id,
      runId: s.runId,
      agentName: s.agentName,
      status: s.status,
      currentUrl: s.currentUrl,
      pageTitle: s.pageTitle,
      viewport: { w: s.viewport.width, h: s.viewport.height },
      startedAt: s.startedAt,
      pagesVisited: s.pagesVisited,
      actionsCount: s.actionsCount,
      shots: (s.shots ?? []).map((sh, i) => ({ ...sh, tone: SHOT_TONES[i % SHOT_TONES.length] ?? SHOT_TONES[0]! })),
      actions: s.actions ?? [],
      console: s.console ?? [],
    };
  } catch (e) {
    if (e instanceof Error && e.message.includes("API 404")) return null;
    throw e;
  }
}

/**
 * Incrementally tail a run's logs: fetch only lines with seq > sinceSeq. Because
 * the log is append-only with a monotonic per-run seq, this is a correct live
 * tail — and it works across the API/worker process split because it reads
 * Postgres (shared truth), not the API's in-memory event bus (which the separate
 * worker process never publishes to). Pass -1 to get everything.
 */
export async function fetchRunLogsAfter(runId: string, sinceSeq: number): Promise<LogEntry[]> {
  const qs = sinceSeq >= 0 ? `?afterSeq=${sinceSeq}` : "";
  const page = await getJSON<Page<LogEntry>>(`/api/runs/${runId}/logs${qs}`);
  return page.items;
}

/** Launch a run for a workflow slug with optional task input; returns the run id. */
export async function launchRun(slug: string, input?: Record<string, unknown>): Promise<string> {
  const run = await getJSON<{ id: string }>(`/api/workflows/${slug}/runs`, {
    method: "POST",
    body: JSON.stringify(input ? { input } : {}),
  });
  return run.id;
}

/* ---------------------- additional live-data fetchers --------------------- */

import type { Workflow, ActivityItem, AppNotification } from "./types";

/** Dashboard KPI stats. The API shape matches the UI's dashboardStats 1:1. */
export interface DashboardStats {
  activeRuns: number;
  runsToday: number;
  successRate: number;
  spendTodayUsd: number;
  spendBudgetUsd: number;
  computeHours: number;
  tokensToday: number;
  runVolume: number[];
  spendSeries: number[];
}

export async function fetchDashboard(): Promise<DashboardStats> {
  return getJSON<DashboardStats>("/api/dashboard");
}

/** Workflows list (API WorkflowSummary ≈ UI Workflow). */
export async function fetchWorkflows(): Promise<Workflow[]> {
  const page = await getJSON<Page<Workflow>>("/api/workflows");
  return page.items;
}

export async function fetchWorkflow(slug: string): Promise<Workflow | null> {
  try {
    return await getJSON<Workflow>(`/api/workflows/${slug}`);
  } catch (e) {
    if (e instanceof Error && e.message.includes("API 404")) return null;
    throw e;
  }
}

/** Runs for one workflow (newest first), normalized. */
export async function fetchWorkflowRuns(slug: string): Promise<WorkflowRun[]> {
  const page = await getJSON<Page<ApiRun>>(`/api/workflows/${slug}/runs`);
  return page.items.map(toWorkflowRun);
}

/** Agent definitions (library). */
export interface AgentDefinition {
  id: string;
  name: string;
  role: string;
  model: string;
  description: string;
  tools: string[];
}

export async function fetchAgents(): Promise<AgentDefinition[]> {
  const page = await getJSON<Page<AgentDefinition>>("/api/agents");
  return page.items;
}

/** Activity feed. */
export async function fetchActivity(): Promise<ActivityItem[]> {
  const page = await getJSON<Page<ActivityItem>>("/api/activity");
  return page.items;
}

/** Notifications: API uses readAt/createdAt; UI wants read/at. */
interface ApiNotification {
  id: string;
  type: AppNotification["type"];
  title: string;
  body: string;
  createdAt: string;
  readAt: string | null;
  workflowSlug?: string | null;
  runNumber?: number | null;
}

export async function fetchNotifications(): Promise<AppNotification[]> {
  const page = await getJSON<Page<ApiNotification>>("/api/notifications");
  return page.items.map((n) => ({
    id: n.id,
    type: n.type,
    title: n.title,
    body: n.body,
    at: n.createdAt,
    read: n.readAt != null,
    workflowSlug: n.workflowSlug ?? undefined,
    runNumber: n.runNumber ?? undefined,
  }));
}

export async function markNotificationRead(id: string): Promise<void> {
  await fetch(`/api/notifications/${id}/read`, { method: "POST" });
}
