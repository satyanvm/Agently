# Phase 2 plan: first version of the signal → approved-action loop

Status (2026-10-18): steps 1–12 are built and tested on the local stack.
Still open:
- **Live publishing:** Google Docs, Gmail, LinkedIn and X haven't been tested
  against real accounts. That waits for the sign-in apps described in
  `docs/integrations-status.md`.
- **Two choices for the user:**
  - Whether customer email should also require approval.
  - Whether a new schedule should fire right after it's created.

## Design in one page

```text
competitor pages ─► intel.snapshotPage ─► page_snapshots ─► intel.diffPage ─► signals
GDELT/RSS/Reddit/HN ─────────────────────────────────► intel.recordSignal ─► signals
                                                                          │
                       agent.llm (brand: true) ◄── brand_context ─────────┘
                                │ draft
                                ▼
                       human.approval ──► approvals row + Slack/email link
                                │  (Temporal workflow waits on a signal; no worker held)
         web Inbox: edit / approve / reject ──► API writes decision
                                │  reasoner relay sends the Temporal signal
                                ▼
                 approved → publish nodes run │ rejected/timed out → skipped
                                ▼
                       approval_outcomes (what was published, when, run, cost)
```

### Key decisions (my calls unless flagged ❓)

1. **Approval wait = a Temporal signal plus `workflow.wait_condition` with a
   timeout**, inside `DynamicWorkflow`. The Go API stays free of Temporal: it
   writes the decision to Postgres. A relay loop in the reasoner, next to the
   dispatcher, sends the signal to `agently-run-<id>` and stamps
   `signaled_at`. This is the same pattern already used for starting runs.
   Signals are idempotent: they're keyed by approval id, and duplicates are
   ignored.
2. **Approvals don't block each other.** `DynamicWorkflow` runs nodes one at a
   time, so a naive approval node would hold the second draft's notification
   until the first one is decided. Instead, an approval node *opens* its
   request and notifies immediately. The wait happens when a node that
   *depends on* it is reached. Playbook A's four drafts all land in the inbox
   at once, and each is approved on its own.
3. **The approval node is a gate**, like `logic.branch`. Approved means its
   dependents run and receive the edited content
   (`{{outputs.approve_post.content}}`). Rejected or timed out means its
   dependents are skipped. On timeout, the node's `onTimeout` setting either
   skips the dependents (the default) or fails the run.
4. **Provenance is recorded automatically, not typed by hand.** Signals carry
   `run_id`, an approval links to the signals created earlier in its run (or
   to an explicit `signalId`), and every node that succeeds downstream of an
   approval is written to `approval_outcomes`. That gives the full history:
   signal → run → draft → approver → published item, with the time and the
   run's cost. Cost is tracked per run, so drafts from the same run share it.
   The inbox will say so rather than invent a per-draft split.
5. **Snapshots:** fetch over HTTP first, honouring robots.txt, or through
   Browserbase when the node is set to `render: browser`. Before diffing,
   reduce the page to visible text and normalise it: drop scripts and styles,
   collapse whitespace, and remove date and time noise. A new row is stored
   only when the content hash changes. The first snapshot of a page is the
   baseline and produces no signal. The diff is line-level (`difflib`):
   added and removed lines plus a short summary. An AI node decides what the
   change means; the diff itself is deterministic.
6. **Brand context:** one row per workspace (voice, product facts,
   positioning, claims to avoid). It is injected into the system prompt of
   `agent.llm` nodes that have `brand: true`. The planner and the playbooks
   set that flag on drafting nodes. It is not applied everywhere, so analysis
   nodes don't pick up marketing voice.
7. **Strict failure model holds:**
   - a missing integration credential fails the node with its real reason;
   - a page that can't be fetched fails the node (or continues, if
     `continueOnFail` is set);
   - Slack or email notification is best-effort, and the inbox is the source
     of truth. A failed notification is logged as a warning on the run and
     shown on the approval, never hidden.
8. **Tables carry `workspace_id`** even though there is only one workspace
   today, so multi-user support later needs no schema change.

### Decided with the user (2026-10-18)
- **Enforcement:** the validator rejects any graph where a *public-publishing*
  node has no `human.approval` before it. That covers social, CMS/blog,
  newsletter campaigns and social schedulers. Slack messages to your own team
  and email to yourself are exempt, so existing digests keep working.
- **Approver:** the approve form has a "Your name" field, remembered per
  browser and recorded as entered. It will be replaced by real users later.
- **Test accounts:** Google (Docs + Gmail), Slack, LinkedIn and X. Buffer,
  WordPress and HubSpot won't be available, so blog posts go to a Google Doc
  draft. "Schedule" means the approved post waits on a durable Temporal timer
  and is then posted directly to LinkedIn/X. There is no third-party
  scheduler.
