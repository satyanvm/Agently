# Archived: native Go worker (engine='native')

**Archived 2026-07-21.** This is the retired native execution engine — the
Postgres-as-queue Go worker that executed `engine='native'` runs via the
`claim_next_run()` / heartbeat / `reap_stalled_runs()` lease machinery.

Every run now executes on the Temporal + LangGraph reasoner (`apps/reasoner`,
`engine='temporal'`): the API hard-codes the engine, validation rejects
`engine=native`, and migration `0010_retire_native_engine.sql` dropped the
claim/reap SQL functions (the `claimed_by` / `heartbeat_at` columns and
historical `engine='native'` rows were kept so old run history still renders).

## Why keep it

- Reference implementation of durable execution with **zero dependency on a
  running Temporal server** — just Postgres (`FOR UPDATE SKIP LOCKED` claim,
  3s heartbeats, 15s reaper lease).
- Escape hatch if we ever need to run without Temporal again.

## To resurrect

1. Move it back: `git mv archive/worker apps/worker`
2. Restore `claim_next_run(text)` / `reap_stalled_runs(interval)` from
   `packages/db/migrations/0009_temporal_engine.sql` (engine-scoped versions).
3. Re-add `"native"` to `validEngines` in
   `apps/api/internal/domain/validate/api.go` and restore routing in
   `run_service.go` (see git history: `engineForNodes`).
4. Re-add the `worker` entries to `package.json` scripts and
   `scripts/agently.sh`.

It compiled and passed tests at the time of archival; it is **not** built,
started, or tested by any current tooling.
