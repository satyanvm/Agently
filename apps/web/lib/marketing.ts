/**
 * Client for the signal → approval loop: approvals (the Inbox), competitors,
 * signals and brand context. Shapes mirror apps/api/internal/domain/marketing.go
 * and services/approval_service.go; nothing here is synthesized.
 */
import { getJSON } from "./api";

export type ApprovalStatus = "pending" | "approved" | "rejected" | "timed_out" | "canceled";

export interface SignalRef {
  id: string;
  source: string;
  title: string;
  url: string;
}

export interface Signal extends SignalRef {
  summary: string;
  competitorId: string | null;
  detail: Record<string, unknown>;
  runId: string | null;
  detectedAt: string;
}

export interface NotifyTarget {
  target: string;
  ok: boolean;
  detail: string;
}

export interface Approval {
  id: string;
  runId: string;
  nodeKey: string;
  iteration: number;
  title: string;
  channel: string;
  draft: string;
  finalContent: string | null;
  status: ApprovalStatus;
  assignee: string;
  notify: { url?: string; targets?: NotifyTarget[] };
  signalIds: string[];
  decidedBy: string | null;
  decisionNote: string | null;
  decidedAt: string | null;
  signaledAt: string | null;
  expiresAt: string | null;
  createdAt: string;
}

export interface ApprovalListItem extends Approval {
  workflowName: string;
  workflowSlug: string;
  signal: SignalRef | null;
  signalCount: number;
}

export interface ApprovalOutcome {
  id: string;
  nodeKey: string;
  nodeType: string;
  status: "succeeded" | "failed" | "skipped";
  summary: string;
  url: string;
  occurredAt: string;
}

export interface ApprovalRun {
  id: string;
  number: number;
  status: string;
  workflowName: string;
  workflowSlug: string;
  costUsd: number;
  langfuseTraceId: string | null;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface HistoryEvent {
  at: string;
  kind: "signal" | "run_started" | "draft" | "decision" | "outcome" | "run_finished";
  title: string;
  detail?: string;
  url?: string;
  status?: string;
  ref?: string;
}

export interface ApprovalHistory extends Approval {
  signals: Signal[];
  run: ApprovalRun | null;
  outcomes: ApprovalOutcome[];
  edited: boolean;
  published: ApprovalOutcome[];
  timeline: HistoryEvent[];
}

export function fetchApprovals(status?: ApprovalStatus): Promise<ApprovalListItem[]> {
  const q = status ? `?status=${status}&limit=100` : "?limit=100";
  return getJSON<ApprovalListItem[]>(`/api/approvals${q}`);
}

export function fetchApprovalHistory(id: string): Promise<ApprovalHistory> {
  return getJSON<ApprovalHistory>(`/api/approvals/${encodeURIComponent(id)}/history`);
}

export function decideApproval(
  id: string,
  body: { decision: "approved" | "rejected"; decidedBy: string; content?: string; note?: string },
): Promise<unknown> {
  return getJSON(`/api/approvals/${encodeURIComponent(id)}/decision`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/* ------------------------------ competitors ------------------------------ */

export const PAGE_KINDS = ["pricing", "changelog", "blog", "careers", "other"] as const;
export type PageKind = (typeof PAGE_KINDS)[number];

export interface CompetitorPage {
  kind: PageKind;
  url: string;
  render: "http" | "browser";
}

export interface Competitor {
  id: string;
  name: string;
  website: string;
  pages: CompetitorPage[];
  notes: string;
  createdAt: string;
  updatedAt: string;
  archivedAt: string | null;
}

export interface SnapshotSummary {
  id: string;
  pageKind: string;
  url: string;
  runId: string | null;
  fetchedVia: string;
  title: string;
  chars: number;
  fetchedAt: string;
  lastSeenAt: string;
}

export type CompetitorInput = Pick<Competitor, "name" | "website" | "pages" | "notes">;

export function fetchCompetitors(): Promise<Competitor[]> {
  return getJSON<Competitor[]>("/api/competitors");
}

export function createCompetitor(input: CompetitorInput): Promise<Competitor> {
  return getJSON<Competitor>("/api/competitors", { method: "POST", body: JSON.stringify(input) });
}

export function updateCompetitor(id: string, input: Partial<CompetitorInput>): Promise<Competitor> {
  return getJSON<Competitor>(`/api/competitors/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export async function archiveCompetitor(id: string): Promise<void> {
  const res = await fetch(`/api/competitors/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!res.ok) throw new Error(`API ${res.status}: ${(await res.text()).slice(0, 200)}`);
}

export function fetchSnapshots(competitorId: string): Promise<SnapshotSummary[]> {
  return getJSON<SnapshotSummary[]>(`/api/competitors/${encodeURIComponent(competitorId)}/snapshots?limit=100`);
}

export function fetchSignals(params: { competitorId?: string; limit?: number } = {}): Promise<Signal[]> {
  const q = new URLSearchParams();
  if (params.competitorId) q.set("competitorId", params.competitorId);
  q.set("limit", String(params.limit ?? 25));
  return getJSON<Signal[]>(`/api/signals?${q}`);
}

/* ------------------------------ brand context ------------------------------ */

export interface BrandContext {
  voice: string;
  productFacts: string;
  positioning: string;
  avoidClaims: string;
  updatedAt: string;
  updatedBy: string;
}

export function fetchBrand(): Promise<BrandContext> {
  return getJSON<BrandContext>("/api/brand");
}

export function saveBrand(input: Omit<BrandContext, "updatedAt">): Promise<BrandContext> {
  return getJSON<BrandContext>("/api/brand", { method: "PUT", body: JSON.stringify(input) });
}

/* -------------------------- remembered approver name -------------------------- */

const NAME_KEY = "agently.approverName";

/** There are no user accounts yet: the approver types their name once and the
 * browser remembers it. Storage can be unavailable (private mode) — then it's
 * simply not remembered. */
export function rememberedName(): string {
  try {
    return localStorage.getItem(NAME_KEY) ?? "";
  } catch {
    return "";
  }
}

export function rememberName(name: string): void {
  try {
    localStorage.setItem(NAME_KEY, name);
  } catch {
    /* not remembered */
  }
}

/** API errors arrive as "API 409 on /api/…: {json}"; show the message part. */
export function apiErrorMessage(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e);
  const json = raw.indexOf("{");
  if (json >= 0) {
    try {
      const parsed = JSON.parse(raw.slice(json)) as {
        error?: { message?: string; details?: { path: string; message: string }[] };
      };
      const details = parsed.error?.details?.map((d) => `${d.path}: ${d.message}`).join("; ");
      if (parsed.error?.message) return details ? `${parsed.error.message} — ${details}` : parsed.error.message;
    } catch {
      /* fall through */
    }
  }
  return raw;
}

/* ------------------------------- playbooks ------------------------------- */

export interface PlaybookInput {
  key: string;
  label: string;
  required: boolean;
  type?: "list";
  help?: string;
  example?: string;
}

export interface PlaybookSummary {
  id: string;
  name: string;
  summary: string;
  trigger: string;
  schedule: string;
  publishesTo: string[];
  inputs: PlaybookInput[];
  steps: number;
  approvals: number;
  logins: { type: string; label: string; connected: boolean }[];
}

export function fetchPlaybooks(): Promise<PlaybookSummary[]> {
  return getJSON<PlaybookSummary[]>("/api/playbooks");
}

export function installPlaybook(id: string, body: { name?: string; inputs: Record<string, string> }): Promise<{ slug: string }> {
  return getJSON<{ slug: string }>(`/api/playbooks/${encodeURIComponent(id)}/install`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}
