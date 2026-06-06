import type { SupabaseClient } from "../db/index.js";
import type { LogEntry, LogLevel } from "../types.js";

/**
 * Append-only run logs. The worker writes; the frontend reads/streams via
 * Supabase Realtime on the run_logs table. `seq` gives stable ordering even if
 * timestamps collide. Bodies wired in Phase 4/5.
 */

export async function appendLog(
  _client: SupabaseClient,
  _runId: string,
  _level: LogLevel,
  _message: string,
): Promise<void> {
  throw new Error("appendLog: implemented in Phase 4");
}

export async function listLogs(
  _client: SupabaseClient,
  _userId: string,
  _runId: string,
): Promise<LogEntry[]> {
  throw new Error("listLogs: implemented in Phase 5");
}
