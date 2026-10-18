# Agently

**Intelligence tools tell you what happened. Agently does something about it.**

Agently does competitor intelligence, marketing intelligence and marketing
automation as one loop:

```text
detect a market signal → decide what it means → draft the marketing response
        → a person approves → publish → record what happened
```

A competitor changes its pricing page. Agently notices the change and works out
what it means for you. It then drafts an updated battlecard, a comparison-page
edit, a note to at-risk customers and a talk track for sales. Each draft waits
in an approval inbox until someone on the team edits, approves or rejects it.
Only then does it publish, and the published item links back to the signal that
caused it.

## Who it is for

B2B software companies with one to five marketers and no budget for enterprise
intelligence suites. They need to watch competitors and react quickly, but
can't pay for one tool that monitors and a second that executes.

## How it works

- **Signals we collect ourselves.** Agently doesn't resell a paid intelligence
  feed. It reads competitor websites (pricing, changelog, blog and careers
  pages), GDELT news, RSS, Reddit, Hacker News, review sites, and any page
  reachable over plain HTTP or a real Browserbase browser.
- **Playbooks in plain English.** Describe what should happen ("when a
  competitor launches anything AI-related, draft a LinkedIn post in our CEO's
  voice and send it to Rahul to approve"). The planner compiles it into a typed
  workflow graph you can inspect and edit. You can also start from a ready-made
  playbook.
- **AI drafts, a person approves.** Nothing publishes without human approval.
  The only exception is a small set of explicitly safe actions, such as pausing
  scheduled posts during a brand crisis. Drafts use the workspace's brand
  voice, product facts and claims to avoid, so they don't come out generic.
- **Integrations.** Drafts publish through 700+ MIT-licensed Activepieces
  integrations plus hand-written nodes: LinkedIn, X, WordPress, Webflow,
  HubSpot, Buffer, Slack, Notion, Google Docs, Gmail and more.

## Why durability and traceability matter

A playbook can wait days for an approver. Workers restart, deploys happen and
laptops sleep in the meantime, and the run has to resume exactly where it
stopped. Every run is a Temporal workflow with one activity per node, so a
crashed worker resumes at the node it was on, and a run waiting on a Temporal signal holds
no worker at all.

Every published item has to answer five questions: which signal caused it,
which run drafted it, what it cost, who approved it and when it went out.
Agently records each node's input and output, run logs, per-run model cost, and
a Langfuse trace of every model call. The approval record ties all of it
together.

## Status

Built and tested end to end on a local stack:
- **Signals:** competitors and their pages, page snapshots and diffs, and
  signals from news, RSS, Reddit and Hacker News.
- **Approval:** the durable approval step, with Slack and email notifications
  (tested against a stand-in webhook so far, not real Slack or email).
- **Web app:** the Inbox (edit, approve, reject, full history), plus the
  Competitors and Brand pages.
- **Drafting:** brand-aware drafts.
- **Scheduling:** weekly and cron schedules.
- **Approval rule:** no publishing step without an approval before it,
  enforced when planning, saving and running.
- **Playbooks:** three ready-made ones (competitor pricing change, competitor
  feature launch, weekly content plan).

**Not yet tested against real accounts:** publishing to Google Docs, Gmail,
LinkedIn and X. Every integration loads and its sign-in is ready; see
`docs/integrations-status.md` for what each needs. `DEMO.md` walks through
the whole loop. `product.md` has the scope and `docs/product-direction.md`
the reasoning and competitors.

## Architecture

The API and web app form the control plane. Temporal, the Python reasoner and
the Node pieces worker form the execution plane.

```text
Next.js web -> Go API -> Postgres
                           |
                    queued runs
                           v
               Python reasoner dispatcher
                           |
                 Temporal workflow history
                           |
               one activity per DAG node
                  /          |          \
              Gemini     Browserbase   pieces worker
```

- `apps/api`: workflow planner, CRUD, schedules, triggers, logs, credentials, and
  the web API.
- `apps/reasoner`: the only run executor. It walks DAGs in deterministic Temporal
  workflow code and persists each node result as an activity checkpoint.
- `apps/pieces-worker`: executes `pieces.*` actions/triggers and stores durable
  trigger state.
- `apps/web`: live UI backed only by API data.
- `packages/nodes`: hand-written and generated integration catalogs.
- `packages/db/migrations`: the Postgres schema and upgrades.

The retired native Go execution worker and the `runs.engine` selector have been
removed. Migration `0013_drop_retired_execution_columns.sql` also removes its
lease columns.

## Strict failure model

Agently does not manufacture successful-looking work:

- Planner router/map/reduce failures return the provider error.
- Missing Gemini or Moonshot (Kimi) credentials prevent planning.
- Missing or stale piece embeddings prevent API startup.
- Missing Browserbase, Langfuse, SMTP, database, or integration credentials fail
  startup or the selected node.
- An unavailable pieces worker fails piece nodes.
- The web app does not synthesize costs, logs, browser sessions, or activity data.

## Models

The planner uses `PLANNER_MAP_MODEL` (default `gemini-3.1-flash-lite`) for
routing and map selection, and `PLANNER_MODEL` for graph creation and repair
(code default `kimi-k3` via the Moonshot API; `.env.example` uses
`gemini-3.5-flash` so a single Gemini key runs everything). The provider is
inferred from the model name, and any tier can run locally as `ollama/<model>`
(e.g. `ollama/qwen3:8b`): no keys or quotas, slower on a laptop. `PLANNER_EMBED_MODEL` provides catalog recall —
`ollama/nomic-embed-text` locally, or `gemini-embedding-001` /
`text-embedding-3-small`. The reasoner uses `REASONER_MODEL` and
`REASONER_SYNTHESIS_MODEL` (default `gemini-3.5-flash-lite`). The reduce model
sizes each graph: large ones (80+ nodes) come back as an outline and are written
one section per reduce call; the `PLANNER_*` limits in `.env.example` tune this. Gemini 429s are
retried after the server-suggested delay, so free-tier keys work, just slower.

## Local development

```bash
cp .env.example .env
# Fill every required credential.
pnpm install
pnpm agently
```

Open the web app at `http://localhost:3000` and Temporal UI at
`http://localhost:8080`. See `RUNBOOK.md` for setup, migrations, service restart,
and verification commands, and its "Deploying" section for running the stack in
containers behind an access token and password.
