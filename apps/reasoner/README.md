# apps/reasoner — the reasoning + durability plane

Vertical Slice 1 of Agently's unified platform. A run launched with
`engine='temporal'` is executed here by a **LangGraph** graph whose nodes run as
**Temporal** activities (durable + resumable), driving a **Browserbase** session and
traced in self-hosted **Langfuse**. The reasoner writes run state back into the same
Postgres tables the Go API serves, so the existing Agently UI shows these runs with
no frontend change.

```
prompt ──▶ Go API (engine='temporal' run row, status='queued')
                 │  (dispatcher polls Postgres)
                 ▼
        Temporal workflow ──▶ LangGraph: plan → browse → synthesize → deliver
                 │                each node = a Temporal activity
                 ▼
        Postgres (runs/run_logs/run_agents/artifacts/browser_*)  +  Langfuse traces
```

## Why "reasoning inside durability"
Each graph node is declared `execute_in="activity"`, so Temporal records its result
in workflow history. Kill the worker mid-`browse` and the workflow resumes at the
browse activity — not from the top, and without re-running the planner or
re-charging its tokens.

## Run it

Prereqs (from the repo root): Postgres + Temporal up via `docker compose up -d`,
migrations applied (incl. `0009_temporal_engine.sql`), and `.env` populated
(`DATABASE_URL`, `TEMPORAL_*`, optionally `OPENAI_API_KEY`, `BROWSERBASE_*`,
`LANGFUSE_*`). Langfuse is optional: `docker compose -f docker-compose.langfuse.yml up -d`.

```bash
cd apps/reasoner
python -m venv .venv && source .venv/bin/activate
pip install -e .
playwright install chromium          # only needed for real Browserbase browsing
python -m reasoner.worker
```

Then launch a temporal run against any existing workflow slug:

```bash
curl -XPOST 'http://localhost:8080/api/workflows/<slug>/runs?engine=temporal' \
  -H 'content-type: application/json' \
  -d '{"input":{"topic":"top Show HN launches","urls":["https://news.ycombinator.com/show"],"email":"you@example.com"}}'
```

Watch it in the Agently UI (the run lists and its logs/agents/browser tabs fill in),
in the Temporal UI (http://localhost:8080 → one activity per node), and — if
configured — in Langfuse (http://localhost:3001, grouped under session = run id).

## Graceful degradation
- No `OPENAI_API_KEY` → deterministic mock completions (run still completes).
- No `BROWSERBASE_*` → simulated browse over plain HTTP (still writes browser_* rows).
- No `LANGFUSE_*` → tracing is a no-op.

## Notes
- The Temporal LangGraph plugin (`temporalio.contrib.langgraph`) is pre-GA; versions
  are pinned in `pyproject.toml`. If its API shifts, `graph.py` (node metadata) and
  `worker.py` (plugin construction) are the only touch points.
