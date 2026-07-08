# packages/nodes — the integration node catalog

Single source of truth for every node the platform knows, consumed by three planes:

- **Go API** (`apps/api/internal/services/nodecatalog.go`) — the map-reduce prompt
  compiler: the *map* phase reads each cluster's compact index with a small fast
  model; the *reduce* phase hands only the selected nodes' full schemas to the big
  model that authors the graph.
- **Python reasoner** (`apps/reasoner/reasoner/catalog.py`) — the generic
  integration executor: any GraphNode whose `type` matches a catalog id executes
  via the def's declared runtime (`http`/`browser`/`code`/`llm`).
- **Web builder** (`apps/web/components/builder/node-catalog.ts`) — palette +
  inspector forms, grouped by cluster.

## Why data, not code

n8n-style breadth (hundreds of integrations) is only sustainable if a node is a
JSON definition compiling onto a small set of generic runtimes — not a bespoke
handler. Adding an integration = adding an entry to a cluster file. No code.

## Files

- `catalog/<cluster>.json` — one file per cluster, `{ "cluster": ..., "nodes": [...] }`.
- The 15 built-in types (`trigger.*`, `agent.*`, `tool.*`, `logic.*`, `output.*`)
  stay code-backed in the reasoner (`nodes.py`); the catalog's `builtin.json`
  mirrors them so the planner sees one uniform universe.

## Node definition schema

```jsonc
{
  "id": "slack.sendMessage",        // GraphNode.Type. "<service>.<verb>" camelCase.
  "label": "Slack: Send Message",
  "description": "Post a message to a Slack channel via the Web API.",
  "kind": "action",                 // trigger | action | logic | output
  "runtime": "http",                // http | browser | code | llm | builtin
  "search": "slack chat message notify channel post",  // map-phase index line
  "config": [                       // inspector form + what the LLM must fill
    { "key": "channel", "label": "Channel", "control": "text", "required": true,
      "placeholder": "#general", "help": "Channel name or ID" },
    { "key": "text", "label": "Message", "control": "textarea", "required": true }
  ],
  "outputs": ["ok", "ts", "channel"],  // fields downstream nodes may reference
  "credentials": [                  // env vars resolved at execution time
    { "key": "SLACK_BOT_TOKEN", "help": "Bot token (xoxb-…) with chat:write" }
  ],
  "http": {                         // runtime="http" only. Templates support
    "method": "POST",               // {{config.x}}, {{credentials.X}},
    "url": "https://slack.com/api/chat.postMessage",   // {{input.x}}, {{outputs.k.f}}
    "headers": { "Authorization": "Bearer {{credentials.SLACK_BOT_TOKEN}}",
                 "Content-Type": "application/json" },
    "body": "{\"channel\": {{json config.channel}}, \"text\": {{json config.text}}}",
    "outputMap": { "ok": "ok", "ts": "ts", "channel": "channel" }  // optional: output field → JSON path in response
  }
}
```

Template helpers (inside `http` blocks): `{{json expr}}` renders the value
JSON-encoded (quoted + escaped — use for every value embedded in a JSON body);
`{{urlencode expr}}` percent-encodes (use in query strings and form bodies).
Optional `http.auth`: `{ "type": "basic", "username": "…", "password": "…" }`
(templated) for APIs using HTTP Basic auth.

Runtime notes:

- **http** — the reasoner renders the template and performs the request. The
  response body is JSON-decoded when possible; `outputMap` lifts fields into the
  node's output dict; the raw (clipped) body is always exposed as `body`, the
  status as `status`. `{{json …}}` renders a value JSON-encoded (quoted/escaped)
  for safe embedding in JSON bodies.
- **browser** — def provides `urls` (template) instead of `http`; executes via
  the existing browser stack. For JS-heavy or auth-walled surfaces.
- **code** — def provides `source` (template) run in the tool.code sandbox
  (requires `TOOL_CODE_ENABLED=1`, else record-intent). For local transforms
  (csv/json/xml/pdf/crypto/datetime).
- **llm** — def provides `system`/`prompt` templates; executes like agent.llm.

Rules of honesty:

- Only real, public, documented API endpoints. If a service has no public API
  (or it is auth-walled beyond a simple token), use `browser` or mark clearly.
- Credentials are env vars; a missing credential degrades the node to
  record-intent (logged loudly) — it never fails the run.
- Platform triggers remain manual/schedule/webhook. Service "triggers" (e.g.
  "new email received") are modeled as pollable `action` nodes.
