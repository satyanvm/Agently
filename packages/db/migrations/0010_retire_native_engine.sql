-- 0010_retire_native_engine.sql — retire the native Go worker engine.
--
-- Every run now executes on the Temporal + LangGraph reasoner. The native
-- Postgres-queue worker (apps/worker) is archived under archive/worker; its
-- lease machinery — claim_next_run() / reap_stalled_runs(), claimed_by,
-- heartbeat_at — is no longer exercised.
--
-- We DROP the claim/reap functions so a stray old worker binary fails loudly
-- instead of silently claiming runs, but KEEP the columns (claimed_by,
-- heartbeat_at) and historical engine='native' rows: existing run history must
-- keep reading correctly in the UI.

begin;

-- New runs default to the Temporal plane. (The API also sets this explicitly;
-- the default covers direct inserts.)
alter table runs alter column engine set default 'temporal';

-- Retire the native lease machinery. Any old worker binary still running will
-- now error on claim instead of executing runs nothing dispatches to it.
drop function if exists claim_next_run(text);
drop function if exists reap_stalled_runs(interval);

-- The dispatch index for temporal runs (0009) is now the hot path; the native
-- queue ordering no longer needs anything special.

commit;
