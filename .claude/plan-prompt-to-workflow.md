# Plan — "a prompt that becomes a running agent crew"

## The one-line summary
The durable engine is already built and works. The missing piece is the **front door**:
a prompt → workflow **compiler**, the **buttons** that call it, a **launch dialog**, and
the worker change that lets a browser agent **visit the exact sites a prompt names**
(X, Google News, a blog) instead of hardcoded URLs. No new per-site adapters — the
browser is the universal source.

---

## What already works (verified, do NOT rebuild)
- Postgres-as-queue, crash-safe workers (lease/heartbeat/reaper), parallel multi-agent
  DAG executor, real arXiv/HN/Reddit/web fetch, Browserbase CDP (navigates + extracts
  arbitrary URLs), real SMTP email of the digest. AI Digest runs end-to-end.

## The real gaps (root cause of "buttons don't work")
1. **Dead buttons** — `New workflow`, `Run now`, `New run` have no `onClick`.
2. **`WorkflowService.Create` builds an unrunnable workflow** — `currentVersionId: nil`,
   `agentCount: 0`, no graph. Even if a button called it, the result couldn't run.
3. **No prompt→graph step** — nothing turns "pull AI news from arxiv/reddit/news and
   email me" into fetcher agents + an editor + the run input (topic/email/urls).
4. **Browser agent uses hardcoded `example.com`** — `dag.go runBrowser` ignores the
   prompt's URLs, so "visit site X" can't work.
5. **Operational**: `dev:api` script sets no `DATABASE_URL`, so the API silently runs
   in-memory (data vanishes); API never loads `.env`.

---

## Design: the prompt → workflow compiler (hybrid, per your choice)

New file `apps/api/internal/services/planner.go`. Input: the user's prompt (+ optional
name/schedule/email). Output: a `domain.WorkflowVersion` (`[]GraphNode`) + a default
run-input map (topic, arxivQuery, subreddits, urls, email).

**Two-tier, robust:**
- **LLM tier (uses your OPENAI_API_KEY):** ask the model to return strict JSON:
  `{ topic, sources:[arxiv|hn|reddit|web|news], urls:[...], subreddits:[...], email, schedule }`.
  A small, fixed system prompt; we parse JSON, never free text.
- **Deterministic fallback (no key / bad JSON):** keyword scan of the prompt —
  detect `arxiv`, `reddit`/subreddit names, `hacker news`/`hn`, `google news`/`news`,
  bare URLs (regex), an email (regex), and "every morning/daily/9am" → schedule.
  This guarantees the feature works offline.

**Graph it builds** (mirrors the seeded AI Digest shape, so the worker already knows how
to run it): one fetcher node per detected source (col 0) → one **Editor** writer node
(col 1) that depends on all fetchers and emails the result. Node **names contain the
keyword** the worker dispatches on (`arXiv Fetcher`, `HN Fetcher`, `Reddit Fetcher`,
`News Fetcher`, `Web Fetcher`) — that's the existing `fetchForAgent` contract.

