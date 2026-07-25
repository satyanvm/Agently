# Credentials & full-catalog contract

Shared contract between the node/backend workstream and the web UI workstream.
Everything here is load-bearing for both sides. If something is impossible as
specified, stop and report instead of improvising.

Goal: every catalog node (~193 hand-written/imported) and every Activepieces
community piece action (`pieces.<slug>.<action>`) is (a) present in the web
palette, (b) flagged when it needs credentials, (c) configurable via a DB-backed
credential store with an n8n-style UI, and (d) runnable end-to-end with those
credentials.

## 1. Field control vocabulary

One shared vocabulary for BOTH node config fields and credential fields. A
single dynamic renderer in the web app maps `control` → component:

| control     | component                                | notes |
|-------------|------------------------------------------|-------|
| `text`      | single-line input                        | renderer MAY infer `type=email`/`type=url` when key/label matches /email/i, /url|link/i |
| `secret`    | password input with show/hide toggle     | never echoed back from the API |
| `textarea`  | multi-line input                         | |
| `number`    | numeric input                            | |
| `checkbox`  | checkbox                                 | value is boolean |
| `select`    | select with `options: string[]`          | |
| `json`      | textarea, monospace, JSON-validated      | value stored as string |

Field shape (both config and credential fields):

```json
{ "key": "botToken", "label": "Bot Token", "control": "secret",
  "required": true, "placeholder": "xoxb-…", "help": "…",
  "options": ["a","b"], "dynamic": false }
```

`dynamic: true` marks Activepieces dropdown/dynamic props that normally need a
live authenticated fetch — rendered as plain `text` with help text telling the
user to enter the raw ID (n8n expression-style fallback). `options` only for
`select`.

Activepieces prop type → control mapping (used by the index/catalog generator
AND by the credential-field derivation):

```
short_text → text          long_text → textarea       number → number
checkbox → checkbox        static_dropdown → select   secret_text → secret
json | object | array → json                          date_time → text (ISO help)
file → text (URL help)     markdown → OMIT (display-only)
dropdown | multi_select_dropdown | static_multi_select_dropdown | dynamic
  → text with dynamic:true (raw-ID fallback)
```

## 2. Generated web catalog — `apps/web/components/builder/integration-catalog.generated.json`

Written ONLY by `packages/nodes/build-web.mjs` (backend workstream). Now merges
the hand-written catalogs AND `packages/nodes/pieces/index.json`. Per-node shape
(existing fields unchanged, one addition):

```json
{
  "id": "slack.replyInThread",
  "label": "Slack: Reply in Thread",
  "description": "…",
  "kind": "action",
  "runtime": "http",
  "cluster": "communication",
  "clusterLabel": "Communication",
  "config": [ /* fields, §1 shape */ ],
  "credentials": ["SLACK_BOT_TOKEN"],
  "credentialType": "slack"
}
```

- `credentialType`: string id into the credential-types catalog (§3), or
  omitted/null when the node needs no credentials. The web UI treats a node
  with a non-null `credentialType` as "needs credentials".
- Pieces nodes appear with `id: "pieces.<slug>.<action>"`,
  `runtime: "pieces"`, `cluster` derived from the piece's categories (fallback
  `"pieces"`), `config` mapped from props via §1, `credentials: []`, and
  `credentialType: "pieces.<slug>"` (or null when the piece has no auth).
- The web app must not hand-edit this file and must tolerate nodes without
  `credentialType` (legacy shape) at runtime.

## 3. Credential types — `apps/web/components/builder/credential-types.generated.json`

Also written ONLY by `build-web.mjs`. Map of credential type id → definition:

```json
{
  "slack": {
    "id": "slack",
    "label": "Slack",
    "source": "catalog",
    "fields": [
      { "key": "SLACK_BOT_TOKEN", "label": "Slack Bot Token",
        "control": "secret", "required": true,
        "help": "Bot token (xoxb-…) with chat:write scope" }
    ]
  },
  "pieces.github": {
    "id": "pieces.github",
    "label": "GitHub",
    "source": "pieces",
    "authType": "oauth2",
    "fields": [ … ]
  }
}
```

Derivation rules (deterministic — generator and any stub must agree):

