# Agently

Agently is a durable execution platform for long-running, multi-agent workflows.
The API and web app form the control plane; Temporal, the Python reasoner, and the
Node pieces worker form the execution plane.

## Architecture

```text
Next.js web -> Go API -> Postgres
                           |
                    queued runs
                           v
               Python reasoner dispatcher
                           |
                 Temporal workflow history
                           |
               one activity per DAG node
                  /          |          \
             Anthropic   Browserbase   pieces worker
```

- `apps/api`: workflow planner, CRUD, schedules, triggers, logs, credentials, and
  the web API.
- `apps/reasoner`: the only run executor. It walks DAGs in deterministic Temporal
  workflow code and persists each node result as an activity checkpoint.
- `apps/pieces-worker`: executes `pieces.*` actions/triggers and stores durable
  trigger state.
- `apps/web`: live UI backed only by API data.
- `packages/nodes`: hand-written and generated integration catalogs.
- `packages/db/migrations`: the Postgres schema and upgrades.

The retired native Go execution worker and the `runs.engine` selector have been
removed. Migration `0013_drop_retired_execution_columns.sql` also removes its
lease columns.

## Strict failure model

Agently does not manufacture successful-looking work:

- Planner router/map/reduce failures return the provider error.
- Missing Anthropic or Voyage credentials prevent planning.
- Missing or stale piece embeddings prevent API startup.
- Missing Browserbase, Langfuse, SMTP, database, or integration credentials fail
  startup or the selected node.
- An unavailable pieces worker fails piece nodes.
- The web app does not synthesize costs, logs, browser sessions, or activity data.

## Models

The planner uses `PLANNER_MAP_MODEL` (default `claude-haiku-4-5`) for routing and
map selection, and `PLANNER_MODEL` (default `claude-opus-4-8`) for graph creation
and repair. Voyage `voyage-4` embeddings provide catalog recall. The reasoner uses
`REASONER_MODEL` (default `claude-opus-4-8`) and
`REASONER_SYNTHESIS_MODEL` (default `claude-sonnet-4-6`).

## Local development

```bash
cp .env.example .env
# Fill every required credential.
pnpm install
pnpm agently
```

Open the web app at `http://localhost:3000` and Temporal UI at
`http://localhost:8080`. See `RUNBOOK.md` for setup, migrations, service restart,
and verification commands.