`WorkflowService.Create` is extended to:
1. parse the prompt via the planner,
2. insert the workflow, insert its `WorkflowVersion` (the graph),
3. `Workflows.Update` to set `CurrentVersionID` + `AgentCount` (so it's runnable),
4. store the planner's default run-input on the workflow so launches inherit it.

Default-input storage: add `runs.input` already exists; for the **workflow's** default
input I'll add a `default_input jsonb` column via a new migration `0007_workflow_default_input.sql`
(+ memory/postgres repo + entity field). `Launch` merges `workflow.default_input` with any
per-run input (per-run wins). This is what makes "every run of this workflow emails YOU
about THESE sources" work without re-typing.

**Contract/validation update:** `ParseCreateWorkflowInput` gains optional `prompt`,
`email`, `schedule`. `CreateWorkflowInput` carries them. Zod `CreateWorkflowInput` in
`packages/contracts` updated to match (keeps FE/BE in sync per the repo's discipline).

---

## Worker change: browser visits the prompt's sites (your key idea)

Two precise edits in `apps/worker/internal/agent/dag.go`:
1. **`fetchForAgent`**: add a `news` case → reuse the existing `sources.Web` over Google
   News RSS URL built from the topic (`news.google.com/rss/search?q=<topic>`). Keyless,
   high-value, no new adapter file — just a URL. (Still "web fetch", not a new seam.)
2. **`runBrowser`**: replace hardcoded `targets := []string{example.com,...}` with URLs
   from `input["urls"]` (the planner's extracted sites). If none, fall back to current
   behavior. Now "visit X.com and summarize" drives the real Browserbase session to
   X.com. Browser agents already feed their extract into the LLM prompt — unchanged.

Net effect: any site a prompt names is visited by the real browser; common feeds
(arxiv/hn/reddit/news) use fast keyless APIs. The platform is now general, n8n-like.

---

## Frontend: make the buttons real

New primitive `apps/web/components/ui/dialog.tsx` — a tiny portal/overlay modal modeled
on the existing `command-palette.tsx` pattern (same backdrop + stop-propagation; no new
dep). Used by:

1. **`CreateWorkflowDialog`** (`components/create-workflow-dialog.tsx`): a textarea for
   the prompt ("Every morning pull AI research from arXiv, Reddit, Google News and email
   me a digest at me@gmail.com"), optional name + schedule + email fields, a **live
   preview** of the parsed graph (calls a new `POST /api/workflows/plan` dry-run that
   returns the graph without saving — so you SEE the agents before creating). On submit →
   `createWorkflow()` → redirect to the new workflow's detail page.
2. **`RunWorkflowDialog`** (`components/run-workflow-dialog.tsx`): pre-filled from the
   workflow's default input (topic/email/urls), editable, → `launchRun(slug, input)` →
   redirect to the run detail page to watch it stream live.

Wire the dead buttons:
- `workflows/page.tsx` `New workflow` → opens `CreateWorkflowDialog`.
- `shell/sidebar.tsx` `New workflow` → same (turn the `<Link>` into a button that opens it).
- `workflow-detail.tsx` `Run now` → opens `RunWorkflowDialog`.
- `shell/topbar.tsx` `New run` / `NewRunButton` → opens a workflow picker → `RunWorkflowDialog`.

New `lib/api.ts` functions: `createWorkflow(input)`, `planWorkflow(prompt)` (dry-run).

---

## Scheduling — deferred per your choice ("manual run first")
I will **store** the schedule on the workflow (so it's captured from the prompt and shown
in the UI), but NOT build the scheduler daemon now. The "Run now" button + dialog is the
path you'll use. A `SCHEDULING.md` note records exactly where the scheduler hooks in later
(a tiny loop in the worker that `INSERT`s a queued run per due schedule), so it's a clean
follow-up. (Say the word and I'll build it in this pass instead.)

---

## Backend API additions
- `POST /api/workflows/plan` → `planWorkflow` handler (dry-run, returns graph + input).
- `POST /api/workflows` (exists) → now produces a **runnable** workflow.
- Router: add the one `plan` route.

---

## Files touched (summary)
**Backend (Go, apps/api):**
- `internal/services/planner.go` *(new)* — prompt→graph + input, LLM+fallback.
- `internal/services/workflow_service.go` — Create builds+links the version; plan dry-run.
- `internal/services/llm.go` *(new, tiny)* — minimal OpenAI/Anthropic JSON call for the
  planner (the API has no LLM client yet; the worker's lives in its own module).
- `internal/domain/validate/api.go` — prompt/email/schedule on CreateWorkflowInput.
- `internal/domain/entities.go` + repos (memory+postgres) — `Workflow.DefaultInput`.
- `internal/handler/workflows.go` + `router.go` — plan route.
- `cmd/server/main.go` — load `.env` (godotenv) so OPENAI/SMTP keys are available.
- `packages/db/migrations/0007_workflow_default_input.sql` *(new)* + docker-compose mount.

**Worker (Go, apps/worker):**
- `internal/agent/dag.go` — `news` fetch case; `runBrowser` uses `input["urls"]`.

**Frontend (apps/web):**
- `components/ui/dialog.tsx` *(new)*, `components/create-workflow-dialog.tsx` *(new)*,
  `components/run-workflow-dialog.tsx` *(new)*.
- `lib/api.ts` — `createWorkflow`, `planWorkflow`.
- `workflows/page.tsx`, `shell/sidebar.tsx`, `shell/topbar.tsx`, `workflow-detail.tsx`
  — wire buttons.

**Contracts:** `packages/contracts/src/api.ts` — CreateWorkflowInput fields.

**Ops/docs:**
- `package.json` `dev:api` → pass `DATABASE_URL` (so API is durable in dev).
- `architecture.md` — new "Chunk 17 — the front door: prompt→workflow + launch UI"
  section (the flow/orchestration write-up you asked for: how a sentence becomes a
  running crew, traced through every layer).

---

## How I'll verify (end-to-end, real)
1. `docker compose up -d`; start API (with DATABASE_URL + .env) and worker.
2. Create via dialog: *"Every morning pull the latest AI research from arXiv and Reddit
   r/MachineLearning plus AI news from Google News, and email me a digest at
   <your-gmail>."* → preview shows arXiv+Reddit+News fetchers → Editor.
3. Confirm a runnable workflow row (currentVersionId set, agent_count>0) in Postgres.
4. Click **Run now** → watch the run stream (logs, per-agent DAG, cost) on the run page.
5. Confirm the digest email lands in your Gmail (SMTP already configured in `.env`).
6. Add a prompt naming a specific site → confirm the Browser tab shows the real
   Browserbase session visiting it.
7. `kill -9` the worker mid-run → another worker resumes (durability still holds).

## Risks / honest notes
- **X/Twitter** is auth-gated; headless visits may get a login wall. Handled by graceful
  degradation (the agent proceeds with whatever it extracted) and labeled in the UI — not
  a new failure mode, just a limited result for that one source.
- **OpenAI planner JSON**: guarded by the deterministic fallback, so a bad/blocked LLM
  call still yields a working graph.
- I can't visually QA your Gmail inbox for you; I'll verify the SMTP send succeeds and the
  payload contains real items, and tell you explicitly what I could and couldn't confirm.
