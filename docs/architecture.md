# Agently — architecture as of v1-13

> **This is the single current-state document.** Root `architecture.md` is the
> *historical build log* (2000+ lines, sections 8–17 describe the retired native Go
> engine in present tense — do not trust it for how the system works today).
> `README.md` is the product pitch. `v1-13.md` is the deep-dive on piece routing.
> This file supersedes all of them on "how it actually works right now."
>
> Verified against branch `v1-13` @ `5c189039`. `go build ./...` and `go test ./...`
> pass clean.

---

## 0. The one sentence

**A prompt becomes a typed DAG; the DAG executes durably on Temporal; every node —
including ~6,700 real third-party integration actions — is a checkpoint you can
crash through.** Durability is the product; the catalog is the moat; the compiler is
the hard part.

---

## 1. Processes (what is actually running)

| Process | Lang | Port / queue | Role | Started by |
|---|---|---|---|---|
| Postgres | Docker | 5433 | **source of truth** — workflows, runs, agents, events, credentials, trigger state | `docker-compose.yml` |
| Temporal (+ own PG + UI) | Docker | 7233 / UI 8080 | durable execution engine | `docker-compose.yml` |
| **API** | Go | **8090** | control plane: REST + SSE, prompt→DAG compiler, scheduler, poller | `agently.sh` |
| **Reasoner** | Python | queue `agently-reasoner` | data plane: Temporal worker + dispatcher; runs the graph | `agently.sh` |
| **Pieces worker** | Node | queue `agently-pieces` + HTTP **7391** | runs real Activepieces packages; dynamic options; trigger runtime | `agently.sh` (optional) |
| **Web** | Next.js | **3000** | UI; proxies `/api/*` → 8090 | `agently.sh` |

`apps/worker/` (Go, 3.1k LOC) is **dead code**. It was the native lease+heartbeat
engine, retired in v1-11 (`e364333f`); migration `0010_retire_native_engine.sql`
dropped its SQL functions and `validate/api.go` rejects `engine='native'`. It is not
built, not started, not tested. Only `archive/worker-README-pending.md` was
committed — the directory itself was never moved. **It should be deleted or moved to
`archive/`.**

Everything downstream of the API degrades independently: no pieces worker → piece
nodes record intent; no embedding sidecar → router reads the full directory; no LLM
key → deterministic fallback graph. A run never fails because a dependency is absent.

---

## 2. The two halves

### Control plane — `apps/api` (Go, 10.8k LOC)

Owns everything that must stay up regardless of what an agent does.

```
cmd/server/main.go          godotenv → Postgres (or in-memory) → chi router → :8090
  ├─ go scheduler.Start()   cron/"daily 09:00" workflows            (SCHEDULER=0 disables)
  └─ go piecesPoller.Start() polling triggers, 5m tick              (PIECES_POLLER=0 disables)

internal/handler/     thin HTTP: workflows, runs, credentials, integrations, hooks, SSE
internal/services/    the logic — planner, catalog, run/workflow lifecycle, triggers
internal/platform/    Postgres repos (1.3k LOC), in-memory twin, event bus, Temporal client
internal/domain/      entities, enums, validation
```

Services depend on repository **interfaces**, so the in-memory store is a full twin
of Postgres and the whole suite runs with no database.

### Data plane — `apps/reasoner` (Python, ~4.5k LOC)

```
worker.py       Temporal worker (activities + workflows) + dispatcher loop
dispatcher.py   polls runs WHERE engine='temporal' AND status='queued'
                → client.start_workflow(id=f"agently-run-{run_id}")   ← idempotent by construction
workflow.py     DynamicWorkflow (per-node durability) | ReasoningWorkflow (static legacy path)
plan.py         pure topo-order / skip / descendants logic — the Python twin of validateGraph
activities.py   every side effect: load_graph, run_node, skip_node, add_edge, set_progress, finish_run
nodes.py        node-type → executor registry (LLM, browser, http, email, slack, code, db, logic.*)
catalog.py      catalog-driven `_integration` execution
```

---

## 3. The two flows

### 3.1 Prompt → DAG (the compiler)

`apps/api/internal/services/planner.go` — **route → map → reduce**, all of it in the
control plane, none of it on the critical path of a run.

```
                              user prompt
                                   │
              ┌────────────────────┴────────────────────┐
              │           ROUTE  (one small-model call)  │
              │  piecesembed.go: embedding prefilter     │   optional
              │    prompt vector · 6,758 node vectors    │   (sidecar absent today)
              │    → top-100 clusters, TIE-INCLUSIVE     │
              │  piecesrouter.go: routeClusters()        │
              │    directory of ALL clusters (~700 piece │
              │    + 12 hand-written) → ≤12 slugs        │
              │  any failure → lexical topPieceClusters  │   fallback
              └────────────────────┬────────────────────┘
                                   ▼
              MAP: ≤12 concurrent small-model calls, one per routed
                   cluster, each returning ≤8 node ids in rank order
                                   ▼
              capFairly: round-robin in ROUTER RELEVANCE ORDER → 32 ids
                                   ▼
              REDUCE: strong model authors the typed DAG from only the
                      selected schemas + 15 builtins
                                   ▼
              validate → repair (×2) → deterministic fallbackGraph
```

