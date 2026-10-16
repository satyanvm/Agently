# Credentials contract

The builder, API, reasoner, and pieces worker share one credential model.
Generated credential types live in
`apps/web/components/builder/credential-types.generated.json`; node definitions
refer to them through `credentialType`.

## Storage and API

Migration `0011_credentials.sql` creates workspace-scoped credential rows. Secret
values are write-only through the API; responses expose only the set key names.
With `AGENTLY_CREDENTIALS_KEY` set (required in production) the API stores `data`
as `{"$enc": "v1:<base64(nonce || ciphertext)>"}`, sealed with AES-256-GCM; the
reasoner and pieces worker open it with the same key
(`apps/api/internal/platform/secretbox.go`).

```text
GET    /api/credentials
POST   /api/credentials
PUT    /api/credentials/{id}
DELETE /api/credentials/{id}
```

The selected credential id is stored in node config as `__credentialId`. The web
inspector handles that reserved field separately and the runtime removes it before
rendering normal node props.

## Runtime resolution

- Built-in and catalog nodes resolve the credential row from Postgres inside the
  activity, then expose its values to `{{credentials.KEY}}` templates.
- Piece nodes send only the credential id over Temporal. The Node worker reads the
  row from the same `DATABASE_URL` and normalizes it for the piece auth type.
- OAuth tokens, API keys, passwords, and custom auth objects never appear in
  Temporal workflow payloads or API read responses.

A dangling credential id, missing required value, database error, or malformed
auth object fails the node with the exact reason. There is no record-intent or
environment-only success path for a credential that could not be resolved.

## Generated field controls

Credential and node config fields use `text`, `secret`, `textarea`, `number`,
`checkbox`, `select`, and `json`. Dynamic Activepieces props are rendered as raw
value/id inputs when the provider requires an authenticated lookup; `/options`
supplies choices when available.

Generated catalog files are produced by `packages/nodes/build-web.mjs` and are not
hand-edited.

## Run preflight

Before a run starts, the API applies the runtime's resolution rules to the
workflow's current graph without executing anything
(`apps/api/internal/services/preflight.go`):

- `GET /api/workflows/{slug}/preflight` returns `{ready, accounts[]}`. Each
  account (`credentialType`, `label`, `authType`, `envKeys`) lists the nodes
  that cannot run, each with a `problem`: `not_connected`,
  `credential_deleted` or `incomplete` (plus `missingKeys`). An account with
  an empty `credentialType` is a server env var (for example `TOOL_DB_URL`).
- `POST /api/workflows/{slug}/credentials` with `{credentialId}` selects that
  credential on every node of its type that cannot run, saving a new graph
  version, and returns the new preflight.
- A manual launch that fails the preflight gets 409 `credentials_required`,
  with one detail per node; `skipCredentialCheck: true` in the launch body
  overrides it. Scheduled and webhook launches record the run as failed with
  the same reason instead. `RUN_CREDENTIAL_CHECK=off` disables the check.