- **Naming:** sidebar labels are Inbox (approvals), Playbooks (templates plus
  your workflows; the UI label replaces "Workflows", while API, database and
  route names stay `workflows`), Competitors and Brand.

## Build steps (each one is a commit or two, with all four suites run after)

| # | Step | Verified by |
|---|------|-------------|
| 1 | **Migration `0015_marketing_loop.sql`**: `competitors`, `page_snapshots`, `signals`, `brand_context`, `approvals`, `approval_outcomes`; added to `EnsureSchema`; Go domain types and in-memory + Postgres repositories. | Go repository tests; migration applied to local Postgres |
| 2 | **Snapshot + diff nodes** in the reasoner: `intel.snapshotPage`, `intel.diffPage`, `intel.watchCompetitors` (all registered pages of the chosen competitors → list of changes), `intel.recordSignal` (turns GDELT/RSS/Reddit/HN items into traceable signals). Catalog entries in `builtin.json` and `node-catalog.ts`. **API**: competitors CRUD, `GET /api/signals`, `GET /api/competitors/{id}/snapshots`. | Unit tests for normalisation and diffing; a test against a local HTTP server that changes between fetches; one live run against a real public pricing page |
| 3 | **`human.approval` node**: open-request activity, signal handler, deferred wait, timeout, gate; relay loop in the dispatcher; Slack-webhook and email notification with an `APP_BASE_URL/inbox/<id>` link. **API**: `GET /api/approvals?status=`, `GET /api/approvals/{id}`, `POST /api/approvals/{id}/decision` with `{decision, content, approver, note}`. Runs show status `paused` ("Waiting for approval") while blocked. | Temporal time-skipping tests (as in `test_workflow_policy.py`): approve, edit, reject, timeout, two parallel approvals, and a worker restart while waiting; Go handler tests |
| 4 | **Traceability**: `approval_outcomes` written for nodes downstream of an approval; `GET /api/approvals/{id}/history` joins signal → run (cost, Langfuse trace) → draft → decision → outcomes. | Reasoner + Go tests |
| 5 | **Web**: Inbox (pending list with the causing signal; detail page with an editable draft, approve/reject, full history timeline), Competitors page, Brand page, sidebar entries. | `pnpm build`, then driven in the built-in browser against the local stack |
| 6 | **Brand context injection** into `agent.llm` (`brand: true`). | Reasoner unit test on the prompt; a live draft compared with and without it |
| 7 | **Schedules**: `weekly <day> HH:MM` and real 5-field cron in `scheduler.go`. This also fixes the schedule node's `cron` field, which is currently ignored. | Go `dueAt` table tests |
| 8 | **Planner**: reduce-prompt guidance for playbooks (signal → AI meaning → `brand: true` drafts → `human.approval` → publish); add `intel` and `human` to the built-in prefixes in `servicesOf`; the approval-before-publish validator rule (❓ scope). | Go validator tests |
| 9 | **Publishing integrations check**: load each candidate piece in the worker and check its props: LinkedIn, X, Buffer, Typefully, WordPress, Webflow, HubSpot blog, Google Docs, Notion, Gmail, Slack. A real publish only with the accounts you connect. Unusable ones are reported, and the closest working one is used. | A written table of works / needs app review / broken |
| 10 | **Three playbooks** as JSON graphs in `packages/nodes/playbooks/`; `GET /api/playbooks` and `POST /api/playbooks/{id}/install` (fills in the competitor, channels and approver); a "Start from a playbook" option in the new-workflow dialog. | A Go test runs every playbook through `validateGraph` + `validateRefs`; one end-to-end live run of playbook A |
| 11 | **Planner eval**: marketing goldens in `planeval` (recall for LinkedIn, Buffer, Google Docs, HubSpot…), plus a new `-graphs` mode that compiles about ten playbook prompts and checks structure (an approval upstream of every publish, `intel.*` for competitor prompts, `brand: true` on drafts). | Real eval numbers reported, misses included |
| 12 | **Docs**: DEMO.md (playbook A end to end), classmate.txt Part 11 file paths, README status. | — |

The playbooks will publish to these targets unless step 9 finds one unusable:

- **A (pricing change):** battlecard → Google Docs `append_text` (or Notion);
  comparison-page update → Google Doc draft (not a live site edit); at-risk
  customer email → Gmail; talk track → Slack.
- **B (feature launch):** "we're better" → blog post draft in a Google Doc,
  a LinkedIn post, and ad copy in a Google Doc (there is no ads
  integration); "we're missing it" → Slack alert to the product team plus a
  sales FAQ in a Google Doc.
- **C (weekly content plan):** this week's signals plus GDELT, Reddit and
  Hacker News → rank the top five → outlines (Google Doc) and social posts →
  approval → each approved post waits on a durable timer until its slot,
  then posts to LinkedIn/X.

## Not in this version
Crisis auto-pause, content performance measurement, multiple users and roles,
paid intelligence feeds, publishing without approval.
