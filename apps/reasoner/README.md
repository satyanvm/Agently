# Reasoner

The reasoner is Agently's only execution plane. It polls queued runs from the
shared Postgres database, starts one Temporal workflow per run, and executes each
DAG node as a separately checkpointed Temporal activity.

## Required configuration

The worker refuses to start when any required dependency is missing:

- `DATABASE_URL`
- `ANTHROPIC_API_KEY`
- `LANGFUSE_PUBLIC_KEY` and `LANGFUSE_SECRET_KEY`
- `BROWSERBASE_API_KEY` and `BROWSERBASE_PROJECT_ID`
- `SMTP_HOST`

Temporal defaults to `localhost:7233`, namespace `default`, and task queue
`agently-reasoner`. Piece nodes are sent to `PIECES_TASK_QUEUE`, which defaults
to `agently-pieces`.

There are no mock completions, simulated browser sessions, no-op traces, or
record-intent success paths. A missing credential, unavailable worker, provider
error, timeout, or unsupported node fails the node and records the reason.

## Run locally

```bash
docker compose up -d
cd apps/reasoner
python3 -m venv .venv
./.venv/bin/pip install -e .
./.venv/bin/python -m reasoner.worker
```

The API creates queued runs. The dispatcher starts `DynamicWorkflow` for composed
graphs and `ReasoningWorkflow` for the built-in plan/browse/synthesize/deliver
flow. Run state, logs, agents, artifacts, browser sessions, and Langfuse handles
are written back to Postgres for the web app.

## Test

```bash
./.venv/bin/python -m pytest -q
```
