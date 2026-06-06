# Agently — Backend Foundations

This document describes the backend foundations built to support the existing
frontend. The frontend is unchanged and still runs entirely on its own mock data
(`apps/web/lib/mock-data.ts`); everything here runs **alongside** it, ready to be
wired in later with minimal refactoring.

The guiding rule: build only what the *current frontend obviously requires* and
what is *likely to survive* future decisions about the agent framework,
orchestration, browser infrastructure, and cloud provisioning. None of those are
assumed anywhere below.

---

## 1. What was built

| Layer | Package / location | What it is |
|---|---|---|
| **Contracts** | `packages/contracts` | Canonical domain model: branded ids, enums, zod entity schemas (+ inferred types), domain events, error envelope, pagination, and the typed HTTP contract. |
| **Application core** | `packages/core/src/platform` | Storage-agnostic platform: repository interfaces + in-memory implementation, a typed event bus, a service layer (use-cases), a structured logger, a clock, and the canonical seed. Assembled by `createPlatform()`. |
| **Mock backend** | `apps/web/app/api/**` | 18 Next.js route handlers implementing the contract over the platform, including two SSE streams. Plus a server singleton and HTTP helpers. |
| **Database** | `packages/db/migrations` | SQL schema (init, RLS, Postgres-as-queue, realtime) mirroring the contracts model 1:1. |
| **Docs** | this file + package READMEs | Architecture, rationale, and the integration guide. |

### Entities identified (from the actual UI)

Workspace, Member, ApiKey · AgentDefinition · Workflow, WorkflowVersion (agent
graph) · Run, RunAgent, AgentMessage, Artifact · LogEntry · BrowserSession,
BrowserAction, BrowserShot, BrowserConsoleLine · Notification · ActivityEvent ·
WorkspaceStats / WorkflowStats.

### User actions identified (from the actual UI)

Launch a run · cancel a run · re-run · create workflow · create agent · list /
filter / paginate workflows, runs, agents, notifications · view a run with its
agent graph, messages, artifacts · stream + search + filter logs · view a
browser session (timeline, actions, console) · mark notifications read · read
dashboard aggregates and the activity feed.

---

## 2. Why each piece survives future architecture decisions

The test applied to every addition: *would a decision to use CrewAI vs LangGraph,
Browserbase vs Playwright, or AWS vs Fly change this?* If yes, it wasn't built.

- **Contracts (entities, enums, ids).** These are *product* concepts — what a
  run, an agent node, a log line, a notification *are*. How they're produced is
  irrelevant to their shape. Mirrors the frontend exactly, so the API and UI can
  never disagree.
- **Validation schemas (zod).** Validating data at the boundary is required
  regardless of what's behind it. Schemas are the single source of truth; types
  are inferred, so they can't drift.
- **API contract.** The interface the UI talks to. Implementations change; the
  request/response shapes the screens need do not.
- **Domain events.** Everything the UI shows "live" is a *fact that happened*
  (run started, log appended, agent transitioned, cost threshold crossed).
  Modeling facts as events is transport-, store-, and runner-independent;
  notifications, the activity feed, the log stream, audit, and webhooks are all
  projections of this one stream.
- **Repository interfaces.** Services depend on storage *interfaces*, never a
  concrete store. The in-memory implementation can be swapped for Postgres
  without touching a service or route.
- **Service layer.** Use-cases (launch, cancel, append log, transition agent,
  mark read) are the durable business logic. A real runner/worker will call the
  same lifecycle methods (`runs.start/progress/transitionAgent/finish`,
  `logs.append`, `browser.recordAction`) the mock backend exercises now.
- **Event bus interface + SSE scaffolding.** Realtime is required by the log
  viewer and live dashboard. The bus is an interface (in-memory today; Postgres
  `LISTEN/NOTIFY`, Supabase Realtime, Redis, or Kafka later) and the SSE routes
  are framed so swapping transport touches one file.
- **SQL schema.** The data model is stable even as execution changes. Table per
  entity, snake_case, prefixed text PKs — a direct map from the contracts types.
- **Structured logger + clock.** Cross-cutting primitives every backend needs;
  injected, so they're swappable and testable.

---

## 3. Project structure (new + changed)

