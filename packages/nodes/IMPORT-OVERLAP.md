# Catalog vs. imported overlap report

Status after dedup applied on 2026-07-14. `catalog/` is the live source of truth
(consumed by the Go planner, the Python reasoner, and the web palette build);
`imported/` is a staging area produced by the Activepieces→Agently transpile
pilot and is consumed by nothing yet — merging a staged file into a cluster is a
deliberate act.

## Numbers

- **catalog/**: 208 node ids across 13 clusters (193 integration + 15 builtin).
- **imported/**: 146 node ids across 29 service files (was 150 before dedup).
- **Exact-id intersection: 0** (was 4 — resolved below).
- 5 staging files are intentionally empty shells documenting why nothing was
  transpiled: `amazon-s3` (SigV4 signing inexpressible in static templates),
  `box` (piece has no concrete actions), `cal-com` (webhook-triggers only),
  `mysql`/`postgres` (DB protocols, not HTTP).

## Service-level overlap

13 services exist on both sides. All staged nodes for these services are
**additive** (new operations, no id collisions):

| service  | catalog ops | staged new ops | service  | catalog ops | staged new ops |
|----------|------------:|---------------:|----------|------------:|---------------:|
| airtable | 2           | 5              | msteams  | 1           | 8              |
| clickup  | 1           | 8              | notion   | 3           | 7              |
| discord  | 1           | 8              | slack    | 2           | 8              |
| gdrive   | 2           | 7              | stripe   | 4           | 8              |
| github   | 12          | 7              | todoist  | 2           | 4              |
| gsheets  | 2           | 7              | trello   | 2           | 5              |
| hubspot  | 3           | 8              |          |             |                |

12 services are entirely new: algolia, attio, bitly, bravesearch, figma, gmail,
jotform, salesforce, webflow, woocommerce, wordpress, zoom.

## The 4 exact-id collisions and how they were resolved

The transpiler tags an imported node with `metadata.replaces` when it is meant to
supersede a live catalog node. All 4 collisions carried that tag. Resolution:
the improved definition was merged **into catalog/** and the staged duplicate
removed, so `replaces` never needs merge-time machinery.

1. **`gdrive.getFileMetadata`** — imported version adopted wholesale: adds
   `supportsAllDrives`, an explicit `fields=` projection, `{{urlencode}}` on the
   path segment, and 4 more mapped outputs (size, webViewLink, modifiedTime,
   trashed).
2. **`github.createGist`** — merged: took the imported `X-GitHub-Api-Version`
   header, select-control for `public`, and `{{json config.public}}` (the old
   unquoted `{{config.public}}` emitted invalid JSON when blank). Kept the
   catalog's `GITHUB_TOKEN` credential and `description` config key (continuity
   with 12 other github nodes). Also fixed a latent bug: the catalog's
   `outputMap` had `"html_url": "htmlUrl"` — reversed per the schema convention
   (output field → response path), so `htmlUrl` always resolved empty. A sweep
   found no other reversed outputMaps in the catalog.
3. **`todoist.createTask`** — imported version adopted: moves from the
   deprecated `rest/v2` base to the unified `api/v1`, and drops the `dueString`
   config. Dropping it is deliberate, not a regression you can fix by keeping
   the key: the template runtime renders `{{json config.dueString}}` as `""`
   when blank (see `render_tpl` in `apps/reasoner/reasoner/nodes.py`) and
   Todoist rejects `"due_string": ""` — i.e. the old node failed whenever Due
   was left empty. Templates cannot conditionally omit body keys; a
   `createTaskWithDue` variant with a *required* due field is the clean way to
   restore the capability if wanted.
4. **`todoist.listTasks`** — imported version adopted: `api/v1` base plus a
   `results` outputMap so downstream nodes can reference the task array.

## Credential-name normalization (applied to staging)

Env-var names must be consistent per service or the same workflow would demand
two secrets for one account. Two mismatches found and fixed in `imported/`:

- `CLICKUP_ACCESS_TOKEN` → `CLICKUP_API_TOKEN` (17 occurrences; ClickUp accepts
  personal `pk_…` tokens and OAuth2 access tokens in the same header).
- `GITHUB_ACCESS_TOKEN` → `GITHUB_TOKEN` (14 occurrences; matches the 12 live
  github nodes).

All other staged services either match the live convention exactly (airtable,
gdrive, gsheets, hubspot, notion, slack, stripe, todoist, trello) or introduce
new names with no counterpart (discord `DISCORD_BOT_TOKEN`, msteams
`MSTEAMS_ACCESS_TOKEN`, and the 12 new services).

## Merge readiness

With ids disjoint and credentials aligned, any staged file can now be merged
into its target cluster by appending its `nodes` array and re-running
`node packages/nodes/build-web.mjs`. Per-file caveats (dropped params that
templates cannot express, pagination limits, needsPieceRuntime actions) live in
each staged file's `notes` field — read it before merging.
