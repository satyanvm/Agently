# architecture.md — what you should understand

> A living knowledge log for the person *building* Agently, not for users.
> README.md = the design (the "what" and "why"). This file = the "how it actually
> works right now," the mental models, and a reading path. Updated as we build.
> If you only half-read the code, read it in the order at the bottom.

---

## 0. The one sentence to internalize

The product's whole value is **durability** — "close your laptop, the run survives,
come back and see everything." Every hard/impressive part of this codebase is, or will
be, in service of that one promise. When explaining this project in an interview, lead
with durability, not with the UI.

---

## 1. Where the code actually stands today (be honest about this)

| Part | Path | State | Reality |
|---|---|---|---|
| Frontend | `apps/web` | **Fully on live API (chunk 16 ✓)** | Next.js 15. Every page consumes the live Go API via `lib/api.ts`. `lib/mock-data.ts` deleted. Dashboard KPIs computed from real runs. |
| API | `apps/api` | **Postgres-backed, real data only** | Real Go HTTP server + SSE. Persists to Postgres (`DATABASE_URL`); in-memory fallback for tests. Minimal bootstrap seed (1 workspace, AI Digest workflow); no fake runs. |
| Contracts | `packages/contracts` | Built | Shared TS types the frontend consumes. |
| Migrations | `packages/db/migrations` | **Wired (0001,0003,0005,0006)** | Local Postgres via Docker. `0002_rls`/`0004_realtime` are Supabase-only, skipped locally. |
| Worker | `apps/worker` | **Full real engine (chunks 2–15 ✓)** | Separate Go module. Claims runs (lease+heartbeat, crash-safe), runs multi-agent **DAGs** in parallel, **fetches real content** (arXiv/HN/Reddit/web), drives **browser** sessions (sim + Browserbase CDP), and **emails the result** (SMTP). Three+ swappable seams: LLM, browser, notify, sources. |
| Reasoner | `apps/reasoner` | **Second execution plane (v1-6 ✓)** | Separate Python service. LangGraph graphs run as **Temporal activities** (durability via Temporal event history instead of the Go lease/heartbeat), traced in **Langfuse**, browsing via **Browserbase**, writing the SAME Postgres rows the UI renders. Dispatches `engine='temporal'` runs. Now executes **user-composed graphs dynamically** (§20), not just the static plan→browse→synthesize→deliver flow. |

**Where it stands now:** the AI Digest workflow runs for real end-to-end — fetches live
arXiv papers + HN news, synthesizes a digest, emails it (SMTP), all durably resumable. With
a real Anthropic key the synthesis is prose; with a Browserbase key the browser is live.

---

## 2. How a request flows *today* (trace this in the code)

```
Browser (apps/web, a React Server/Client component)
   │  fetch("/api/runs/123")
   ▼
Next.js dev server  ── proxy /api/* ──►  Go API  (apps/web/next.config.ts rewrites)
   │
   ▼
Go API (apps/api)
   cmd/server/main.go         → builds Platform, starts http.Server on :8080
   handler/router.go          → chi routes /api/*  to handlers
   handler/runs.go            → thin HTTP handler, parses + validates
   services/run_service.go    → use-case logic (List/Get/Launch/cancel)
   platform/repositories.go   → repository *interfaces*
   platform/memory.go         → in-memory implementation (seeded by platform/seed.go)
   │
   ▼  (for live updates)
platform/bus.go  → in-process event bus w/ ring buffer
handler/events.go → SSE stream; client reconnects via Last-Event-ID
```

Two things to notice, because they're the good architecture you already have:

1. **Storage-agnostic services.** Services depend on repository *interfaces*, not the
   in-memory store. Swapping in Postgres = writing a new implementation of those
   interfaces and changing one assembly line in `NewPlatform`. The services don't change.
   *(This is the seam that makes step "wire Postgres" tractable.)*
2. **The DB is meant to be the source of truth, but isn't yet.** Today the "source of
   truth" is a Go map in RAM. That's why nothing survives a restart — and why there's no
   durability story until Postgres is wired.

---

## 3. The mental models to hold

**Control plane vs data plane.** The API (`apps/api`) *manages* runs — it must stay up
no matter what an agent does. The worker (`apps/worker`, unbuilt) *executes* runs — it's
disposable and may die anytime. They talk only through the database. Today only the
control plane exists, half-built (in-memory).

**The database is the source of truth; the worker is cattle.** A run's progress lives in
Postgres rows + checkpoints, never only in the worker's memory. This is the single idea
that makes "close your laptop" work: kill any worker, the run resumes elsewhere.

**Postgres *is* the queue.** No Redis/Kafka. `claim_next_run()` (in `0003_queue.sql`)
uses `FOR UPDATE SKIP LOCKED` so many workers poll the same `runs` table without ever
handing one run to two workers. `reap_stalled_runs()` requeues runs whose worker stopped
heartbeating. (See README Glossary for lease/heartbeat.)

**Lease + heartbeat = crash recovery.** Lease = a time-limited claim (`is the run still
owned?`). Heartbeat = the worker periodically renewing it (`is the owner still alive?`).
Short lease + frequent heartbeat = fast crash detection *and* arbitrarily long runs.

**Native executor first.** The first agent runtime is a trivial loop
(`prompt → LLM → tool → repeat`) with no hidden state — so when a resume bug appears, it's
unambiguously *yours*, not a framework's. Frameworks (LangGraph/CrewAI) plug in later.

---

## 4. What "done" looks like for the next milestone

The milestone is **one real run that survives a kill**. Acceptance test, in plain words:

1. Start a run from the UI. It persists to Postgres (`status=queued`).
2. A worker claims it (`claim_next_run`), sets `status=running`, starts heartbeating.
3. The worker makes a real LLM call, appends real logs (streamed to the UI via SSE),
   writes a checkpoint, and produces a result artifact.
4. **`kill -9` the worker mid-run.** Its lease stops being renewed.
5. `reap_stalled_runs()` requeues it; another worker claims it and **resumes from the
   checkpoint** (does not restart from scratch, does not double-charge the LLM).
6. The run finishes; a notification fires; the UI shows the full timeline.

When you can screen-record steps 4–5, you have the thing that gets you the job.

---

## 5. Reading path (if you read ~40% of the code, read this 40%)

Read in this order — each builds on the last:

1. `apps/api/internal/domain/entities.go` + `enums.go` — the nouns of the system
   (Run, Workflow, Agent…). Everything else manipulates these.
2. `apps/api/internal/platform/repositories.go` — the storage *interfaces*. This is the
   seam between "logic" and "where data lives."
3. `apps/api/internal/services/run_service.go` — the run lifecycle. Note the write-path
   methods (`start/progress/finish`) that *"a real runner will call"* — that runner is
   the worker you're about to build.
4. `packages/db/migrations/0001_init.sql` then `0003_queue.sql` — the real schema and the
   queue function. Compare the SQL tables to the Go entities in step 1.
5. `apps/web/lib/mock-data.ts` — what the UI *expects* a real run to look like. Your
   backend's job is to produce this shape for real.

---

## 6. Open decisions (resolve before building, recorded here as we go)

- **Worker language** — Go (one language, simple ops, no framework ecosystem) vs Python
  (unlocks LangGraph/CrewAI later, but adds a second runtime). *Pending your call.*
- **Database host** — local Postgres (Docker) for dev vs Supabase cloud now. Migrations
  are Supabase-flavored (RLS) but plain Postgres runs them fine.

*(This section is the running log of choices and their reasons — the thing an interviewer
loves, because it shows you decided rather than cargo-culted.)*

---

## 7. Chunk 1 — wiring the API to Postgres (DONE)

**What we did and why it mattered:** the API's "database" was a Go map in RAM
(`platform/memory.go`) — restart and everything vanished. Durability is impossible on
top of that. We made Postgres the real source of truth, so data outlives the process.

**How to run it locally:**
```bash
docker compose up -d                  # Postgres on :5433, schema auto-applied
cd apps/api
DATABASE_URL="postgres://agently:agently@localhost:5433/agently" go run ./cmd/server
# no DATABASE_URL → in-memory mode (what the tests use)
```

**The acceptance test that proves it (re-runnable):** create a workflow via the API,
`pkill` the API process, start a fresh one, GET the workflow back — it's still there.
We verified this: a "Persistence Probe" workflow survived a full process restart.

**Files added (read these to understand the seam in practice):**
- `docker-compose.yml` — local Postgres; mounts only `0001_init` + `0003_queue` (the
  portable migrations). RLS/realtime are Supabase-only and skipped locally.
- `apps/api/internal/platform/postgres_conn.go` — connection pool + the type-conversion
  helpers (ISO-string ↔ timestamptz, jsonb marshal/unmarshal, nullable handling).
- `apps/api/internal/platform/postgres_repos.go` — the 13 repositories as SQL. **This is
  the payoff of the repository interface:** same method signatures as `memory.go`, SQL
  inside instead of map operations. Services/handlers were not touched.
