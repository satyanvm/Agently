/**
 * Builder debugging + failure-handling API (apps/api node_data.go, failures.go).
 *
 *   GET  /api/workflows/{slug}/node-data   each node's latest output / error
 *   POST /api/workflows/{slug}/runs        with exec {targetNode, usePins}
 *   GET  /api/runs/{id}                    polled until the run finishes
 *   PUT  /api/workflows/{slug}/settings    error workflow + alert targets
 */
import { getJSON, launchRun } from "./api";

/** Reserved node config keys the builder owns (lifted off by the reasoner). */
export const PINNED_KEY = "__pinned";
export const SETTINGS_KEY = "__settings";

export interface NodeSettings {
  retries?: number;
  retryWaitSec?: number;
  timeoutSec?: number;
  continueOnFail?: boolean;
}

export interface NodeSample {
  nodeKey: string;
  status: "succeeded" | "failed" | string;
  output?: Record<string, unknown>;
  error?: string;
  attempts?: number;
  runId: string;
  runNumber: number;
  finishedAt?: string | null;
}

export type NodeSamples = Record<string, NodeSample>;

export function fetchNodeData(slug: string): Promise<NodeSamples> {
  return getJSON<NodeSamples>(`/api/workflows/${slug}/node-data`);
}

export type RunState = "queued" | "running" | "succeeded" | "failed" | "canceled";

interface RunPoll {
  id: string;
  number: number;
  status: RunState;
  error?: string | null;
  currentStep: string;
}

const TERMINAL: RunState[] = ["succeeded", "failed", "canceled"];

/**
 * Launch a builder test run and poll it to the end. `onUpdate` sees every
 * status change. Resolves with the finished run.
 */
export async function runAndWait(
  slug: string,
  exec: { targetNode?: string; usePins?: boolean },
  onUpdate: (run: RunPoll) => void,
  signal?: { cancelled: boolean },
): Promise<RunPoll> {
  const id = await launchRun(slug, undefined, exec);
  for (;;) {
    const run = await getJSON<RunPoll>(`/api/runs/${id}`);
    onUpdate(run);
    if (TERMINAL.includes(run.status) || signal?.cancelled) return run;
    await new Promise((r) => setTimeout(r, 1200));
  }
}

export interface WorkflowSettings {
  errorWorkflow?: string;
  alertEmail?: string;
  alertSlackWebhook?: string;
}

export async function fetchWorkflowSettings(slug: string): Promise<WorkflowSettings> {
  const wf = await getJSON<{ settings?: WorkflowSettings }>(`/api/workflows/${slug}`);
  return wf.settings ?? {};
}

export async function saveWorkflowSettings(slug: string, s: WorkflowSettings): Promise<void> {
  await getJSON(`/api/workflows/${slug}/settings`, { method: "PUT", body: JSON.stringify(s) });
}

/** Template reference for a value at `path` inside a dependency's output. */
export function outputRef(nodeKey: string, path: (string | number)[]): string {
  return `{{${["outputs", nodeKey, ...path].join(".")}}}`;
}

/** Drag payload type for output fields (drop targets also accept text/plain). */
export const REF_MIME = "application/x-agently-ref";
