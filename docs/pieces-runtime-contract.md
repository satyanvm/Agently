# Activepieces runtime contract

`pieces.<piece-slug>.<action-or-trigger>` nodes execute as Temporal activities on
the Node worker in `apps/pieces-worker`. Agently loads Activepieces packages as
libraries; it does not run the Activepieces flow engine.

## Generated catalog and embeddings

`npm run gen:index` writes `packages/nodes/pieces/index.json`. Each entry records
the package, version, action/trigger name, props, auth shape, categories, and
search text consumed by the API planner and web builder.

`npm run gen:embeddings` writes `embeddings.json` and `embeddings.bin` using
Voyage (`voyage-4`, 1024 dimensions by default). Documents use
`input_type=document`; planner queries use `input_type=query`. The binary contains
unit-normalized little-endian float32 rows in manifest id order.

The embeddings sidecar is required. A missing `VOYAGE_API_KEY`, missing or stale
sidecar, model/dimension mismatch, malformed response, or provider failure aborts
planner startup/compilation with the reason.

## Temporal topology

- Queue: `PIECES_TASK_QUEUE`, default `agently-pieces`.
- Activity: `execute_piece`.
- Reasoner queue: `TEMPORAL_TASK_QUEUE`, default `agently-reasoner`.
- Piece schedule-to-start timeout: 30 seconds.
- Piece start-to-close timeout: 180 seconds.
- Retry policy: at most three attempts for infrastructure failures.

If no pieces worker polls the queue, the piece node and run fail with an
actionable worker-unavailable error. The workflow never records intent and
continues as if the integration ran.

## Payload boundary

The reasoner sends package/action metadata, fully rendered props, the credential
row id, and the credential environment variable name. It never sends secrets in
the Temporal payload because event history is durable.

```json
{
  "piece": "@activepieces/piece-slack",
  "pieceVersion": "0.11.4",
  "action": "send_channel_message",
  "props": { "channel": "C123", "text": "hello" },
  "credentialId": "cred_123",
  "authEnvKey": "AP_SLACK_AUTH"
}
```

The pieces worker resolves `credentialId` from Postgres using `DATABASE_URL`.
`DATABASE_URL` is mandatory for credentials and trigger state. Auth-less pieces
may omit both credential fields. Required credentials that are absent or invalid
return `MissingCredential`; the reasoner marks the node failed.

Business failures return `{ok:false,error,errorType}` so the reasoner can persist
the exact cause. Infrastructure failures throw so Temporal can apply retries.

## HTTP surface and triggers

The worker also serves `PIECES_HTTP_PORT` (default `7391`):

- `POST /options` resolves dynamic property options.
- `POST /run-trigger` executes webhook or polling trigger logic.
- `POST /trigger-lifecycle` invokes enable/disable hooks.

Trigger cursors and framework store values live in `piece_trigger_state`
(migration `0012`) keyed by workflow, node, and key. They are never kept only in
process memory. Missing credentials, worker outages, provider failures, and
invalid payloads are returned as failures; raw webhook data is not substituted
for an unexecuted trigger.