- `apps/api/internal/platform/postgres_seed.go` — writes the demo dataset into an empty
  DB on first boot (idempotent: skips if a workspace already exists).
- `platform.go` / `main.go` — the flip: `DATABASE_URL` set → Postgres repos, else memory.

**Concepts worth internalizing from this chunk:**
- **The repository seam is why this was a clean swap.** Logic depends on *interfaces*; we
  added a second *implementation*. No service changed. This is the single most valuable
  pattern in the codebase — it's also how the worker will share storage with the API.
- **FK cycles need a two-pass insert.** Two relationships are circular:
  `workflows.current_version_id ↔ workflow_versions.workflow_id`, and
  `runs.browser_session_id ↔ browser_sessions.run_id`. You can't insert either side
  "first" with both FKs satisfied. The fix (in the seeder): insert the parent with the
  cyclic column NULL, insert the child, then UPDATE the parent to re-link. Worth
  remembering — it recurs anywhere two tables point at each other.
- **Two storage backends coexist on purpose.** Tests want zero-setup in-memory; dev/prod
  want durable Postgres. The env var selects. Keep it that way — fast tests matter.

**Known shortcut (recorded honestly, to revisit):** the repository methods don't take a
`ctx` or return errors on the read path (they mirror the in-memory signatures). On
Postgres, a failed query is logged and returns a zero value / not-found. Fine for MVP;
a later pass threads context + errors through for proper timeouts and error surfacing.

---

## 8. Chunk 2 — the worker skeleton: claim + lease + heartbeat (DONE)

**What we built:** `apps/worker`, a **separate Go module** that shares no code with the
API — only the database. This is the control-plane / data-plane split made real: the API
manages runs, the worker executes them, Postgres is the only thing between them. The work
itself is still *simulated* (sleeps + progress updates); chunk 3 swaps in real LLM calls.

**The structure (read these three files in order):**
- `internal/queue/queue.go` — the worker's view of the Postgres-as-queue: `Claim`,
  `Heartbeat`, `Finish`, `Progress`, `Reap`. Every write is scoped to `(id, claimed_by)`.
- `internal/runner/runner.go` — the execution loop: claim → spawn heartbeat goroutine →
  do work → finish. The heartbeat goroutine and a per-run child context are the core.
- `internal/runner/reaper.go` — requeues runs whose lease lapsed (the other half of
  crash recovery).

**Migration `0005_worker_claim.sql` — why it was needed:** 0003's `claim_next_run()`
recorded *no owner*. Without an owner, a merely-slow (not dead) worker and a new worker
that took over a requeued run could both think they own it — **split-brain**. 0005 adds a
`claimed_by` column, makes claim stamp the owner + first heartbeat, and scopes every
heartbeat/finish to `(id, claimed_by)`. A worker can only touch a run it still owns.

**How to run it:**
```bash
docker compose up -d
go build -o /tmp/agently-worker ./apps/worker/cmd/worker
DATABASE_URL="postgres://agently:agently@localhost:5433/agently" \
  WORKER_ID=worker-A /tmp/agently-worker
# env: WORKER_ID (default hostname-pid), WORKER_REAPER=0 to disable the in-process reaper
```

**The acceptance test we ran (and it passed):** queued a run, worker-A claimed it and did
step 1, we `kill -9`'d worker-A mid-run. The reaper (in worker-B) detected the dead lease,
requeued the run **at step 1, not reset to 0**, worker-B claimed it and drove it to
`succeeded`. A hard crash with no cleanup, and the run still finished — automatically.

**Concepts to internalize from this chunk:**
- **Lease = ownership + expiry; heartbeat = the renewal that proves liveness.** A killed
  worker stops heartbeating → lease goes stale → reaper requeues → another worker resumes.
  This is the literal mechanism behind "close your laptop." (See the README Glossary.)
- **Lease length lives in SQL** (`reap_stalled_runs` `max_silence`, currently 15s); the
  **heartbeat interval lives in the worker** (3s). Heartbeat ≪ lease is the safety margin
  that lets a worker miss a beat (GC pause, blip) without losing its run. We set ~5×.
- **Scope every mutation to `(id, claimed_by)`.** This single rule is what makes the system
  safe under concurrency + handoff. If `RowsAffected() == 0`, the lease was lost — the
  worker must stop. The runner cancels a child context to make "stop" actually interrupt.
- **Only owned runs are reapable** (`claimed_by is not null`). A subtle bug we hit and
  fixed: the seed marks some runs `running` purely for the UI, with no owner. The reaper
  must not touch those — they're not stalled workers, they're display data. Lesson: the
  queue's authority extends only to runs a worker actually claimed.
- **`go run` compiles first** (~3–5s); for timing-sensitive tests, `go build` a binary and
  run that. Cost us one confusing test run before we switched.

**Known shortcuts (revisit):** (1) one run at a time per worker — no concurrency yet;
(2) the reaper runs in-process in any worker (fine because requeue is idempotent +
SKIP-LOCKED-safe), but production wants it as an independent/leader-elected singleton;
(3) lease timings are demo-fast (15s/3s), not production values (which'd be minutes).

---

## 9. Chunks 3 & 4 — native agent runtime + crash-resume (DONE)

**Chunk 3 — real model work.** The worker's simulated `doWork()` is gone; runs now
execute a real agent loop and persist everything.

- `internal/llm/llm.go` — model layer behind a `Provider` interface. Real **Anthropic**
  when `ANTHROPIC_API_KEY` is set; a deterministic **mock** otherwise (so the system runs
  with no key/network). Same interface-and-swap seam as storage. Returns token counts;
  `EstimateCostUSD` turns them into the product's cost number.
- `internal/queue/store.go` — the worker's WRITE path: `AppendLog` (append-only, monotonic
  `seq` per run), `AddArtifact`, `AddUsage` (accumulates tokens+cost, scoped to owner).
  This is the observability half written from the data plane.
- `internal/agent/agent.go` — the **native runtime**: an explicit ordered plan
  (Plan→Research→Analyze→Synthesize), each step = a logged "Thinking" line + an LLM call
  logged as a reasoning trace (`reasoning=true`) + usage accounting; the final step emits a
  `result.md` artifact. Tiny and stateless beyond the DB — every step is persisted as it
  happens. This is the boundary where LangGraph/CrewAI will later plug in as alt executors.

A mock run produced: `status=succeeded, 4/4, 290 in / 451 out tokens, $0.0076`, a 10-line
reasoning trace, and a report artifact — exactly the shape the UI run-viewer expects.

**Chunk 4 — checkpointing + crash-resume.** The checkpoint is simply `runs.steps_done`:
the agent marks a step done (`Progress(i+1)`) only *after* its LLM call and usage are
persisted. On claim, the runner passes `startStep = steps_done` into the runtime, which
runs `steps[startStep:]` — completed steps are skipped. The reaper preserves `steps_done`
on requeue (it only nulls lease fields), so a resumed run continues from its checkpoint.

**The proof we ran:** worker-A completed 3 of 4 steps then `kill -9`. worker-B's reaper
requeued (steps_done=3 preserved), B claimed and logged *"Resuming from step 4 of 4"*, ran
only the last step, finished. **Total model outputs across both workers: exactly 4, not
8.** No completed step re-ran; no LLM call was duplicated. The one in-flight step
(Synthesize, started by A but not completed → steps_done still 3) correctly replayed on B.

**Concepts to internalize:**
- **A checkpoint is "durably recorded progress you can resume from."** Here it's one column
  (`steps_done`). The discipline that makes it correct: **persist the step's effects BEFORE
  advancing the checkpoint.** We log + bank usage, *then* `Progress(i+1)`. If we advanced
  first and crashed, we'd skip un-done work. Order is the whole game.
- **Resume semantics: completed = skip, in-flight = replay.** A step that didn't reach
  `steps_done++` is treated as not done and re-run from scratch. That means side effects
  within a step must tolerate replay (idempotency). With a pure LLM call that's fine; once
  steps perform external actions (send email, write a file), they'll need idempotency keys.
- **Append-only logs with a per-run `seq`** are why the trace survives a crash and resumes
  cleanly: B calls `NextLogSeq` and continues numbering from where A stopped — no collisions
  with the `unique(run_id, seq)` constraint, stable ordering in the UI.
- **The mock provider is a feature, not a crutch.** It lets the entire durability story be
  demoed and tested deterministically with no key, no network, no cost. Keep it.

**Note on the live env:** an `ANTHROPIC_API_KEY` is present but returns 401 (it's the IDE
session credential, not a usable public-API key). The Anthropic path is correct — it makes
a real call and logs the 401 gracefully — but happy-path demos use `env -u ANTHROPIC_API_KEY`
to force the mock. Drop a valid key in to see real Claude output.

---

## 10. Chunk 5 — frontend on the live API + the end-to-end loop (DONE)

