# Running Agently locally

Agently runs four application processes against Postgres and Temporal:

| Process | Port | Purpose |
|---|---:|---|
| API (`apps/api`) | 8090 | Control plane, planner, schedules, web API |
| Reasoner (`apps/reasoner`) | - | Temporal workflows and DAG node execution |
| Pieces worker (`apps/pieces-worker`) | 7391 | Activepieces activities, options, and triggers |
| Web (`apps/web`) | 3000 | User interface |

Docker Compose provides Agently Postgres on `5433`, Temporal on `7233`, and the
Temporal UI on `8080`; `docker-compose.langfuse.yml` provides Langfuse on `3001`.
Ollama (`:11434`) serves catalog embeddings when
`PLANNER_EMBED_MODEL=ollama/...`. `pnpm agently` starts all of it.

On a Mac without Docker Desktop, Colima works:

```bash
brew install colima docker docker-compose ollama
colima start --cpu 4 --memory 8
brew services start ollama && ollama pull nomic-embed-text
```

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

Embedding generation uses `PLANNER_EMBED_MODEL` — with the default
`ollama/nomic-embed-text` it needs only a running Ollama and takes ~90s for the
whole catalog (Gemini's free tier would take days at ~1k embeds/day). The API
refuses to start when the sidecar is absent, malformed, or built with a
different model/dimension.

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

## Deploying

Each service has a Dockerfile (build context: repo root). The full stack runs
with Compose on one host:

```bash
cp .env.example .env            # fill credentials; set ENV=production,
                                # AGENTLY_API_TOKEN and AGENTLY_WEB_PASSWORD
cd apps/pieces-worker && npm install && npm run build && npm run gen:embeddings && cd -
docker compose -f docker-compose.yml -f docker-compose.app.yml up -d --build
```

Inside containers, services the host provides need `host.docker.internal`
instead of `localhost` (for example `OLLAMA_HOST` and `LANGFUSE_HOST`).

Before exposing the app:

- `ENV=production` makes the API refuse to start without `AGENTLY_API_TOKEN`.
  The web proxy attaches the token, so browsers never see it.
- `ENV=production` also requires `AGENTLY_CREDENTIALS_KEY`, which encrypts
  stored credentials. The API encrypts any existing plaintext rows when it
  starts. Back the key up: without it stored credentials cannot be read.
- `AGENTLY_WEB_PASSWORD` puts the whole UI and API behind a password prompt.
  Serve it over HTTPS (a reverse proxy such as Caddy or a platform load
  balancer) so the password is not sent in clear text.
- `APP_BASE_URL` must be the public web URL: webhook URLs registered with
  providers and the Google OAuth redirect are built from it. Webhook ingress
  (`/api/hooks/*`) stays reachable without the password.
- Change the Compose Postgres passwords, or point `DATABASE_URL` at a managed
  Postgres and apply `packages/db/migrations` in order (skipping the
  Supabase-only `0002_rls.sql` and `0004_realtime.sql`).
- Run one API instance with `SCHEDULER=1` and `PIECES_POLLER=1`; set both to 0
  on any additional instances.

CI (`.github/workflows/ci.yml`) runs the four test suites below and builds the
API, reasoner, and web images on every pull request.
