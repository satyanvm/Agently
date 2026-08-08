# Running Agently locally

Agently runs four application processes against Postgres and Temporal:

| Process | Port | Purpose |
|---|---:|---|
| API (`apps/api`) | 8090 | Control plane, planner, schedules, web API |
| Reasoner (`apps/reasoner`) | - | Temporal workflows and DAG node execution |
| Pieces worker (`apps/pieces-worker`) | 7391 | Activepieces activities, options, and triggers |
| Web (`apps/web`) | 3000 | User interface |

Docker Compose provides Agently Postgres on `5433`, Temporal on `7233`, and the
Temporal UI on `8080`.

## Prerequisites

1. Copy `.env.example` to `.env` and fill every required credential.
2. Install root dependencies with `pnpm install`.
3. Create the reasoner virtual environment:

```bash
cd apps/reasoner
python3 -m venv .venv
./.venv/bin/pip install -e .
```

4. Build the pieces worker and its generated index/embeddings:

```bash
cd apps/pieces-worker
npm install
npm run build
npm run gen:index
npm run gen:embeddings
```

Embedding generation requires `VOYAGE_API_KEY`. The API refuses to start when
the sidecar is absent, malformed, or built with a different model/dimension.

## Commands

```bash
pnpm agently
pnpm agently:status
pnpm agently:logs
pnpm agently:stop
pnpm agently:down
```

Restart individual services with `pnpm agently:api`, `pnpm agently:reasoner`,
`pnpm agently:pieces`, or `pnpm agently:web`.

## Failure behavior

Agently does not substitute fake work. Planner/router/map/reduce errors fail
workflow creation with their provider reason. Runtime provider errors, missing
credentials, an unavailable pieces worker, missing Browserbase, failed delivery,
and unavailable tracing fail the affected run or prevent service startup.

## Migrations

Apply a migration to an existing local database:

```bash
docker exec -i agently-postgres psql -U agently -d agently < packages/db/migrations/0013_drop_retired_execution_columns.sql
```

For a clean database, `docker compose down -v` removes local data and the next
`docker compose up -d` applies all mounted portable migrations.

## Verification

```bash
cd apps/api && go test ./...
cd apps/reasoner && .venv/bin/python -m pytest -q
cd apps/pieces-worker && npm run build && npm test
cd apps/web && pnpm build
```