**What we wired:** `apps/web/lib/api.ts` is a typed client for the Go API (reached via the
existing `/api/*` dev proxy). The **/runs** page (`app/(app)/runs/page.tsx`) now polls
`fetchRuns()` every 2s instead of importing the mock `runs` array — so a run created by the
worker appears here and a running run's progress updates live (with a "Live · N runs"
badge and an error banner if the API is down).

**Scope decision (honest):** only the Runs surface is on live data in this cut — it's the
demo's center of gravity. The other 9 pages still read `lix`b/mock-data.ts`; each is a small,
mechanical follow-up migration. Wiring all of them with full visual QA isn't something I can
verify headlessly, so I did the high-value path well rather than all paths blindly.

**One backend fix this forced (important):** `RunService.Launch` created runs as
`RunRunning` — a leftover from the pre-worker era when nothing executed them. For the worker
to claim work, runs must be born **`queued`**. Changed it; updated the one test that asserted
the old behavior. This is the control/data-plane split asserting itself in code: **the API
enqueues, the worker executes.** Never let the API mark a run running again.

**The full loop, verified end to end:** `POST /workflows/{slug}/runs` → run `queued` →
worker claims (`queued→running`) → native agent loop runs, streaming logs + cost → run
`succeeded`, artifact produced → all visible via the same `/api/runs` the UI polls. The
log trace even shows the seam cleanly: seq 0–1 are the API's bootstrap logs, seq 2+ are the
worker's native-runtime trace — two processes, one coherent timeline in Postgres.

**Gotchas worth remembering (cost real debugging time):**
- **Stale process on a port.** A `go run` server from chunk 1 was still holding :8080, so a
  freshly-built binary silently failed to bind and the old (buggy) behavior persisted. Lesson:
  when behavior doesn't match a rebuilt binary, check `lsof -nP -iTCP:8080 -sTCP:LISTEN` —
  the thing you're talking to may not be the thing you just built.
- **List endpoints return an envelope** (`{items, nextCursor}`), not a bare array. The client
  unwraps `.items`.
- **`steps_total` cosmetic mismatch:** the API estimates total steps as `agent_count*4` (e.g.
  28) for the progress bar, while the native runtime runs its own 4-step plan. Harmless for
  the demo (status + completion are correct), but worth reconciling when the workflow
  definition actually drives the plan.

**The demo script lives in `DEMO.md`** — happy path + the crash-recovery money shot + the
one-paragraph interview version. That file is the thing to screen-record.

---

## 11. Chunk 6 — run-detail live view + incremental log tailing (DONE)

**What we built:** the run-detail page (`app/(app)/runs/[id]/page.tsx`) — the product's
"center of gravity" — is now a **client component on live data**. It was a static
server-component pre-rendered from mock; now it fetches the run + tails its logs in real
time, so you watch an agent think (Plan → Research → Analyze → Synthesize → artifact) as
the worker writes each line. Stops polling once the run is terminal.

**The key technique — incremental log tailing by `seq`:** `lib/api.ts` gained
`fetchRunLogsAfter(runId, sinceSeq)`, which calls `/api/runs/{id}/logs?afterSeq=N` to fetch
only lines newer than the highest seq seen. The page keeps a `sinceSeq` high-water mark and
appends only new lines each tick. Verified: a mid-flight run streamed 12 lines across 12
ticks, each fetch returning just the new ones.

**Why polling-by-seq, not the SSE endpoint** (important architectural point): the API *has*
an SSE log stream (`streamLogs` in `handler/events.go`), but it emits a frame only on a
`RunLogAppendedEvent` published to the API's **in-process event bus**. The **worker is a
separate process** — it writes logs straight to Postgres and never touches that bus. So the
SSE stream would show *nothing* for worker-driven runs. Tailing `?afterSeq=` reads Postgres
(the shared truth), so it works across the process boundary. It's the same "resume from
Last-Event-ID" idea, but over the durable append-only log instead of a volatile bus. To make
the SSE path work cross-process later, the worker would publish to a shared bus (Postgres
LISTEN/NOTIFY — `0004_realtime.sql` already adds a `domain_events` NOTIFY trigger — or
Supabase realtime). Logged as a future improvement; the seq-tail is correct and simple now.

**Gotchas:**
- **`afterSeq=-1` is a 422.** The validator rejects negatives. The client guards: `sinceSeq
  >= 0 ? "?afterSeq="+n : ""` — so the first tick omits the param (gets all), later ticks
  pass the real max. Never send -1.
- **Mock runs finish in ~1s**, so to *see* streaming you must poll fast (≤0.5s) or the run is
  already done by the first tick. Real LLM latency makes this naturally visible.
- The run-detail route flipped from `●` (SSG) to `ƒ` (dynamic) in the build output — correct,
  it's now a live client page, not pre-rendered from mock.

**Still mock on this page (labeled):** the Agents tab reads `run.agents` (empty for native
single-agent runs until multi-agent lands) and the Browser tab is `null` until the browser
layer. Overview/Logs/Artifacts are fully live.

---

## 12. Chunk 7 — the multi-agent DAG engine (DONE)

**What we built:** the worker now executes a run's **agent graph** (the `run_agents` DAG)
instead of a fixed linear plan. It walks the graph in dependency order, runs each agent
against the LLM, threads upstream outputs into downstream prompts, and records hand-offs as
`agent_messages`. Chunk 7 runs ready agents **sequentially** (chunk 8 parallelizes them).

**Where the DAG comes from (key insight — it already existed):** the control plane's
`RunService.Launch` already materializes the workflow version's `GraphNode[]` into
`run_agents` rows at launch time, resolving each node's `dependsOn` (keys) into run-agent
ids. So **the per-run DAG lives in Postgres** before the worker ever sees it. The worker's
job was never to build the graph — only to *execute* it. This is why multi-agent runs are
durably resumable: the graph + each agent's status are rows, not memory.

**Files to read (in order):**
- `apps/worker/internal/queue/graph.go` — the worker's graph ops: `LoadAgents` (read the
  DAG), `SetAgentStatus`, `SetAgentResult` (metrics), `AddMessage` (hand-offs).
- `apps/worker/internal/agent/dag.go` — **the engine**. `RunDAG` walks the frontier;
  `readyAgents` computes "all deps done"; `runAgent` executes one node (status → messages →
  context-aware prompt → LLM → metrics → status). `buildAgentPrompt` threads upstream
  summaries into the prompt — the data-flow edge that makes it a pipeline, not N isolated calls.
- `apps/worker/internal/runner/runner.go` — chooses executor: `LoadAgents` returns a graph →
  `RunDAG`; empty → the single-agent linear plan. Both durable.

**The mental model — topological execution:**
```
frontier = agents whose every dependency is 'succeeded'
loop: run the frontier → mark done → recompute frontier → until all done
      (no ready agents but graph incomplete ⇒ something upstream failed ⇒ block the rest)
```
Verified on the 7-agent flagship (`competitive-intelligence-sweep`):
```
Conductor → {Scout·Pricing, Scout·Launches, Navigator} → Synthesizer → Composer → Auditor
   root         (3 children of root)                       3-way join    chain     chain
```
Conductor ran first; only after ALL three scouts succeeded did Synthesizer start (the
**fan-in join correctly waited**); then the downstream chain. 8 hand-off messages recorded
matching the exact DAG edges; per-agent tokens/cost/runtime on all 7; run totals aggregated.

**One backend fix this forced:** `Launch` was materializing entry-node agents as `running`
with a `started_at` (pre-worker leftover, same control/data-plane leak as run status). Now
**all agents start `idle`** and the worker drives their status. Updated in `run_service.go`.

**Concepts to internalize:**
- **The DAG state IS the checkpoint.** "Which agents are `succeeded`" is the resume point —
  no separate checkpoint structure needed (chunk 9 uses exactly this).
- **`depends_on` drives everything.** Ready = all deps done. Parallel-able = multiple ready
  at once (chunk 8 exploits this). Join = a node with many deps waits for all. Block = a node
  whose dep failed. One relation expresses the whole control flow.
- **Data-flow vs control-flow are separate.** `depends_on` is control-flow (ordering);
  passing `outputs[dep]` into the prompt is data-flow (what flows along the edge). Keep them 
  distinct — a dependency might gate timing without passing data, or vice versa.
- **Sequential first, parallel later — on purpose.** Getting topological *correctness* right
  with one-at-a-time execution means chunk 8's concurrency only has to add a worker pool, not
  also debug ordering.

---

## 13. Chunk 8 — parallel agent execution + message-passing (DONE)

**What changed:** the ready frontier now runs **concurrently** (bounded by
`maxConcurrentAgents = 4`) instead of one-at-a-time. On the flagship, the 3 scouts execute
in parallel; the fan-in (Synthesizer) still waits for all of them. Message-passing was
already recorded in chunk 7 — this chunk is about correct, race-free concurrency.

