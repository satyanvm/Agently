# Pieces worker

The pieces worker executes `pieces.*` nodes as Temporal activities by importing
Activepieces packages and calling their real action/trigger functions. It does
not run the Activepieces orchestration engine.

## Required configuration

- `DATABASE_URL`: credential resolution and durable trigger state.
- `TEMPORAL_HOSTPORT`: defaults to `localhost:7233`.
- `TEMPORAL_NAMESPACE`: defaults to `default`.
- `PIECES_TASK_QUEUE`: defaults to `agently-pieces`.

The worker refuses to start without `DATABASE_URL`. A missing credential returns
`MissingCredential`, which the reasoner records as a failed node. An unavailable
worker causes the Temporal piece activity to time out and fail the run.

## Build and run

```bash
npm install
npm run build
npm start
```

Generate planner metadata after package changes:

```bash
npm run gen:index
npm run gen:embeddings   # reads PLANNER_EMBED_MODEL; with ollama/* just keep `ollama serve` running
```

Embedding generation uses `PLANNER_EMBED_MODEL` (`ollama/nomic-embed-text` in
`.env.example` — local, no quota; also `gemini-embedding-001` or
`text-embedding-3-small`) at 768 dimensions and writes the
manifest/binary sidecar under `packages/nodes/pieces`.

## Test

```bash
npm test
```
