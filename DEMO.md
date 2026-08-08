# Agently demo

This demo exercises the current Temporal-only architecture. It requires real
Postgres, Anthropic, Voyage, Langfuse, Browserbase, SMTP, and Temporal
configuration; it never replaces a missing dependency with mock output.

## Start

```bash
cp .env.example .env
# Fill the required values in .env.
pnpm agently
```

Open `http://localhost:3000`, create a workflow from a prompt, inspect the
generated DAG, and start a run. Use the run page for persisted logs, node status,
cost, artifacts, browser activity, and the Langfuse trace link.

## Durability demonstration

1. Start a multi-node run.
2. Stop the reasoner process while a node is running.
3. Restart it with `pnpm agently:reasoner`.
4. Inspect Temporal UI at `http://localhost:8080`.

Temporal replays workflow history and resumes from the in-flight activity.
Completed node activities are read from history instead of being executed again.

If any required service or credential is unavailable, the run or service fails
with that reason. That visible failure is part of the demonstration.
