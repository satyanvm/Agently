# Agently — Demo Runbook

> **⚠️ HISTORICAL (native engine, retired 2026-07-21).** This demo exercises the
> native Go worker, which is archived under `archive/worker` — every run now
> executes on the Temporal + LangGraph reasoner, where crash recovery is
> event-history replay instead of lease/reaper requeue. Kept as documentation of
> the Postgres-only durability design. To run it, first resurrect the worker
> (see `archive/worker/ARCHIVED.md`).

The end-to-end demo that proves the product's core promise: **durable autonomous
execution**. A run is created, executed by a worker, streamed live to the UI, and —
the money shot — **survives a worker crash and resumes from its checkpoint**.

Everything runs locally with no API key (a mock LLM stands in). Drop a real
`ANTHROPIC_API_KEY` in the worker's env to see real Claude output instead.

---

## 0. One-time setup

```bash
docker compose up -d        # Postgres on :5433, schema auto-applied
```

Build the two Go binaries (fast startup for timing-sensitive steps):

```bash
cd apps/api    && go build -o /tmp/agently-api ./cmd/server && cd ../..
cd apps/worker && go build -o /tmp/agently-worker ./cmd/worker && cd ../..
```

Set the connection string once per shell:

```bash
export DATABASE_URL="postgres://agently:agently@localhost:5433/agently"
```

---

## 1. Start the three processes (three terminals)

```bash
# Terminal 1 — API (control plane). Postgres-backed.
DATABASE_URL="$DATABASE_URL" /tmp/agently-api

# Terminal 2 — Worker (data plane). Mock LLM (omit ANTHROPIC_API_KEY).
env -u ANTHROPIC_API_KEY DATABASE_URL="$DATABASE_URL" WORKER_ID=worker-A /tmp/agently-worker

# Terminal 3 — Frontend.
pnpm --filter @agently/web dev      # http://localhost:3000
```

Open **http://localhost:3000/runs** — the runs page polls the live API every 2s
(note the "Live · N runs" badge).

---

## 2. The happy path — a real run, start to finish

Launch a run (this is exactly what the UI's "New run" button calls):

```bash
curl -s -X POST localhost:8080/api/workflows/competitive-intelligence-sweep/runs -d '{}'
```

Watch on the **/runs** page (or via API): the run goes
**queued → running** (worker claims it) **→ steps tick 1→2→3→4, cost climbs → succeeded.**

What just happened, narrated:
- The **API** only *enqueued* the run (status `queued`) — the control plane never executes.
- The **worker** claimed it via `claim_next_run()` (FOR UPDATE SKIP LOCKED), ran the native
  agent loop (Plan → Research → Analyze → Synthesize), and wrote a reasoning-trace log +
  token/cost accounting + a `result.md` artifact — all to Postgres as it went.

---

## 3. The money shot — crash recovery (close-your-laptop)

This is the demo that matters. Two workers, kill one mid-run.

```bash
# Reset a run to queued, give it 10 steps so there's time to crash it.
psql "$DATABASE_URL" -c "insert into runs (id, workspace_id, workflow_id, number, status, trigger, triggered_by, region, steps_total, queued_at) select 'run_demo', workspace_id, id, 9999, 'queued', 'manual', '{\"name\":\"Demo\",\"initials\":\"DE\"}', 'us-east-1', 10, now() from workflows limit 1 on conflict (id) do update set status='queued', claimed_by=null, heartbeat_at=null, started_at=null, finished_at=null, steps_done=0;"
```

```bash
# Worker A, NO reaper (so only the crash matters), let it do a few steps:
env -u ANTHROPIC_API_KEY DATABASE_URL="$DATABASE_URL" WORKER_ID=worker-A WORKER_REAPER=0 /tmp/agently-worker &
sleep 4
# >>> KILL IT HARD (simulate laptop dying):
kill -9 %1     # or: pkill -9 -f agently-worker
```

The run is now frozen mid-flight: `running`, owned by the dead `worker-A`, at e.g. step 3.
In a memory-only system it's **lost forever**. Here:

```bash
# Worker B, WITH reaper:
env -u ANTHROPIC_API_KEY DATABASE_URL="$DATABASE_URL" WORKER_ID=worker-B /tmp/agently-worker
```

Watch worker B's log:
```
requeued stalled runs count=1          ← reaper noticed A's dead lease
claimed run runId=run_demo workerId=worker-B
Resuming from step N of 10 after recovery
run finished status=succeeded
```

**The proof of correctness:** count the model-output log lines —
```bash
psql "$DATABASE_URL" -c "select count(*) from run_logs where run_id='run_demo' and reasoning=true;"
```
It equals the number of steps (10), **not** more. Completed steps were **not** re-run;
no LLM call was duplicated. Only the one in-flight step replayed. Exactly-once execution
across a hard crash.

---

## 4. The one-paragraph version (for the interview)

> "Agently keeps a run's state in Postgres, not in the worker. A worker *leases* a run and
> *heartbeats* to hold it. If the worker dies, the heartbeat stops, a reaper requeues the
> run, and another worker resumes it **from its last checkpoint** — completed steps are
> skipped, so no work is duplicated. That's the whole 'close your laptop and come back'
> promise, and it's built on `FOR UPDATE SKIP LOCKED` plus a `steps_done` checkpoint — no
> Kafka, no Temporal, just Postgres used well."

---

## Cleanup

```bash
psql "$DATABASE_URL" -c "delete from runs where id='run_demo';"
docker compose down          # keep data;  add -v to wipe it
```
