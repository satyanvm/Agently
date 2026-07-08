# Intelligent Prompt → DAG: map-reduce compiler over an n8n-breadth node catalog

## What this delivers

Any prompt compiles into an arbitrary typed-node DAG over a catalog of ~208 node
types (15 built-ins + 193 integrations across 12 clusters), executed by the
Temporal reasoner. The old fixed 5-source / fetchers→Editor planner is gone.

## Architecture

### The catalog is data (`packages/nodes/`)
One JSON definition per integration; four generic runtimes (`http`, `browser`,
`code`, `llm`) execute all of them — adding a node means adding a JSON entry,
never code. Consumed by all three planes:
- **Go compiler** (`apps/api/internal/services/nodecatalog.go`) — indexes + schemas + validation.
- **Python executor** (`apps/reasoner/reasoner/catalog.py` + `nodes.py _integration`) — one generic handler.
- **Web palette** (`node-catalog.ts` + `integration-catalog.generated.json`, regenerate via `node packages/nodes/build-web.mjs`).

### Map-reduce compilation (`apps/api/internal/services/planner.go`)
- **Map**: one parallel call per cluster with a small fast model
  (`PLANNER_MAP_MODEL`, default `claude-haiku-4-5`) over that cluster's compact
  index → relevant node ids (≤8/cluster, ≤32 total).
- **Reduce**: the big model (`PLANNER_MODEL`, default `claude-opus-4-8`) gets the
  node contract (graph rules, templating semantics, fail-open warning) + full
  schemas of built-ins and selected nodes only → authors the complete graph.
- **Validate → repair**: structural rules (unknown types, dup keys, bad deps,
  cycles, missing required config) mirrored from `reasoner/plan.py`; errors are
  fed back verbatim, up to 2 repair rounds.
- **Deterministic fallback**: no key / hard failure → typed
  `trigger → agent.llm(research) → output.report [+ output.email]`. Creation
  never fails. Compiled plans are memoized (the create dialog previews on every
  pause in typing).
- No source cookbook: the model is trusted to design; `tool.http`/`tool.browser`
  remain universal escape hatches for anything not in the catalog.

### Auto-routing (`run_service.go`, `validate/api.go`)
Engine absent = AUTO: any typed node → `temporal`; legacy untyped digests →
`native`. Explicit `?engine=` still wins. Prompt-compiled graphs are now also
editable in the builder like hand-drawn ones.

### Executor upgrades (`apps/reasoner`)
- **Generic integration runtime** — renders `{{config.x}}` / `{{credentials.X}}`
  (+ `{{json …}}`, `{{urlencode …}}` helpers), performs the request, lifts
  `outputMap` fields; missing credential env vars degrade to record-intent with a
  loud log. Basic auth supported.
- **`tool.code` executes** (`sandbox.py`) — subprocess, 30s wall / 15s CPU /
  512MB, stripped env, isolated tmp cwd. **Gated: `TOOL_CODE_ENABLED=1`**,
  default off (process isolation, not a hardened VM — documented honestly).
- **`tool.db` executes** against `TOOL_DB_URL` only (never the platform
  Postgres); record-intent when unset.
- **`logic.loop` fans out** its dominated body (`plan.loop_body`) once per item
  with `{{item}}` / `{{loop.<key>.index}}`; collected `results` on the loop
  output; gates inside a body prune that iteration only. Implemented in both the
  per-node `DynamicWorkflow` (each body execution its own durable activity) and
  the single-activity fallback.

## Verification (all passing)
- Go: `go build ./... && go test ./...` in `apps/api` (planner fallback validity,
  validation rules, layout, routing, schedule parsing).
- Python: 39 tests in `apps/reasoner` (templating helpers, catalog routing,
  integration http + missing-cred paths, sandbox happy/stripped-env/failure,
  code/db gates, loop domination + fan-out + zero-items skip).
- Web: `tsc --noEmit` clean; palette carries all 208 types grouped by cluster.

## Honest limits
- Integration HTTP templates are authored against documented public APIs but not
  live-tested per service; failures surface as node output (`status`/`error`),
  never run crashes. Services needing SigV4/multi-step OAuth were omitted, not faked.
- `tool.code` sandbox is process-level containment; enable deliberately.
- Prompt-built graphs need the reasoner + Temporal running; legacy native digests
  don't.
- Platform triggers remain manual/schedule/webhook; service "events" are modeled
  as pollable actions.
