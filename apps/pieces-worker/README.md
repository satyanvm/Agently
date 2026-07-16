# pieces-worker

A Node **Temporal activity worker** that executes [Activepieces](https://github.com/activepieces/activepieces)
piece actions **as a library** — the third execution plane of Agently's single-orchestrator
architecture (see `docs/pieces-runtime-contract.md`, the binding contract for
everything here).

```
DynamicWorkflow (Python reasoner, the ONLY orchestrator)
    └─ pieces.* node → execute_piece activity on task queue `agently-pieces`
           └─ THIS worker: piece.getAction(name).run(minimal ActionContext)
```

There is no Activepieces server, engine or flow-worker involved: piece npm
packages are imported directly and their `run()` is invoked with a
self-contained context (in-memory store, tmpdir files, no platform services).
The vendored monorepo under `external/activepieces` is reference-only.

## Install & build

```bash
cd apps/pieces-worker
npm install                 # framework + Temporal SDK + the piece packages
npm run build
```

Piece packages are plain npm deps, e.g. `@activepieces/piece-slack`. Add one:

```bash
npm install @activepieces/piece-github
npm run build && npm run gen:index   # regenerate the planner-facing index
```

## Generate the piece index

```bash
npm run gen:index    # writes packages/nodes/pieces/index.json
```

The index (contract §2) is what makes pieces plannable: the Go compiler merges
it into the node catalog as `pieces.<slug>` clusters, and the Python reasoner
uses it to render props / gate credentials before calling this worker.
Regenerate and commit it whenever piece packages change.

## Run

```bash
npm start
```

| env var | default | meaning |
|---|---|---|
| `TEMPORAL_HOSTPORT` | `localhost:7233` | Temporal frontend (same var as the reasoner) |
| `TEMPORAL_NAMESPACE` | `default` | Temporal namespace |
| `PIECES_TASK_QUEUE` | `agently-pieces` | queue this worker polls; must match the reasoner's `PIECES_TASK_QUEUE` |
| `AP_<SLUG>_AUTH` | — | credential per piece, e.g. `AP_SLACK_AUTH`, `AP_GOOGLE_SHEETS_AUTH` (slug upper-cased, `-`→`_`) |

Credentials resolve **on this worker** by env-var name — secrets never ride
Temporal payloads. Value format per auth type:

- `oauth2` — an access token string (or full connection JSON `{"access_token": ...}`)
- `secret_text` — the API key string
- `basic_auth` — `user:pass` (or JSON `{"username": ..., "password": ...}`)
- `custom_auth` — JSON object with the piece's declared fields

## Error semantics (contract §4)

- Piece-level failures (API errors, bad props, missing credential, unsupported
  platform feature) are **returned** as `{ok:false, error, errorType}` — the
  reasoner records them on the node without failing the run, and Temporal does
  not retry business errors.
- Unknown piece/action **throws** — a retryable infra error meaning this worker
  is missing a package the planner knows about (fix: install it, regen index,
  redeploy).

## Test

```bash
npm test    # node:test against a fake registry; no Temporal, no live APIs
```
