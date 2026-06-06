import "dotenv/config";
import {
  getServiceClient,
  getRunner,
  claimNextRun,
  type RunnerContext,
} from "@agently/core";

/**
 * The worker: a single long-running Node process. It polls Postgres for queued
 * runs, claims one atomically (FOR UPDATE SKIP LOCKED), looks up the runner for
 * that agent's type, and executes it — streaming logs into run_logs as it goes.
 *
 * It shares NO runtime state with the web app. They communicate only through
 * the database, which is exactly what lets them be deployed independently
 * (same box, or web→Vercel + worker→Railway).
 *
 * Phase 0: the poll loop and graceful shutdown are real; claim/execute are
 * wired to the database in Phases 3–4. Until then the loop idles cleanly.
 */

const POLL_INTERVAL_MS = Number(process.env.WORKER_POLL_INTERVAL_MS ?? 2000);

let running = true;

async function tick(): Promise<void> {
  const client = getServiceClient();

  const run = await claimNextRun(client); // throws until Phase 3 — caught below
  if (!run) return; // nothing queued

  console.log(`[worker] claimed run ${run.id} (agent ${run.agentId})`);

  const ctx: RunnerContext = {
    agent: null as never, // loaded in Phase 4
    run,
    log: async (level, message) => console.log(`[run ${run.id}] ${level}: ${message}`),
    isCanceled: async () => false,
  };

  const runner = getRunner(ctx.agent.type);
  await runner.run(ctx);
}

async function loop(): Promise<void> {
  console.log(`[worker] started, polling every ${POLL_INTERVAL_MS}ms`);
  while (running) {
    try {
      await tick();
    } catch (err) {
      // Phase 0: claimNextRun is a deliberate stub. Log at debug and keep idling
      // so the process stays healthy until the DB wiring lands.
      if (process.env.WORKER_VERBOSE) console.error("[worker] tick error:", err);
    }
    await sleep(POLL_INTERVAL_MS);
  }
  console.log("[worker] stopped");
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function shutdown(signal: string): void {
  console.log(`[worker] received ${signal}, shutting down gracefully`);
  running = false;
}

process.on("SIGINT", () => shutdown("SIGINT"));
process.on("SIGTERM", () => shutdown("SIGTERM"));

loop().catch((err) => {
  console.error("[worker] fatal:", err);
  process.exit(1);
});