Three properties worth internalizing, because they were each a bug once:

- **Recall lives in ROUTE, precision lives in MAP.** The router is prompted to
  over-include; the map models narrow. Before v1-13 selection was lexical token
  overlap — "spreadsheet" never found `google-sheets`; measured recall@12 was 67%.
- **No stage may order by alphabet.** The old code did `sort.Strings(selected)[:32]`,
  silently dropping everything late in the alphabet. `capFairly` now cuts in relevance
  order; the final sort is cosmetic (reduce-prompt reproducibility only). Same fix in
  the embedding prefilter: the top-100 cut is tie-inclusive, so equal scores can never
  be decided by name.
- **Hand-written clusters compete too** (PR #25). They used to fire 12 unconditional
  map calls every compile; now they sit in the same router directory as the pieces.
  A typical compile went from ~13–25 map calls to ~3–7.

The catalog behind it:

| population | clusters | nodes | source |
|---|---|---|---|
| builtin + hand-written integrations | ~12 | ~193 | `packages/nodes/catalog/*.json` (13 files, hand-authored) |
| Activepieces pieces (`pieces.<slug>`) | **707** | **6,758** | `packages/nodes/pieces/index.json` (14.6 MB, generated) |

`packages/nodes/catalog/*.json` is the **one contract shared by Go, Python, and the
web builder**. `build-web.mjs` regenerates the palette; `nodecatalog.go` and
`reasoner/catalog.py` are two readers of the same file.

### 3.2 Launch → durable run

```
POST /api/runs  →  run_service.Launch()  →  INSERT runs(engine='temporal', status='queued')
                                                    │
   reasoner/dispatcher.py polls the row ◄───────────┘
                   │  start_workflow(id="agently-run-<run_id>")   ← duplicate = ALREADY_EXISTS, ignored
                   ▼
   DynamicWorkflow (deterministic — no DB, no clock, no random in workflow code)
       load_graph activity ──────────────► graph + agent ids + prior status
       for node in topological order:
           skip?    → skip_node activity, prune descendants
           resumed? → prior status 'succeeded' in DB, don't redo side effects
           pieces.* → prepare_piece_node  (our queue)
                      execute_piece       (queue agently-pieces, 30s schedule_to_start)
                      record_piece_result (our queue)
           else     → run_node activity   (600s timeout, 3 attempts)
           logic.loop → fan the dominated body out once per item, each its own activity
           add_edge activity per dependent  (live graph view)
           set_progress activity
       finish_run activity
                   │
                   ▼
   Postgres rows → API event bus → SSE → the UI redraws
```

**Where durability comes from:** Temporal's event history, not a lease. Kill the
reasoner mid-run and Temporal replays the loop, serving already-completed node
activities *from history* rather than re-running them, and resumes at the in-flight
node. That is per-node checkpointing. The retired Go engine got the same property from
`claim_next_run()` + `FOR UPDATE SKIP LOCKED` + heartbeat — worth knowing because it's
the better interview answer (you built it twice, two different ways).

`schedule_to_start = 30s` on the cross-queue piece call is deliberate: "nobody is
polling `agently-pieces`" is detected in 30s and degrades to record-intent, instead of
hanging for the 180s node timeout.

---

## 4. Pieces: the integration layer

Contract: `docs/pieces-runtime-contract.md` (authoritative). Shape:

- **Node id** `pieces.<slug>.<action>`; the reasoner routes any `type.startswith("pieces.")`
  to the `agently-pieces` queue — a pure string test, deterministic under replay.
- **`apps/pieces-worker`** is a standalone npm package *outside* the pnpm workspace
  (Activepieces' dependency tree would poison the root lockfile). It loads real
  `@activepieces/piece-*` packages as a library and serves one activity, `execute_piece`.
- **`:7391` HTTP surface** — `/options` (dynamic dropdown props, proxied by Go),
  `/run-trigger`, `/trigger-lifecycle`.
- **Two generators** feed `packages/nodes/pieces/`: `gen-index.ts` (the 6,758-node
  index) and `gen-embeddings.ts` (the vector sidecar — incremental + resumable).
- `external/activepieces` (184 MB) is vendored **reference only**; the worker consumes
  published npm packages. Licensing: Activepieces is MIT and usable; n8n's nodes are
  SUL-licensed and off-limits.

## 5. Credentials & triggers

- **Credentials** (`docs/credentials-contract.md`, migration `0011`): DB-backed,
  write-only CRUD at `/api/credentials`. A node config carries `__credentialId`; the
  pieces worker resolves DB-first at execution. Credential *type* is derived from the
  piece's auth schema.
- **Four trigger paths into a run:**
  1. `trigger.manual` — the Launch button.
  2. `trigger.schedule` — `services/scheduler.go`, ticked by a goroutine in the API,
     idempotent + restart-safe, `SCHEDULER_TZ` sets the zone.
  3. Webhook — `POST /api/hooks/{slug}/{nodeKey}` → run launched with
     `input.__trigger_event`. Needs `WEBHOOK_PUBLIC_BASE`.
  4. Polling — `services/piecespoller.go`, 5m tick, state in migration `0012`.

  Both daemons assume **a single API instance** (no leader election).

---

## 6. Degradation ladder (nothing here can break workflow creation)

```
embedding sidecar missing  → router reads the full 35k-token directory
router fails (no key/timeout/junk) → lexical prescreen + map all hand-written clusters
map calls fail             → empty selection; reduce still has the 15 builtins
reduce fails               → deterministic fallbackGraph
pieces worker down         → piece nodes record intent, run still succeeds
no credential              → record intent
Langfuse unset             → no-op tracer
Browserbase unset          → simulated browser
```

---

## 7. What is NOT built (honest list)

| Gap | Evidence | Severity |
|---|---|---|
| **No auth, no multi-tenancy** | `handler/middleware.go` is CORS-only; every request runs as the seeded `ws_agently` / `mem_owner` (`platform/seed.go:22,39`); "Sign in" on the landing page just links to `/dashboard` | blocks any real deployment |
| **RLS never enabled** | `0002_rls.sql` is commented scaffolding — "enable once Supabase Auth is wired" | pairs with the above |
| **No deployment path** | no Dockerfile for any service, no `.github/workflows/`, `docker-compose.yml` is dev-only (Postgres + Temporal) | can only be demoed on a laptop |
| **Webhook ingress unsecured** | `handler/hooks.go` — no HMAC/signature verification, no dedup/idempotency key, no per-hook secret | correctness + security |
| **Embedding sidecar unfinished** | `embeddings.partial.json` = **900 / 6,758** vectors; no `embeddings.json` exists, so the prefilter is **inactive in every compile today** | the router works, but untested at its designed shape |
| **Selection recall unmeasured** | `planeval`: lexical 29/43 (67%); router 7/7 but on only 3 of 22 prompts before quota death | the central claim of v1-13 is unproven |
| **`tool.code` / `tool.db` record-only** | gated behind `TOOL_CODE_ENABLED=1` / `TOOL_DB_URL` (`nodes.py:302,330`) | expected, but know it |
| **No human-in-the-loop** | no approval/pause node; `RunPaused` exists in the enum and is never set; `execute.ts` throws `unsupported('run.pause (waitpoints)')` | roadmap phase 3 |
| **Scheduler/poller are single-instance** | no leader election | fine until you scale out |

---

## 8. Reading path

Ordered so each file makes the next one make sense. Contracts first — they are the
shared vocabulary the three languages agree on.

**Level 0 — contracts (read, don't rewrite)**
1. `RUNBOOK.md` — processes, ports, what to restart.
2. `docs/pieces-runtime-contract.md` — everything `pieces.*`.
3. `docs/credentials-contract.md` — the credential store.
4. `packages/nodes/catalog/builtin.json` — what a node definition *is*.

**Level 1 — the compiler**
5. `apps/api/internal/services/nodecatalog.go` — catalog load, cluster index vs full
   schema, `validateGraph`, layout.
6. `apps/api/internal/services/planner.go` — start at the header comment, then
   `CompilePrompt` → `mapPhase` → `capFairly` → `compileGraphLLM` → `reduceSystemPrompt`
   → `fallbackGraph`.
7. `apps/api/internal/services/piecesrouter.go` + `piecesembed.go` — route stage.
8. `apps/api/internal/services/piecesindex.go` — 707 pieces → clusters.
9. `apps/api/cmd/planeval/main.go` — how recall is measured.

**Level 2 — durable execution**
10. `apps/reasoner/reasoner/plan.py` — pure graph logic.
11. `apps/reasoner/reasoner/workflow.py` — the determinism rules and per-node durability.
12. `apps/reasoner/reasoner/activities.py` → `nodes.py` → `catalog.py` — the side effects.
13. `apps/reasoner/reasoner/dispatcher.py` — the Go→Temporal bridge.

**Level 3 — the rest**
14. `apps/pieces-worker/src/execute.ts`, `credentials.ts`, `pieces.ts`.
15. `apps/api/internal/services/{run_service,workflow_service,temporal,scheduler,piecetriggers}.go`.
16. `apps/web/lib/builder-graph.ts` + `components/builder/`.

---

## 9. Change log of architectural shape

| Version | Shape change |
|---|---|
| v1-1…v1-8 | Go API + native Go worker (Postgres-as-queue, lease + heartbeat, crash-resume). |
| v1-6 | Python reasoner added as a **second** execution plane (Temporal + LangGraph + Langfuse). |
| v1-8 | Prompt→DAG map-reduce compiler over an n8n-breadth catalog. |
| v1-9 | Activepieces pieces as Temporal activities; `apps/pieces-worker`; `agently-pieces` queue. |
| v1-11 | **Native Go worker retired.** Temporal is the only engine. Gemini becomes the default LLM. |
| v1-12 | Dynamic options (worker HTTP resolver + Go proxy + web UI); catalog triggers in the builder. |
| v1-13 | **Lexical prescreen → semantic router + embedding prefilter.** Alphabetical bias removed everywhere. Plan preview made explicit instead of per-keystroke. |
