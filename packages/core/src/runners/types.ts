import type { Agent, LogLevel, Run } from "../types.js";

/**
 * A Runner knows how to execute one kind of agent. The worker is agnostic:
 * it looks up the runner for an agent's `type` and calls `run()`. Adding a new
 * agent type = implement this interface + register it. No worker changes.
 */

export interface RunnerContext {
  agent: Agent;
  run: Run;
  /** Emit a log line. Persisted to run_logs and streamed live to the frontend. */
  log: (level: LogLevel, message: string) => Promise<void>;
  /** Resolves true if the run was canceled mid-flight; runners should check
   *  this between steps and stop cooperatively. */
  isCanceled: () => Promise<boolean>;
}

export interface RunnerResult {
  /** Structured result stored on the run row. */
  result?: Record<string, unknown>;
}

export interface Runner {
  readonly type: Agent["type"];
  run(ctx: RunnerContext): Promise<RunnerResult>;
}
