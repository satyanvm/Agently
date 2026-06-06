# @agently/contracts

The canonical, framework-agnostic vocabulary of the platform. One source of
truth that the web app, services, worker, and any future standalone API all
import. Zero runtime dependencies beyond `zod`.

## What's inside

| Module | Exports |
|---|---|
| `ids.ts` | Branded, prefixed id types (`WorkflowId`, `RunId`, …) + `ids.*()` minters |
| `enums.ts` | Closed vocabularies (`RunStatus`, `AgentStatus`, `LogChannel`, …) — mirror the frontend |
| `primitives.ts` | `Timestamp`, `Money`, `Usage`, `AgentMetrics`, `Page<T>`, `PageQuery` |
| `errors.ts` | `ApiError` envelope, `ErrorCode`, `ERROR_STATUS`, throwable `DomainError` |
| `entities.ts` | Zod schemas + inferred types for every entity (Workflow, Run, RunAgent, LogEntry, BrowserSession, Notification, …) |
| `events.ts` | `DomainEvent` discriminated union — the realtime/notification spine |
| `api.ts` | Request DTOs + the typed endpoint `contract` (method/path/query/body/response) |

## Design choices

- **Zod is the single source of truth.** Types are `z.infer`'d from schemas, so
  runtime validation and compile-time types can never drift.
- **Branded ids.** `RunId` is not assignable to `WorkflowId`, even though both
  are strings — caught at compile time, costs nothing at runtime.
- **Enums mirror the frontend** (`apps/web/lib/types.ts`) exactly, so the API and
  UI never disagree about a status or channel value.
- **No execution concerns.** Nothing here knows about runners, orchestrators,
  browsers, or providers — which is exactly why it survives those decisions.

## Usage

```ts
import { Run, LaunchRunInput, contract, ids, DomainError } from "@agently/contracts";

const input = LaunchRunInput.parse(await req.json()); // validate at the boundary
const run = Run.parse(row);                            // validate at the storage edge
const id = ids.run();                                  // mint a new RunId
```
