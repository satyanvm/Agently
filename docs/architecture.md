# Current architecture

Agently has one execution plane: the Python Temporal reasoner in
`apps/reasoner`. The Go API is the control plane and the Node pieces worker is a
separate activity worker for `pieces.*` nodes.

```text
web -> Go API -> Postgres (queued run)
                 |
                 v
        reasoner dispatcher -> Temporal workflow
                                  |
                   one activity per DAG node
                    /        |          \
                 Claude   Browserbase   pieces worker
```

## Planner

Workflow creation is a strict map/reduce compiler over the hand-written catalog
and generated Activepieces index:

1. Voyage embeddings prefilter the piece directory.
2. Anthropic routes clusters and maps selected node ids using the small
   `PLANNER_MAP_MODEL` model.
3. Anthropic reduce uses `PLANNER_MODEL` to author the complete graph, including
   config, dependencies, and templates.
4. Structural validation and bounded repair run before persistence.

Missing keys, stale embeddings, timeouts, malformed responses, or provider errors
return an error. There is no deterministic graph floor.

## Runtime guarantees

- Postgres is required by the API, reasoner, and pieces worker.
- `DATABASE_URL` is required by the pieces worker for credentials and trigger state.
- Langfuse is required; tracing failures are visible.
- Browser nodes require Browserbase and never use a simulated browser.
- Piece nodes require the pieces worker and real credentials; failures fail the
  node/run instead of recording intent.
- Unsupported node types and missing templates fail with their exact reason.
- The web app renders API data only; running clocks use the current time and no
  synthetic costs, logs, screenshots, or activity rows are generated.

## Database migrations

`0009` and `0010` are historical transition migrations. `0013` removes the
retired `runs.engine`, `claimed_by`, and `heartbeat_at` columns. New installs
apply all migrations mounted by `docker-compose.yml` in order.