```
packages/
  contracts/                     ← NEW package @agently/contracts
    src/{ids,enums,primitives,errors,entities,events,api,index}.ts
  core/
    src/platform/                ← NEW: the storage-agnostic application core
      index.ts                   createPlatform() factory
      clock.ts  logger.ts  util.ts
      events/bus.ts              typed EventBus (+ replay buffer)
      repositories/{types,memory}.ts
      services/{workflow,agent,run,log,browser,notification,activity,stats}Service.ts
      seed.ts                    canonical dataset (mirrors the UI's mock data)
    src/types.ts, services/, runners/, queue/, db/   ← unchanged Phase-0 scaffold
  db/migrations/                 ← NEW: 0001_init … 0004_realtime + README
apps/web/
  app/api/**                     ← NEW: 18 route handlers (mock backend) + SSE
  lib/server/{platform,http}.ts  ← NEW: server singleton + HTTP helpers
  next.config.ts                 ← transpilePackages + .js→.ts extensionAlias
  (everything under app/(app)/** and lib/{types,mock-data}.ts is UNCHANGED)
```

`createPlatform()` wires it together:

```
contracts (types)  →  repositories (interface)  →  in-memory store (seed)
                          │
   logger ── clock ──  services  ── event bus  ──  SSE / notifications
                          │
                  route handlers (HTTP)  ──►  the frontend, later
```

---

## 4. API reference

Base path `/api`. Success returns the entity/`Page<T>`; errors return
`{ error: { code, message, details? } }` with a mapped HTTP status.

| Operation | Method & path |
|---|---|
| Health | `GET /api/health` |
| Dashboard stats | `GET /api/dashboard` |
| List workflows | `GET /api/workflows?cursor&limit` |
| Get workflow | `GET /api/workflows/:slug` |
| Create workflow | `POST /api/workflows` |
| Launch run | `POST /api/workflows/:slug/runs` |
| List runs for workflow | `GET /api/workflows/:slug/runs` |
| List runs | `GET /api/runs?status&workflowId&cursor&limit` |
| Get run (graph + artifacts) | `GET /api/runs/:id` |
| Cancel run | `POST /api/runs/:id/cancel` |
| Run logs (filter/paginate) | `GET /api/runs/:id/logs?severity&channel&source&q&afterSeq` |
| **Run log stream (SSE)** | `GET /api/runs/:id/logs/stream` |
| Browser session | `GET /api/runs/:id/browser` |
| List / create agents | `GET·POST /api/agents` |
| List notifications | `GET /api/notifications?unread&type` |
| Mark read / read-all | `POST /api/notifications/:id/read` · `POST /api/notifications/read-all` |
| Activity feed | `GET /api/activity` |
| **Workspace event stream (SSE)** | `GET /api/events/stream` (supports `Last-Event-ID` replay) |

Try it:

```bash
curl localhost:3000/api/runs/run_8842 | jq '.number, .status, (.agents|length)'
curl -X POST localhost:3000/api/workflows/competitive-intelligence-sweep/runs \
  -H 'content-type: application/json' -d '{"trigger":"manual"}'
curl -N localhost:3000/api/events/stream      # live domain events
```

---

## 5. Integrating the frontend later (minimal refactor)

The screens currently import mock data directly, e.g.:

```ts
import { runs, getRun } from "@/lib/mock-data";
```

To switch a screen to the API with no UI change, introduce a typed client that
returns the **same shapes** (the contract types already match the UI's intent),
then swap the import:

```ts
// lib/api-client.ts (future)
import type { RunDetail, Page, Run } from "@agently/contracts";
export const api = {
  runs: {
    list: (q = "") => fetch(`/api/runs?${q}`).then(r => r.json()) as Promise<Page<Run>>,
    get:  (id: string) => fetch(`/api/runs/${id}`).then(r => r.json()) as Promise<RunDetail>,
  },
  // …
};
```

Because the canonical types mirror the frontend's existing `lib/types.ts`, the
mapping is mechanical (mostly field-name parity; a few read-models like
`WorkflowSummary`/`RunDetail` already bundle exactly what the detail screens
render). The log viewer can drop its simulated stream and subscribe to
`/api/runs/:id/logs/stream` — same `LogEntry` shape, pushed live.

Swapping the in-memory store for Postgres happens entirely inside
`createPlatform()` + a new `Repositories` implementation; services, routes, the
worker, and the frontend are untouched.

---

## 6. Verification

- `pnpm --filter @agently/contracts typecheck` — clean
- `pnpm --filter @agently/core typecheck` — clean
- `pnpm --filter @agently/web typecheck` + `build` — clean (18 API routes, all
  existing pages unchanged)
- Runtime: every read endpoint returns correct seeded data; launch→cancel works;
  validation returns `422` with field details; missing ids return `404`; the SSE
  stream emits `run.queued → run.started → run.log.appended → run.finished` live.
- The frontend still renders its own mock data exactly as before.