- **Hand-written/imported nodes**: credential type id = first segment of the
  node id (`slack.replyInThread` → `slack`), created only for nodes with a
  non-empty `credentials` array. `label` = the catalog group's `label`.
  `fields` = union of that provider's credential env keys across all its nodes;
  each field: `key` = the env key verbatim, `label` = title-cased words of the
  env key (`SLACK_BOT_TOKEN` → `Slack Bot Token`), `control: "secret"`,
  `required: true`, `help` = the catalog `credentials[].help`.
- **Pieces**: id = `pieces.<slug>`, `label` = piece displayName, fields by
  `auth.type`:
  - `secret_text` → `[{ key: "value", label: <auth displayName or "API Key">, control: "secret", required: true }]`
  - `basic_auth` → `username` (text) + `password` (secret)
  - `oauth2` → `access_token` (secret, required) + `refresh_token` (secret,
    optional) — manual token entry for now, no OAuth dance
  - `custom_auth` → one field per auth prop, mapped via §1
  - no auth → no credential type; node's `credentialType` is null

## 4. Credentials REST API (Go, `apps/api`)

Workspace-scoped like the existing `integrations` endpoints. Secret values are
WRITE-ONLY: no endpoint ever returns stored values, only which keys are set.

```
GET    /api/credentials
  → 200 [ { "id": "...", "name": "My Slack bot", "type": "slack",
            "setKeys": ["SLACK_BOT_TOKEN"],
            "createdAt": "...", "updatedAt": "..." } ]

POST   /api/credentials
  body { "name": "My Slack bot", "type": "slack",
         "values": { "SLACK_BOT_TOKEN": "xoxb-..." } }
  → 201 with the summary shape above

PUT    /api/credentials/{id}
  body { "name"?: "...", "values"?: { ... } }   // values merge per-key;
  → 200 summary                                 // absent keys are preserved

DELETE /api/credentials/{id}
  → 204
```

Validation: `type` must be a known credential type id (the API loads the same
generated types file / index); required fields must be present on create.
Errors follow the existing API error envelope.

## 5. Storage

Migration `packages/db/migrations/0011_credentials.sql`:

```sql
create table if not exists credentials (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  type          text not null,                -- credential type id (§3)
  name          text not null,
  data          jsonb not null default '{}'::jsonb,  -- secret values; MVP plaintext, TODO encrypt at rest
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);
```

## 6. Selection on the canvas — reserved config key `__credentialId`

The chosen credential for a node is stored in the node's existing `config`
object under the reserved key `__credentialId` (string credential id). This
flows through all existing workflow-JSON plumbing untouched.

- The inspector must NEVER render `__credentialId` as a normal field, and the
  "n/m configured" count must exclude it.
- A node with non-null `credentialType` and no/dangling `__credentialId` shows
  the needs-credentials badge.
- The runtime strips `__credentialId` from config before rendering props.

## 7. Runtime resolution (reasoner + pieces-worker)

- **http/builtin nodes**: the reasoner resolves `config.__credentialId` → the
  `credentials.data` jsonb from Postgres (inside an activity, never in
  workflow code) and uses those key/values for `{{credentials.KEY}}` template
  rendering. Fallback order per key: credential row → process env (today's
  behavior). Missing → existing record-intent degradation.
- **pieces nodes**: the `execute_piece` payload (docs/pieces-runtime-contract.md
  §4) gains `"credentialId": "<id>" | null` alongside the existing
  `authEnvKey`. Secrets still never cross the Temporal payload boundary: the
  NODE WORKER resolves the credential — by id from Postgres when set, else
  `process.env[authEnvKey]`. DB-sourced values are normalized to the piece's
  auth shape: `secret_text` → `data.value` string; `basic_auth` →
  `{ username, password }`; `custom_auth` → data object as-is; `oauth2` →
  `{ access_token: data.access_token, ...data }`. Missing both →
  `{ ok:false, errorType:"MissingCredential" }` (returned, not thrown).
- The pieces-worker reads the same Postgres DSN env the reasoner uses (see
  `apps/reasoner/reasoner/config.py`).

## 8. Ownership boundaries

- **Backend/nodes workstream**: `packages/nodes/**`, `apps/pieces-worker/**`,
  `apps/api/**`, `apps/reasoner/**`, `packages/db/migrations/**`, and the two
  `*.generated.json` files under `apps/web/components/builder/` (via
  `build-web.mjs` only).
- **Web workstream**: everything else under `apps/web/**`; never writes the
  generated JSON files; consumes shapes in §§1–4, 6 verbatim.