**The concurrency design (the part worth reading):** all of `dag.go` now shares state through
`dagState`, a mutex-guarded struct holding the **log seq counter** and the
**done/failed/outputs** maps. Why this matters:
- **Log seq is the sharp edge.** `run_logs` has `unique(run_id, seq)`. With multiple agents
  writing logs at once, two goroutines grabbing the same seq would violate that constraint and
  drop a log. `dagState.nextSeq()` hands out seqs under a lock → every concurrent writer gets a
  unique, monotonic seq. **Verified: 26 logs = 26 distinct seqs = contiguous range, no gaps.**
- **Frontier computed from a snapshot.** We copy done/failed out of the lock, compute
  `readyAgents`, then run them — so the (slow) LLM calls don't hold the lock. Completion is
  written back under the lock (`markDone`/`markFailed`).
- **The frontier runs in goroutines bounded by a semaphore** (`sem := make(chan struct{}, N)`).
  `wg.Wait()` is the **join barrier**: we don't recompute the next frontier until the whole
  current frontier finishes — which is exactly fan-in semantics.

**Verification (with the race detector):** built the worker with `go build -race` and ran the
flagship. Observed `running=3` simultaneously for the scout layer (max concurrent = 3, the
parallel width), the join correctly collapsing to 1 for Synthesizer, then the chain. **Zero
data races reported.** Run the race binary yourself: `go build -race -o /tmp/w ./cmd/worker`.

**Concepts to internalize:**
- **A unique index turns a race into a crash, which is good.** The `unique(run_id, seq)`
  constraint means a seq race can't silently corrupt — it'd error loudly. We avoided it with a
  lock, but the constraint is the safety net. Design your schema so races surface.
- **Hold locks around state, not around work.** The pattern — snapshot under lock, do the slow
  thing unlocked, write back under lock — is how you get concurrency without serializing the
  expensive part (the LLM calls). Holding the lock across `llm.Complete` would make "parallel"
  a lie.
- **`sync.WaitGroup` + semaphore = bounded fan-out with a join.** WaitGroup is the barrier
  (fan-in waits for all); the buffered channel is the concurrency cap (don't blow past rate
  limits / budget). Together they're the whole parallel-DAG primitive.
- **Test concurrency with `-race`, always.** A passing run without the race detector proves
  nothing about thread-safety. The detector is the only way to trust it.

---

## 14. Chunk 9 — DAG crash-resume + live Agents tab (DONE)

**Part 1 — multi-agent crash-resume.** The resume logic was already in `RunDAG` (agents with
`status='succeeded'` are seeded into `done` and skipped). This chunk *proved* it: ran the
flagship under worker-A (no reaper), `kill -9` with **3 of 7 agents succeeded, 1 running, 3
idle**; worker-B's reaper requeued, B logged *"Resuming agent graph: 3 of 7 agents already
done"*, finished the rest. **Total model outputs across both workers: exactly 7, not more** —
no completed agent re-ran. The one in-flight agent (running when A died, so never reached
`succeeded`) correctly replayed on B.

The deep point: **the DAG needs no separate checkpoint structure. `run_agents.status` IS the
checkpoint.** Resume = re-read which agents are `succeeded` and recompute the frontier. This
is why putting graph state in Postgres (not worker memory) back in chunk 7 paid off here for
free.

