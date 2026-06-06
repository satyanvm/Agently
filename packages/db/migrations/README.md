# Database — source of truth for the schema

SQL migrations for Supabase Postgres. Applied in filename order.

- `0001_init.sql` — full schema: workspaces, members, api_keys, agent_definitions,
  workflows, workflow_versions, runs, run_agents, agent_messages, artifacts,
  run_logs, browser_sessions/actions/shots/console, notifications,
  activity_events, domain_events. Enums + indexes included.
- `0002_rls.sql` — Row Level Security: each user sees only their workspace's rows.
  (Forward-looking; enable once Supabase Auth is wired.)
- `0003_queue.sql` — `claim_next_run()` (FOR UPDATE SKIP LOCKED) + stalled-run
  reaper for the worker.
- `0004_realtime.sql` — adds tables to the Supabase realtime publication and a
  `pg_notify` trigger on `domain_events` for non-Supabase gateways.

Apply with the Supabase CLI or by running the files against `DATABASE_URL`:

```bash
for f in packages/db/migrations/0*.sql; do psql "$DATABASE_URL" -f "$f"; done
```

## Relationship to the rest of the codebase

The schema mirrors the canonical entity model in **`@agently/contracts`**
(`packages/contracts/src/entities.ts`) one-for-one — table per entity, snake_case
columns, prefixed text PKs (`wf_…`, `run_…`). When a Postgres-backed
`Repositories` implementation is written (`packages/core/src/platform/`), it maps
these rows to those types directly. The in-memory store used by the mock backend
today already speaks the same shapes, so swapping storage touches only the
repository implementation — not services, routes, or the worker.

> Note: an earlier scaffold (`packages/core/src/types.ts`) modeled a simpler
> single-agent world (Agent/Run/LogEntry). That file is retained for backward
> compatibility; the canonical, current model is `@agently/contracts`.
