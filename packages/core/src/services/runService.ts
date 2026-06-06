import type { SupabaseClient } from "../db/index.js";
import type { Run } from "../types.js";

/**
 * Business logic for runs: enqueue, fetch, cancel, and lifecycle transitions
 * the worker uses to mark a run started/finished. HTTP-agnostic (see
 * agentService for the rationale). Bodies wired in Phase 3.
 */

export async function enqueueRun(
  _client: SupabaseClient,
  _userId: string,
  _agentId: string,
  _input?: Record<string, unknown>,
): Promise<Run> {
  throw new Error("enqueueRun: implemented in Phase 3");
}

export async function getRun(
  _client: SupabaseClient,
  _userId: string,
  _runId: string,
): Promise<Run | null> {
  throw new Error("getRun: implemented in Phase 3");
}

export async function listRunsForAgent(
  _client: SupabaseClient,
  _userId: string,
  _agentId: string,
): Promise<Run[]> {
  throw new Error("listRunsForAgent: implemented in Phase 3");
}

/** Worker-side: mark terminal status + result/error. */
export async function finishRun(
  _client: SupabaseClient,
  _runId: string,
  _outcome: { status: "succeeded" | "failed"; result?: Record<string, unknown>; error?: string },
): Promise<void> {
  throw new Error("finishRun: implemented in Phase 4");
}