**Part 2 — wire the Agents tab to live data.** `lib/api.ts`'s `fetchRun` now maps the API's
`agents` (a superset of the UI's `AgentNode` — same `id/name/role/status/dependsOn/col/row/
summary/metrics`) and `messages` (API `fromAgentId/toAgentId` → UI `from/to`). The run-detail
page already passed `run.agents`/`run.messages` to `RunDetail`, so the dependency graph,
per-agent inspector, and communication feed are now **live** — they update as the worker
drives each agent's status. Verified the API serves the full 7-agent graph + 8 hand-offs +
artifact that the tab renders.

**Files to read:**
- `apps/worker/internal/agent/dag.go` — the `completedAtStart` seeding at the top of `RunDAG`
  is the entire resume mechanism. That's it. No special-case resume path.
- `apps/web/lib/api.ts` — `fetchRun`'s agent/message mapping (the shape-bridge to the UI).

**Concepts to internalize:**
- **Resume falls out of the model when state is durable.** We wrote *zero* dedicated resume
  code for the DAG — seeding `done` from `status='succeeded'` and recomputing the frontier IS
  the resume. When your source of truth is the DB and your loop is idempotent over it, crash
  recovery is nearly free. That's the dividend of the chunk-1 "DB is the truth" decision.
- **API-superset / UI-subset shapes.** The backend returns more fields than the UI needs; the
  client maps down. This keeps the two evolvable independently — the API can add fields without
  breaking the UI, and the UI names things its own way (`from` vs `fromAgentId`).

**Still mock (labeled):** the Browser tab (`session={null}`) until the browser layer. Other
top-level pages (dashboard, workflows list, agents library, notifications) still read
`lib/mock-data.ts` — incremental follow-ups.

---

## 15. Chunk 10 — the browser layer (BrowserProvider) (DONE)

**What we built:** browser-role agents now drive a browser session — navigate, extract,
screenshot — with everything persisted to the `browser_sessions/actions/shots/console`
tables and surfaced in the run-detail **Browser tab**. Behind a `BrowserProvider` interface
with two implementations selected by env (the same seam as the LLM layer):
- **simulated** (default): drives the real browser_* tables with plausible activity, **no
  external service, zero cost, fully demoable**. This is what makes the feature runnable
  without a Browserbase account.
- **browserbase**: creates a real hosted Chromium session via the Browserbase API, exposes
  the live-view URL, releases on close. **Code-complete; activates when `BROWSERBASE_API_KEY`
  is set.** (The CDP page-driver over the session WebSocket is the one documented follow-up;
  session lifecycle + action logging are wired.)

**Files to read:**
- `apps/worker/internal/browser/browser.go` — the `Provider`/`Session` interface + `New()`
  env switch + the `Persister` interface (the slice of the queue the browser writes through —
  defined here, not imported, to keep the dependency one-way).
- `apps/worker/internal/browser/sim.go` — simulated provider (drives the DB tables).
- `apps/worker/internal/browser/browserbase.go` — managed provider (real API calls).
- `apps/worker/internal/queue/browser.go` — the DB write path (sessions/actions/shots/console).
- `apps/worker/internal/agent/dag.go` → `runBrowser` — a browser-role agent opens a session,
  does navigate→extract, and feeds the observations into its LLM prompt.
- `apps/web/lib/api.ts` → `fetchBrowserSession` + run-detail page wiring.

**Verified:** ran the flagship; the `Navigator` (browser-role) agent opened a session,
visited 2 pages, recorded 4 actions + 2 filmstrip frames + 4 console lines, closed
`succeeded`. The `/api/runs/{id}/browser` endpoint serves it; the Browser tab renders it.

**Concepts to internalize:**
- **The provider interface IS the design-doc recommendation in code.** §6 said "start on
  Browserbase behind an interface, swap to self-hosted later without touching the engine."
  Here the interface *also* absorbs the no-account dev case (simulated). Same seam, two payoffs:
  cost-driven backend swap later, and demoability now.
- **Define the dependency boundary from the consumer's side.** The browser package declares a
  `Persister` interface for the exact DB methods it needs, rather than importing `queue`. So
  the dependency points one way (queue → browser via the runtime), and the browser layer is
  unit-testable with a fake persister. This is the interface-segregation principle paying off.
- **Treat the browser as an external, isolated thing** (design §10): one session per agent-run,
  page content untrusted. The simulated provider models the same lifecycle so the shape is
  right when the real one slots in.

---

## 16. Chunk 11 — notifications: "ping me when it's done" (DONE)

**What we built:** when a run reaches a terminal state, the worker (1) writes an in-app
`notifications` row (the UI bell, served by `/api/notifications`) and (2) pushes to external
channels — webhook (real, locally testable) and email (structured + logged; SMTP is the
follow-up) — selected by env. This delivers the core "close your laptop, get pinged" promise
off the run's state transition.

**Files to read:**
- `apps/worker/internal/notifier/notifier.go` — the `Channel` interface + `Notifier` fan-out;
  webhook + email channels; `New()` env switch.
- `apps/worker/internal/queue/notify.go` — `LoadRunMeta` + `CreateNotification` (the in-app row).
- `apps/worker/internal/runner/runner.go` → `notify()` — called AFTER the run is durably
  terminal.

**Verified:** ran the flagship with a local webhook listener + `NOTIFICATION_EMAIL_TO` set.
The webhook received a real JSON payload (runId, workflow, status, deep-link URL); the in-app
notification row was written and served by the API; the email channel fired with the right
subject. All on the `succeeded` transition.

**Concepts to internalize:**
- **Notify AFTER the durable state change, never before, and never let it fail the run.** The
  `notify()` call is after `Finish` succeeds, and every delivery error is logged-not-propagated.
  A missed ping must never roll back a completed run — the run's truth is the DB row, the
  notification is a side effect. Ordering + best-effort is the whole correctness story.
- **Two notification surfaces, different guarantees.** The in-app row is durable (it's a DB
  write in the run's own flow); external delivery is best-effort (networks fail). Don't conflate
  them — the bell is the source of truth, the webhook/email are conveniences.
- **Driven off state transitions, fanned to channels.** The same event ("run finished") fans
  out to N channels via one interface. Adding Slack/SMS later is a new `Channel`, nothing else
  changes — same pattern as LLM and browser providers. The platform is now three swappable
  seams (model, browser, notify) around one durable core.

---

## 17. Chunks 12–16 — from platform to a real product (DONE)

This block turned the placeholder agent into a real one: the **AI Digest** workflow that
fetches live AI research/news and emails you a digest. It also stripped all demo data and
put the entire frontend on live data.

**Chunk 12 — minimal real bootstrap + run input.** `seed.go` rewritten: 1 workspace, 1
member, the real **AI Digest** workflow (4 parallel fetchers → Editor), and **no fake
runs/logs/notifications**. Migration `0006_run_input.sql` adds `runs.input jsonb`; `Launch`
stores it; the worker reads it (topic, sources, email). A run is now *parameterizable*.

**Chunk 13 — real content sources.** `internal/sources/` hits real public APIs: arXiv (Atom),
Hacker News (Algolia), Reddit (JSON), generic web (HTTP + HTML→text). The DAG's fetcher
agents (`fetchForAgent`) pull real items and inject them into the LLM prompt — the agent
summarizes *real data*. Verified live: 8 real arXiv papers + 10 HN stories per run. (Reddit
403s from datacenter/CI IPs — handled gracefully as a skippable source.)

**Chunk 14 — Browserbase CDP driver.** Added `chromedp`; the Browserbase provider connects
to the session's CDP WebSocket (`NewRemoteAllocator`) and really navigates + extracts page
text. Code-complete; activates with `BROWSERBASE_API_KEY`. (Untested live — no key — but the
integration is correct; simulated provider remains the keyless default.)

**Chunk 15 — real SMTP email + digest payload.** The email channel sends over real SMTP
(`net/smtp`, Gmail app-password via env); the run's digest is the email body and webhook
payload (not just a status link); recipient comes from `run.input.email`. Verified against a
local SMTP catcher: a real RFC822 email with real arXiv/HN items in the body was delivered.

**Chunk 16 — frontend fully on live API.** All 8 remaining pages (dashboard, workflows
list+detail, agents, notifications, browser index+detail, command palette) migrated off
mock to `lib/api.ts`; `lib/mock-data.ts` deleted. Dashboard KPIs (`stats_service.go`) now
**computed from real runs** (runsToday/successRate/tokens/spend), not seeded constants.

**Concepts to internalize:**
- **"Real" is a property of the edges, not the core.** The durable engine was already real
  after chunk 9; what made it a *product* was real *inputs* (sources), real *outputs* (email),
  and real *parameters* (run.input). The platform didn't change — its edges connected to the
  world. That's the platform-vs-agent line made concrete.
- **Every external dependency goes behind a provider seam.** Sources, LLM, browser, notify —
  each is an interface with a real impl and a keyless/degraded fallback. This is why the whole
  system runs and is testable with zero external accounts, yet flips to fully-real with env
  vars. It's the single most important architectural habit in the codebase.
- **Strip demo data early once you have real flows.** Fake seed data hides whether the real
  path works (and the reaper nearly ate it — chunk 8). A minimal bootstrap + real runs is both
  more honest and less bug-prone.
- **Graceful degradation per source.** A blocked Reddit, a key-less browser, an LLM-less
  synth — each degrades to "skip / simulate / boilerplate" without failing the run. Partial
  results beat all-or-nothing for an agent that aggregates many sources.

**To run it for real (your use case):**
```bash
# real LLM synthesis + real email to your inbox:
ANTHROPIC_API_KEY=sk-... \
SMTP_HOST=smtp.gmail.com SMTP_PORT=587 SMTP_USER=you@gmail.com SMTP_PASS=<app-password> \
DATABASE_URL=postgres://agently:agently@localhost:5433/agently /tmp/agently-worker
# launch: POST /api/workflows/ai-digest/runs {"input":{"topic":"AI agents","email":"you@gmail.com"}}
# add BROWSERBASE_API_KEY + BROWSERBASE_PROJECT_ID to make the Web Fetcher use a real browser.
```

---

## 18. Chunk 17 — the front door: a prompt becomes a running agent crew (DONE)

**Why this chunk exists.** Everything before this built a world-class *engine* but no
*front door*: the "New workflow", "Run now", and "New run" buttons were decorative (no
`onClick`), and — more deeply — `WorkflowService.Create` produced a workflow with
`current_version_id = nil` and `agent_count = 0`, i.e. **no graph**, so even a created
workflow couldn't run. There was no way to go from an idea ("email me AI research every
morning") to a runnable workflow. This chunk closes that gap: **you type a prompt, we
compile it into an agent graph, and one click runs it** — the n8n-from-a-sentence flow.

**The end-to-end flow now (trace it):**
```
You type a prompt in the New-workflow dialog (apps/web/components/create-workflow-dialog.tsx)
   │  POST /api/workflows/plan   (debounced live PREVIEW — no save)
   ▼
API handler/workflows.go planWorkflow → services.WorkflowService.Plan
   │
   ▼
services/planner.go  CompilePrompt(prompt)               ← the heart of this chunk
   ├─ deterministicPlan(): keyword/regex parse (always works, no key)
   └─ overlayLLM(): OpenAI/Anthropic JSON refines it (best-effort; falls back)
   → returns { name, nodes[], defaultInput{topic,email,subreddits,urls,arxivQuery}, schedule, sources[] }
   │  (the dialog renders the agents you'll get, live, before you commit)
   ▼
You click Create →  POST /api/workflows  →  WorkflowService.Create
   ├─ insert workflow (current_version_id NULL — the FK-cycle two-pass, see chunk 1)
   ├─ insert workflow_version (the GRAPH: fetchers col 0 → Editor col 1)
   ├─ Workflows.Update → link current_version_id + agent_count   ← now RUNNABLE
   └─ store default_input on the workflow (migration 0007)
   ▼
Redirect to /workflows/{slug}; click "Run now" (run-workflow-dialog.tsx)
   │  POST /api/workflows/{slug}/runs  {input:{topic,email}}
   ▼
RunService.Launch merges workflow.default_input UNDER the per-run input → run.input
   → materializes run_agents from the version graph → status=queued
   ▼
Worker claims it → RunDAG executes the graph → fetchers pull REAL data →
Editor synthesizes → result.md artifact → SMTP email → notification.  (chunks 2–16)
```

**The compiler (`services/planner.go`) — the one idea to internalize.** A prompt is
compiled into a graph the *existing* engine already knows how to run. The trick that
makes this near-zero-code on the engine side: **the worker dispatches a fetcher to its
source by matching the agent's NAME** (`fetchForAgent` in worker `internal/agent/dag.go`
keys off "arxiv"/"hn"/"reddit"/"news"/"web"). So the planner's whole job is to emit nodes
*named* for the sources it detected, plus an Editor that depends on all of them. The graph
shape is identical to the seeded AI Digest (chunk 12) — we just generate it from a
sentence instead of hand-writing it in `seed.go`.

- **Hybrid + fail-safe.** `deterministicPlan` (pure regex/keyword) is the floor — it
  works with no key and no network, and it already nails the common case (detects
  arxiv/reddit/hn/news, extracts `r/Sub` names, an email, and "every morning" → a
  schedule). `overlayLLM` then *overlays* a model's richer reading on top (OpenAI/Anthropic
  via the tiny `services/llm.go`), and **any failure leaves the deterministic plan
  intact**. This is the same provider-seam discipline as the worker, applied to planning.
- **Why the LLM client is duplicated in the API.** The worker's `internal/llm` executes
  *runs*; the API needs a model only to *plan at create time*. Rather than couple the two
  Go modules, the API has its own ~110-line single-purpose JSON caller (`services/llm.go`).
  It must never be load-bearing — hence the deterministic fallback.

**`workflows.default_input` (migration 0007) — the template/instance split.** A run's
own `input` (chunk 6's `0006`) is the *instance*; the workflow's `default_input` is the
*template* the plan fills in (topic/email/sources/urls). `Launch` does
`mergeInput(workflow.default_input, run.input)` so a one-click "Run now" inherits the
plan while an explicit per-run value still wins. This is what lets "every run of THIS
workflow emails YOU about THESE sources" work without re-typing — and it's where a future
scheduler reads its parameters from.

**The browser is the universal source (your design call).** Instead of writing a new
source adapter per site (X, a blog, a docs page), the planner puts arbitrary URLs into
`default_input.urls`, and the browser-role **Web Fetcher** visits them. Two precise edits
made this real in worker `internal/agent/dag.go`:
1. `fetchForAgent` gained a `news` case → `sources.GoogleNews` (a keyless Google News RSS
   parse — the one genuinely-easy, high-value feed).
2. `runBrowser` now reads its targets from `input["urls"]` (the prompt's sites) instead of
   the old hardcoded `example.com`. So "summarize what's new on <site>" drives a real
   Browserbase session to that site, extracts the text, and feeds it to the agent's prompt.
   Arbitrary, JS-heavy, or auth-walled sites are the browser's job; clean feeds
   (arxiv/hn/reddit/news) still use the fast keyless APIs. That's the n8n generality.

**One resilience change worth knowing (worker `internal/llm/llm.go`).** A real provider is
now wrapped in `withFallback`: if the model returns a *terminal, non-retryable* error
(401/403/429/quota/rate-limit), the run degrades to the deterministic mock instead of
failing. The mock **echoes the REAL fetched items** into the digest, so the run still
produces useful output. Context-cancellation is *not* degraded (a canceled run must stop,
not fake a result). This is the same graceful-degradation principle the sources and browser
already follow, now applied to the model — so a dead/over-quota key never kills a run.

**Ops glue.** Both `cmd/server` (API) and `cmd/worker` now `godotenv.Load` the repo-root
`.env`, so `DATABASE_URL`, the LLM key the planner uses, SMTP creds, and
`BROWSERBASE_API_KEY` are present without manual exporting (real env vars still win). Root
`package.json` gained `dev:worker` / `build:worker`. Migration `0007` is mounted in
`docker-compose.yml` for fresh installs and was applied to the running DB.

**Files to read (in order):**
- `apps/api/internal/services/planner.go` — `CompilePrompt`, `deterministicPlan`,
  `overlayLLM`, `buildGraph`. The whole prompt→graph compiler.
- `apps/api/internal/services/workflow_service.go` — `Create` (two-pass insert + link +
  store default_input) and `Plan` (the dry-run).
- `apps/api/internal/services/run_service.go` — `mergeInput` in `Launch`.
- `apps/worker/internal/agent/dag.go` — the `news` case + `runBrowser` URL change.
- `apps/web/components/create-workflow-dialog.tsx` — the composer with the live preview.
- `apps/web/components/run-workflow-dialog.tsx` + `components/ui/dialog.tsx` — launch + the
  modal primitive (modeled on `command-palette.tsx`, no new dep).

**Verified end-to-end (real):** created `morning-ai-brief` from the prompt *"Every morning
pull the latest AI research from arXiv and Reddit r/MachineLearning, plus AI news from
Google News, and email me a digest…"* → a runnable 4-agent workflow persisted
(`current_version_id` set, `agent_count=4`, `default_input.email` stored). Launched it →
the worker fetched **8 real arXiv papers + 10 real Google News items**, ran the DAG to
`succeeded`, produced `result.md` with the real items. Plan/create/list/detail all verified
through the Next `/api/*` proxy (what the browser uses). **Two known external blockers,
both user-side, surfaced honestly:** (1) the OpenAI key is over quota (HTTP 429) — the
`withFallback` mock kept the run succeeding with real data, but real *prose* synthesis needs
a funded key or a valid `ANTHROPIC_API_KEY`; (2) Gmail SMTP returned `535 BadCredentials`
because `SMTP_PASS` is an account password — Gmail requires a 16-char **App Password** (2FA
on → myaccount.google.com/apppasswords). Fix that one value and the digest emails land.

**Deferred (recorded honestly):** scheduling. The prompt's "every morning" is *captured*
(`schedule` stored, trigger flipped to `schedule`, shown in the UI) but **not executed yet**
— there is no scheduler daemon. The clean follow-up is a tiny loop (in the worker or a new
control-plane goroutine) that, each minute, finds `trigger='schedule'` workflows whose
`schedule` is due and calls the existing `Launch` with `default_input`. All the pieces it
needs (the stored schedule + default_input + a graph-materializing Launch) now exist; only
the timer is missing.

---

## 19. Code-reading guide — architecture, flow & orchestration (current; supersedes §5)

§5 was written when the worker was unbuilt and the UI ran on `mock-data.ts`. Both are now
real. Read **this** instead. Three passes, each answering one question. Verified
file:symbol anchors — every line below points at code that exists today.

### Pass A — the nouns & the seams (what the system is)
Read these to learn the shape before any behaviour.
1. `apps/api/internal/domain/entities.go` — the nouns: `Run`, `Workflow`,
   `WorkflowVersion`, `GraphNode`, `Agent`, `Principal`. Everything manipulates these.
2. `apps/api/internal/platform/repositories.go` — storage *interfaces*. This is the seam:
   services depend on these, not on a concrete store. `platform/memory.go` and
   `platform/postgres_repos.go` are the two implementations; `platform.go`/`NewPlatform`
   is the one assembly line that picks which (env `DATABASE_URL`).
3. `packages/db/migrations/0001_init.sql` → `0003_queue.sql` → `0007_workflow_default_input.sql`
   — the real schema, the queue function, and the per-workflow default run-input. Map the
   SQL tables back to the Go entities from step 1.
4. `packages/contracts/src/api.ts` — the API/UI contract. The shape the web app consumes.

### Pass B — the request flow (control plane: how a run is born)
Trace one request end to end. Each line hands off to the next.
1. `apps/api/cmd/server/main.go` → `handler/router.go` — chi routes; `main` builds the
   `Platform` and serves `:8080`.
2. **Front door (the prompt→workflow compiler):**
   `handler/workflows.go:planWorkflow` → `services/workflow_service.go:Plan` →
   `services/planner.go:CompilePrompt`. Read `CompilePrompt` (l.66): `deterministicPlan`
   (keyword/regex scan, l.94) runs first, then `overlayLLM` (l.165) refines it if a key is
   present, then `buildGraph` (l.226) emits one fetcher `GraphNode` per source + an Editor
   sink. **Offline-safe by construction** — the LLM only *overlays*.
3. **Persist a runnable workflow:** `handler/workflows.go:createWorkflow` →
   `services/workflow_service.go:Create` (l.113) — builds *and links* a
   `WorkflowVersion` (sets `currentVersionId`, materializes agents, stores `default_input`).
4. **Launch a run:** `handler/workflows.go:launchRun` → `run_service.go:Launch` — merges
   `default_input` with per-run input and inserts the run `status=queued`. (Was the
   single biggest past bug: workflows that compiled but couldn't run. See §18.)
5. **Live updates:** `handler/events.go` (SSE, in-process bus) for API-side events;
   run-detail log tailing is seq-based polling against Postgres (`apps/web/lib/api.ts`,
   `fetchRunLogsAfter`) because the *worker* — a separate process — writes the logs, not
   the API's bus. This split is the single most counter-intuitive thing in the codebase;
   §11 (Chunk 6) explains why.

### Pass C — orchestration (data plane: how a run actually executes)
This is the heart. The worker is a separate Go module (`apps/worker`).
1. `apps/worker/internal/runner/runner.go` — the claim loop. Read in order:
   `Run` (poll loop, l.52) → `claimAndExecute` (l.72) → `execute` (l.94) →
   `heartbeatLoop` (l.187). The mechanism that makes "close your laptop" work:
   `runCtx` is canceled the instant the lease is lost, so a zombie worker stops writing.
   `runner/reaper.go` requeues runs whose worker stopped heartbeating.
2. `apps/worker/internal/queue/queue.go` — the durable primitives the loop calls:
   `Claim` (l.51, `FOR UPDATE SKIP LOCKED`), `Heartbeat` (l.81), `Progress` (l.115),
   `Finish` (l.97), `Reap` (l.131). The "Postgres *is* the queue" idea, in code.
3. **`apps/worker/internal/agent/dag.go` — the multi-agent engine. Read this most
   carefully.** `RunDAG` (l.88) loops: compute the ready frontier (`readyAgents`, l.409 —
   deps all done), run it concurrently under a semaphore + `sync.WaitGroup` (l.142–143,
   bound `maxConcurrentAgents`), repeat until drained. All shared state (completion,
   outputs, log seqs) lives behind `dagState`'s mutex (l.24–66) — that is what makes
   `go build -race` clean. `runAgent` (l.182) threads each upstream's output into the
   downstream prompt (`buildAgentPrompt`, l.447) and records hand-offs as `agent_messages`.
4. **The two work modes a node can take**, both in `dag.go`:
   `fetchForAgent` (l.305, dispatches on the node *name* — `arXiv`/`HN`/`Reddit`/`News`/`Web`
   → real source fetch in `internal/sources/`) and `runBrowser` (l.269, opens a Browserbase
   session and visits the **prompt's** URLs — no per-site adapters; the browser is the
   universal source). `internal/llm/llm.go` is the synthesis seam with `withFallback`
   (degrades on quota/auth instead of failing the run).
5. **Crash-resume for the DAG:** completion is read back from `run_agents.status`
   (`internal/queue/graph.go:LoadAgents`/`SetAgentStatus`), so a killed worker resumes
   exactly-once — finished agents are not re-run. Proven in §14 (Chunk 9).

### The one trace that ties it together
`POST /api/workflows/{slug}/runs` → `launchRun` → row `queued` → worker `Claim` →
`execute` holds the lease → `RunDAG` walks the graph (fetchers in parallel → Editor sink) →
`Finish` writes the terminal status + digest artifact → notifier fans out → UI polls the
final timeline. If you can narrate that path from memory, you understand Agently.

> **Vendored Activepieces** (`external/activepieces/`) is **reference material, not yet
> wired in** — nothing in `apps/` imports it. It's an open-source workflow-automation
> platform (a Zapier/n8n alternative: visual flow builder, a "pieces"/node integration
> framework, a flow-execution engine). It was vendored on 2026-06-10 as a future source for
> the flow-builder UI, the pieces framework, and the engine. Only the MIT portions are
> present — the commercial `ee/` dirs were stripped, so its *full* API server does not
> compile as-is (38 dangling `./ee/...` imports). The components we'd actually lift
> (`packages/server/engine`, `packages/pieces`, `packages/web` flow builder, `packages/shared`)
> are clean. Provenance, strip manifest, and upgrade policy: `external/activepieces/VENDOR.md`;
> license obligations: `NOTICE.md`.

---

## 20. v1-6 — the second execution plane + the visual builder (DONE)

Two big moves land on the `v1-6` branch. First (integration commit `60beff53`) a
**second execution plane** — `apps/reasoner`, a Python service that runs agent
graphs on **Temporal + LangGraph**, traced in **Langfuse**, browsing via
**Browserbase**. Second (this session) the **n8n-style visual builder**: draw a
graph on a canvas, save it, and the reasoner executes that arbitrary DAG.

### 20.1 The reasoner: durability via Temporal, not lease/heartbeat

The Go worker (§8–17) earns durability with the hand-rolled lease + heartbeat +
reaper loop over Postgres. The reasoner earns the *same* promise a different way:
**Temporal's event history is the checkpoint.** Each LangGraph node is declared
`execute_in="activity"`, so it runs as a Temporal activity — durable, retried, and
not re-run on workflow replay. Kill the worker mid-browse and Temporal resumes at
the in-flight activity, not from the top. Same "close your laptop" guarantee, a
different engine underneath.

Crucially the two planes **share one source of truth**: the reasoner writes the
identical `runs / run_logs / run_agents / artifacts / browser_*` rows the Go worker
writes (`apps/reasoner/reasoner/db.py` deliberately mirrors
`apps/worker/internal/queue/*`), so the existing Go API + Next.js UI render a
Temporal-backed run **unchanged**. The control plane routes work between planes by
one column: the Go API creates a run with `engine='temporal'`; the reasoner's
**dispatcher** (`dispatcher.py`) polls those rows and starts a Temporal workflow
(idempotent — the run id *is* the workflow id), while the Go worker's
`claim_next_run` is scoped to `engine='native'`. Neither plane sees the other's runs.

Files to read (in order): `apps/reasoner/reasoner/worker.py` (entrypoint: a Temporal
Worker + the dispatcher loop, one connection) → `workflow.py` (the tiny deterministic
workflow that compiles + invokes the graph) → `graph.py` (the LangGraph itself) →
`db.py` (the Postgres write-back) → `llm.py` / `browser.py` / `obs.py` (the same
provider-seam discipline as the Go worker: real Anthropic/Browserbase/Langfuse when
keys are set, deterministic/simulated fallback otherwise, so it runs keyless).

### 20.2 The visual builder — draw a graph, run it

Chunk 17 (§18) compiled a *sentence* into a graph. This adds the other front door:
compose the graph **directly** on a React Flow canvas. The full seam, five parts:

```
apps/web builder canvas  (components/builder/workflow-builder.tsx, React Flow / @xyflow/react)
   drag nodes from the palette (node-catalog.ts: 15 node types across trigger/agent/tool/logic/output)
   configure each in the inspector (node-inspector.tsx, fields from NODE_FIELDS)
   │  Save →  lib/api.ts saveWorkflowGraph(slug, {nodes, edges})
   ▼
lib/builder-graph.ts   ← the mapping seam (React Flow ⇄ domain GraphNode)
   toBuilderNodes(): React Flow {nodes,edges} → GraphNode[]  (key from node id, role from kind,
      dependsOn computed from incoming edges, canvas x/y preserved under config._position)
   │  PUT /api/workflows/{slug}/graph
   ▼
apps/api  handler/workflows.go:saveWorkflowGraph → validate/api.go:ParseSaveGraphInput
   → services/workflow_service.go:SaveGraph  (versions the graph: new workflow_version, bumps
     current_version_id + agent_count — same versioning the prompt-compiler Create uses)
   ▼
run launched with engine='temporal'  → reasoner dispatcher → ReasoningWorkflow
   ▼
apps/reasoner  dispatcher routes: composed graph → DynamicWorkflow (per-node activities);
   else → ReasoningWorkflow (static). DynamicWorkflow: load_graph activity (fetch + seed)
   → deterministic topo loop → one run_node activity per node → finish_run activity.
   (Fallback: a composed run reaching ReasoningWorkflow still executes via
   graph.py:route_node → dynamic_node → engine.execute_graph in a single activity.)
```

**The dynamic engine (the heart of this chunk).** `apps/reasoner/reasoner/engine.py`
+ `nodes.py` execute an arbitrary user DAG:
- `engine.topo_order` — Kahn topological sort over `dependsOn`, with **cycle and
  missing-dependency detection** (raises `GraphError`) and a stable original-order
  tie-break. `_grid` assigns each node a `(col,row)` by longest-path depth so the
  live Agents-tab graph lays out sensibly.
- `engine.execute_graph` — creates every `run_agent` up front (so the whole graph
  renders immediately), then runs nodes in dependency order, threading each node's
  output into its dependents (`{{outputs.<key>.<field>}}` templating), writing
  progress / logs / usage / hand-off messages / artifacts. Per-node failure fails the
  run cleanly (records the failed node, marks the run `failed`).
- `nodes.py` — the **type→handler registry** keyed by the catalog id, kept in sync
  with `NODE_FIELDS` in the web `node-catalog.ts`. All 15 types are handled:
  `agent.llm`/`agent.chat` (real LLM), `tool.browser` (real Browserbase),
  `tool.http` (httpx), `output.report` (writes an artifact) are **executed for
  real**; `tool.code`/`tool.db`, `output.email`/`slack`, and `logic.*` are
  **recorded honestly, not executed** (matching the catalog's own "recorded, not
  executed" help text) — a later phase wires them to real runtimes.

**How this plugs into the existing graph without a rewrite:** one registered graph,
two paths, chosen at runtime. `graph.py` adds a `route` activity that loads the
composed graph and a **conditional edge** (`_route`, a pure workflow-side function)
that sends composed workflows to the new `dynamic` activity and everything else down
the original static `plan→browse→synthesize→deliver` chain. `build_graph().compile()`
validates the wiring.

**Files to read (in order):**
- `apps/web/components/builder/workflow-builder.tsx` — the canvas (drag/drop, save/load).
- `apps/web/components/builder/node-catalog.ts` — the 15 node types + their inspector
  fields (`NODE_FIELDS`) + `defaultConfig`. The **contract** the reasoner mirrors.
- `apps/web/lib/builder-graph.ts` — the React Flow ⇄ GraphNode mapping (the seam).
- `apps/api/internal/services/workflow_service.go:SaveGraph` — graph versioning.
- `apps/reasoner/reasoner/plan.py` — pure, IO-free graph logic (topo/plan/skip/gate),
  importable inside the Temporal workflow sandbox.
- `apps/reasoner/reasoner/engine.py` + `nodes.py` — the dynamic DAG executor (IO steps +
  single-activity fallback) + handler registry; re-exports the `plan.py` helpers.
- `apps/reasoner/reasoner/workflow.py` — `ReasoningWorkflow` (static) + `DynamicWorkflow`
  (per-node orchestrator); `activities.py` — the per-node Temporal activities.
- `apps/reasoner/reasoner/graph.py` — `route_node` / `_route` / `dynamic_node` (fallback).
- `apps/reasoner/tests/test_engine.py` — 25 tests (toposort/cycle/diamond, templating,
  condition eval, end-to-end run, node-failure→run-failed, plan-module purity/re-exports,
  resume-skip of succeeded nodes, per-node orchestration ordering/skip); run with
  `DATABASE_URL=… .venv/bin/python -m unittest discover -s tests`.

**Concepts to internalize:**
- **Two engines, one truth.** The durability mechanism can differ per plane
  (Go lease/heartbeat vs Temporal event history) as long as both write the same
  Postgres rows. The UI/API don't know or care which plane ran a job — `engine` is
  just a routing column. This is the control/data-plane split taken one level further.
- **The catalog is the contract between UI and engine.** `node-catalog.ts`'s
  `NODE_FIELDS` (keys the inspector writes) and `nodes.py`'s registry (keys the
  handler reads) must stay in lockstep — they're the same interface expressed in two
  languages. Change a node's config keys in one place, change both.
- **Map at the seam, keep the ends pure.** React Flow wants `{nodes,edges}` with
  canvas positions; the domain wants `GraphNode[]` with `dependsOn`. `builder-graph.ts`
  is the *only* place that knows both shapes — the component speaks React Flow, the API
  speaks domain, neither leaks into the other. `dependsOn` (control-flow) is derived
  from edges; positions ride along in `config._position` (presentation), so a
  save→load round-trip is identity.

**Honest limitations (recorded to revisit):**
- **The composed DAG now has *per-node* durability** (v1-6 follow-up — DONE). Composed
  runs are dispatched to a dedicated `DynamicWorkflow` (`reasoner/workflow.py`) that
  drives the topological loop in deterministic *workflow* code and invokes **one
  Temporal activity per user node** (`activities.run_node`). Each node lands its own
  event-history checkpoint, so a crash resumes at the in-flight node — already-succeeded
  nodes are served from Temporal's history and never re-run, matching the per-node
  durability the static graph has. This sidesteps the LangGraph+Temporal plugin's
  fixed-shape-at-startup constraint (the plugin registers graph *shapes* at worker
  startup, so a per-run arbitrary shape can't be one-activity-per-node *through the
  plugin*) by orchestrating the arbitrary DAG in plain workflow code instead. The
  ordering/skip/handler logic is shared with the old single-activity path via
  `reasoner/plan.py` (pure, IO-free) + `reasoner/engine.py` helpers; the single-activity
  `dynamic_node` remains as a coarser-grained fallback. Real per-item `logic.loop`
  fan-out is still a follow-up.
- **`tool.code` / `tool.db` are recorded, not executed** (their source is preserved for
  a later sandboxed executor). `logic.branch` / `logic.filter` now **do** prune their
  downstream subgraph on a false condition (skip-propagation in `plan.py`/`engine.py`);
  `logic.loop` still resolves an item list without per-item fan-out.
- **The builder's "Test run" button is inert** (no `onClick`), and the builder is not
  yet linked from the workflow-detail page — you reach it at
  `/workflows/{slug}/builder` directly.

### 20.3 What's verified vs not
Verified: web `tsc --noEmit` clean; Go API save-graph tests pass; the 25 reasoner
engine tests pass (incl. plan-module purity/re-exports, resume-skip of succeeded nodes,
and a deterministic re-enactment of the `DynamicWorkflow` per-node loop over the FakeDB);
`build_graph().compile()` validates the static route/dynamic wiring; `DynamicWorkflow`'s
Temporal definition loads under the sandbox pass-through; all reasoner modules
byte-compile. **Not** verified end-to-end against a live Temporal + Postgres (no local
Temporal in this session) — so the *actual* Temporal replay/resume behaviour (a real
worker crash resuming at the in-flight `run_node` activity) is designed and unit-covered
by re-enacting the loop, but not observed against a live server. The DB write-back and
dispatch are exercised only through the fakes in `test_engine.py`. The next demo should
run a built graph through a real Temporal worker, kill it mid-node, and confirm the run
resumes at the in-flight node without re-running completed ones.

---

## Changelog

- **v1-6 (DONE) — §20 added** — second execution plane + visual builder. (1) `apps/reasoner`:
  LangGraph graphs as Temporal activities (durability via Temporal event history), Langfuse
  tracing, Browserbase browsing, writing the same Postgres rows the UI renders; dispatched via
  `engine='temporal'`. (2) The n8n-style **visual builder**: React Flow canvas
  (`components/builder/*`) → `lib/builder-graph.ts` mapping seam → `PUT /workflows/{slug}/graph`
  (`SaveGraph` versions the graph) → reasoner `route`/`dynamic` nodes → `engine.execute_graph`
  runs the arbitrary DAG (`engine.py` toposort + `nodes.py` 15-type handler registry). GraphNode
  schema gained `type`+`config`. Verified: web tsc clean, Go save-graph tests pass, 9 reasoner
  engine tests pass, graph compiles. Not yet run against live Temporal. Known limits: dynamic DAG
  runs in one activity (per-node durability is v2); logic/code/db/email/slack recorded-not-executed;
  "Test run" button inert; builder not yet linked from workflow-detail.
- **Doc — §19 added** — code-reading guide (architecture / flow / orchestration) reflecting
  the *current* code: 3 passes (nouns & seams → request flow → DAG orchestration) with
  verified file:symbol anchors. Supersedes the now-stale §5 reading path (worker-unbuilt,
  mock-data era). Also documents the vendored Activepieces tree as not-yet-wired reference.
- **Chunk 17 (DONE)** — the front door: prompt→workflow compiler (`services/planner.go`,
  hybrid LLM+deterministic), `Create` now builds+links a runnable graph + stores
  `default_input` (migration `0007`), `Launch` merges default/per-run input, worker gains a
  Google News source + browser-visits-prompt-URLs + an LLM `withFallback` (degrade on
  quota/auth), and the New-workflow / Run-now / New-run buttons are wired to real dialogs
  (live graph preview). API+worker load `.env`. Verified end-to-end (real arXiv+Google News
  → succeeded run → digest); email blocked only by a user-side Gmail App-Password.
  Digest end-to-end: fetched 8 arXiv + 10 HN items, emailed a real digest.
- **Chunk 11 (DONE)** — notifications. On terminal run state, worker writes an in-app
  notification row + fans out to external channels (webhook real, email structured) via a
  `Channel` interface. Best-effort, after durable finish. Verified webhook payload + in-app row.
- **Chunk 10 (DONE)** — browser layer behind `BrowserProvider` (simulated default + Browserbase,
  env-selected). Browser-role agents navigate/extract with full action/shot/console persistence;
  run-detail Browser tab wired live. Verified the Navigator agent's session end-to-end.
- **Chunk 9 (DONE)** — multi-agent crash-resume proven (kill mid-DAG → resume from
  `run_agents.status`, exactly-once agent execution: 7 outputs, not more). Wired run-detail
  Agents tab + dependency graph + communication feed to live API data (`fetchRun` maps
  agents/messages). Web build green.
- **Chunk 8 (DONE)** — parallel agent execution. Ready frontier runs concurrently (semaphore-
  bounded, `sync.WaitGroup` join), shared state behind `dagState` mutex (`internal/agent/dag.go`).
  Verified race-clean (`go build -race`): 3 scouts ran simultaneously, fan-in waited, log seq
  integrity held (26 logs, 26 distinct seqs, no gaps).
- **Chunk 7 (DONE)** — multi-agent DAG engine. Worker reads the `run_agents` graph and
  executes it in dependency order (`internal/agent/dag.go`, `internal/queue/graph.go`);
  threads upstream outputs into downstream prompts; records hand-offs as `agent_messages` and
  per-agent metrics. Fixed `Launch` to materialize agents `idle` (was `running`). Verified on
  the 7-agent flagship: correct topological order, fan-in join waits for all deps.
- **Chunk 6 (DONE)** — run-detail page on live data with incremental log tailing
  (`fetchRunLogsAfter` + `afterSeq`). Watch an agent's reasoning trace stream in real time;
  polling stops at terminal status. Chose seq-tail over SSE because the worker (separate
  process) writes to Postgres, not the API's in-memory bus. Web build green.
- **Chunk 5 (DONE)** — `/runs` page wired to the live Go API (`lib/api.ts`, 2s poll). Fixed
  `Launch` to create runs `queued` (was `running`) so the worker claims them. Verified the
  full loop: POST → queued → worker executes → succeeded, streamed via the API the UI polls.
  Web build green. Demo runbook in `DEMO.md`.
- **Chunks 3 & 4 (DONE)** — native agent runtime (`internal/agent`) with an LLM provider
  seam (`internal/llm`: Anthropic + mock) and durable write path (`internal/queue/store.go`:
  logs/artifacts/usage). Checkpoint = `runs.steps_done`; resume runs `steps[startStep:]`.
  Verified: crash mid-run → resume from checkpoint → exactly-once step execution (4 model
  outputs, not 8). Real reasoning-trace logs, token/cost accounting, and a result artifact.
- **Chunk 2 (DONE)** — worker skeleton (`apps/worker`, separate module): claim + lease +
  heartbeat + reaper. Migration 0005 adds `claimed_by` for split-brain-safe ownership.
  Verified crash recovery: `kill -9` mid-run → reaper requeues → another worker resumes
  from the same step → completes. Both modules build/vet; API tests green.
- **Chunk 1 (DONE)** — API wired to Postgres. Docker compose for local PG; pgx repository
  layer implementing the same 13 interfaces; first-boot seeder; env-var storage switch.
  Verified durability (workflow survives API restart); in-memory tests still green (`-race`).
- **Initial** — seeded with current-state map, today's request flow, mental models, the
  next-milestone acceptance test, and a reading path. State: shell + in-memory API built;
  persistence + worker unbuilt.
