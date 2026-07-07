# Plan: Intelligent Prompt → DAG (remove fixed sources, execute anything)

## Goal
Any prompt compiles into an arbitrary, world-class typed-node DAG and runs on the
Temporal reasoner. Remove the hardcoded 5 sources and the fixed fetchers→Editor
topology. No source cookbook is given to the LLM — it is trusted to author nodes
against the reasoner's node contract. Close the three executor gaps so the whole
catalog is real.

## What already exists (leverage, don't rebuild)
- `apps/reasoner` **already executes arbitrary typed-node DAGs** with per-node
  Temporal durability (`DynamicWorkflow`) and a single-activity fallback
  (`engine.execute_graph`). Handlers cover all 15 catalog types.
- The web builder already renders/edits arbitrary typed graphs.
- The dispatcher routes any run whose workflow has a composed graph to
  `DynamicWorkflow`.
- **The only thing stuck**: the Go compiler (`planner.go`) emits the fixed
  5-source shape and runs auto-default to `engine=native` (the Go worker, which
  can't interpret typed nodes).

## Design decisions (confirmed with user)
- Auto-route prompt-built (typed) graphs to the Temporal reasoner.
- No example/source cookbook for the LLM. Give it the **node API contract**
  (types, config keys, output fields, templating, limits) — not a source list.
- Implement the executor gaps now: real `tool.code`, real `tool.db`, real
  `logic.loop` per-item fan-out.

---

## Workstream A — Intelligent graph compiler (Go)
**Files:** `apps/api/internal/services/planner.go`, `llm.go`, new `nodecatalog.go`

1. **New `nodecatalog.go`** — single source of truth for the compiler:
   - Canonical list of the 15 node types with config keys, output fields,
     templating rules (`{{input.x}}`, `{{outputs.key.field}}`), and honest limits.
   - `graphPlannerSystemPrompt()` builds the system prompt from that spec.
   - `validateGraph(nodes)` — Go port of the reasoner's structural rules: every
     `type` known, every `dependsOn` references an existing key, no duplicate
     keys, no cycles (Kahn). Returns actionable error strings.
2. **Rewrite `planner.go`**:
   - `CompilePrompt` → LLM emits a **full graph** (nodes w/ key/type/name/role/
     config/dependsOn) + name/description/schedule/defaultInput.
   - **Repair loop** (up to 3 tries): validate → on failure, re-prompt the model
     with the specific errors → re-validate.
   - **Deterministic fallback** (no key / all retries fail): emit a sensible
     *typed* graph — `trigger.manual → agent.llm(research) → output.report`
     (+ `output.email` if a recipient was parsed) — so create still never fails.
     This replaces the 5-source fallback.
   - Delete `knownSources`, `buildGraph`, `filterKnownSources`, `sourceSpec`.
     Keep the useful deterministic extractors (email, schedule, time-of-day).
3. **`llm.go`**: raise `max_tokens` (1024 → ~4096) so a full graph fits; add a
   small multi-message helper for the repair turns. Keep the fail-safe contract.

## Workstream B — Auto-routing to Temporal (Go)
**Files:** `apps/api/internal/services/run_service.go`, `workflow_service.go`

- Add `engineForNodes(nodes []domain.GraphNode) string`: returns `"temporal"` if
  any node has a non-empty `Type`, else `"native"` (legacy digests still work).
- In `RunService.Launch`, when `input.Engine == ""`, resolve engine from the
  run's version graph instead of hardcoding `"native"`. Explicit `?engine=` wins.
- Fix `AgentCount`/`countFetchersAndEditor` to count all nodes generically.
- Result: prompt-built graphs execute on the reasoner; they also become editable
  in the builder (removes the old "prompt graphs are read-only" limitation).

## Workstream C — Close executor gaps (Python reasoner)
**Files:** `apps/reasoner/reasoner/nodes.py`, new `sandbox.py`, `plan.py`,
`engine.py`, `activities.py`, `workflow.py`, `config.py`

1. **`tool.code` → real sandboxed run** (`sandbox.py`): subprocess for
   Python/JS with hard timeout, `RLIMIT_CPU`/`RLIMIT_AS`, stripped env (no
   inherited secrets), isolated temp cwd. Upstream outputs + run input passed as
   JSON on stdin; stdout/last-expression captured as `{result, stdout}`. Honest
   limits documented (process isolation, not a VM/gVisor jail).
2. **`tool.db` → real execution, opt-in & safe**: execute SQL only against a
   dedicated `TOOL_DB_URL` (new in `config.py`); when unset, keep record-intent.
   Never touches the platform Postgres. Returns `{rows, rowCount}`.
3. **`logic.loop` → per-item fan-out**:
   - `plan.py`: pure helper computing a loop's **body** = descendants dominated
     by the loop node (every root→node path passes through it).
   - Execute the body once per resolved item, injecting `{{item}}` /
     `{{loop.<key>.index}}`; collect per-item outputs into `outputs.<key>.results`.
   - `engine.execute_graph`: implement fan-out inline (durable as one activity).
   - `DynamicWorkflow`: the loop node's activity returns the item list; the
     workflow iterates deterministically (recorded in history) invoking `run_node`
     per (item, body-node) — preserves per-node durability. Non-loop graphs
     unchanged.

## Workstream D — Keep contract in sync (Web, minor)
**File:** `apps/web/components/builder/node-catalog.ts`
- Update help text: `tool.code`/`tool.db` now execute (with caveats); `logic.loop`
  now fans out. Add `{{item}}` hint on loop-body fields. No structural changes.

## Workstream E — Tests
- **Go** (`planner_test.go`): valid graph emitted; repair recovers from a bad
  graph; fallback is a valid typed graph with no key; `engineForNodes` selection;
  cycle/bad-dep rejection.
- **Python** (`tests/`): sandbox happy-path + timeout + no-network; `tool.db`
  record vs execute; loop fan-out (N items → N body executions, results
  collected); loop with a nested gate.

---

## Execution approach (multi-agent)
The project is coupled by the node contract, and worktree isolation is
unavailable (not a git repo), so parallel file-mutating agents would conflict.
Plan: I implement the coupled core (A, B, C) sequentially to keep the contract
coherent, and delegate **non-overlapping** pieces to background agents where safe
— e.g. one agent writes the Python sandbox module + its tests, another drafts the
Go planner tests — then I integrate. I'll build/lint each side before wiring.

## Verification
- `go build ./...` + `go test ./internal/services/...` in `apps/api`.
- `python -m pytest` in `apps/reasoner`.
- End-to-end sanity: compile a non-digest prompt (e.g. "scrape my competitor's
  pricing page daily, diff it, and Slack me changes") → confirm a valid typed
  graph, engine=temporal, and a dispatched dynamic run.

## Risks / honest limits
- `tool.code` isolation is process-level (timeout + rlimits + clean env), not a
  hardened VM. Documented; `TOOL_CODE_ENABLED` gate defaults off in shared envs.
- `tool.db` executes only against an explicit `TOOL_DB_URL`; never the platform DB.
- Requires the reasoner + Temporal running for prompt-built graphs to execute
  (legacy native digests still run without them).
- LLM graph quality depends on the contract prompt; the validate→repair loop +
  typed fallback guarantee a runnable graph regardless.
