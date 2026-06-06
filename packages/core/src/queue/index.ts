import type { SupabaseClient } from "../db/index.js";
import type { Run } from "../types.js";

/**
 * Postgres-as-queue. No Redis, no external broker.
 *
 * A worker claims the next queued run atomically. The real implementation runs
 * inside a SQL function using:
 *
 *   SELECT id FROM runs WHERE status = 'queued'
 *   ORDER BY queued_at
 *   FOR UPDATE SKIP LOCKED LIMIT 1;
 *   -- then UPDATE that row to status='running', started_at=now()
 *
 * SKIP LOCKED lets multiple workers run safely without ever handing the same
 * row to two of them. Implemented as the `claim_next_run` Postgres function in
 * packages/db/migrations and called via RPC here (Phase 3).
 */
export async function claimNextRun(
  _client: SupabaseClient,
): Promise<Run | null> {
  throw new Error("claimNextRun: implemented in Phase 3");
}
