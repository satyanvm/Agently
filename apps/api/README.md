# Agently API (Go)

The backend for Agently — a single self-contained Go binary that serves the
HTTP contract the frontend consumes. It replaces the previous TypeScript backend
(Next.js API routes + `@agently/core` platform + worker) while the frontend stays
in TypeScript. In development the Next.js app proxies `/api/*` to this server
(see `apps/web/next.config.ts`).

## Run

```bash
cd apps/api
go run ./cmd/server          # listens on :8080 (override with API_ADDR)
```

From the repo root: `pnpm dev:api` (API) and `pnpm dev:web` (frontend) in two
terminals. The frontend calls same-origin `/api/*`, proxied to `:8080`.

Build a binary: `pnpm build:api` → `bin/api`.

Environment:

- `API_ADDR` — listen address (default `:8080`).
- `ENV=production` — switch the logger from pretty text to JSON.

## Architecture

The structure mirrors the old TS platform 1:1, so the mapping is mechanical:

```
internal/
  domain/        canonical model: ids, enums, entities, events, errors, primitives
    validate/    the zod replacement — Parse* functions that validate + coerce
  platform/      storage-agnostic core: clock, logger, event bus, repositories,
                 in-memory store, seed, cursor pagination
  services/      use-cases (workflow, agent, run, log, browser, notification,
                 activity, stats) + Platform assembly (NewPlatform)
  handler/       HTTP layer: thin handlers, error envelope, SSE, chi router
cmd/server/      entry point
```

- **Storage-agnostic.** Services depend on repository interfaces, not the
  concrete store. The in-memory store (seeded with the canonical dataset) can be
  swapped for Postgres by replacing `NewMemoryRepositories` in `NewPlatform`.
- **Events.** An in-process event bus with a ring buffer backs the two SSE
  streams; a reconnecting client replays via `Last-Event-ID`.
- **Validation.** `internal/domain/validate` returns a `*ValidationError` that
  the HTTP layer renders as `{ error: { code: "validation_failed", details } }`.
- **Concurrency.** The in-memory store and event bus are mutex-protected; the
  server is safe under concurrent requests (`go test -race` passes).

## Endpoints

Base path `/api`. Success returns the entity / `Page<T>`; errors return
`{ error: { code, message, details? } }` with a mapped HTTP status.

| Method | Path |
|--------|------|
| GET  | `/api/health` |
| GET  | `/api/dashboard` |
| GET·POST | `/api/workflows` |
| GET  | `/api/workflows/{slug}` |
| GET·POST | `/api/workflows/{slug}/runs` |
| GET  | `/api/runs` |
| GET  | `/api/runs/{id}` |
| POST | `/api/runs/{id}/cancel` |
| GET  | `/api/runs/{id}/logs` |
| GET  | `/api/runs/{id}/logs/stream` (SSE) |
| GET  | `/api/runs/{id}/browser` |
| GET·POST | `/api/agents` |
| GET  | `/api/notifications` |
| POST | `/api/notifications/{id}/read` · `/api/notifications/read-all` |
| GET  | `/api/activity` |
| GET  | `/api/events/stream` (SSE, supports `Last-Event-ID`) |

## Test

```bash
go test -race ./...
go vet ./...
```
