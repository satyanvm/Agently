# Database — source of truth for the schema

SQL migrations for Supabase Postgres. Applied in filename order.

Planned (Phase 1):

- `0001_init.sql` — `agents`, `runs`, `run_logs` tables, enums, indexes.
- `0002_rls.sql` — Row Level Security policies (each user sees only their own rows).
- `0003_queue.sql` — `claim_next_run()` function using `FOR UPDATE SKIP LOCKED`.

Apply with the Supabase CLI or by running the files against `DATABASE_URL`.
The TypeScript domain types in `packages/core/src/types.ts` mirror these tables.
