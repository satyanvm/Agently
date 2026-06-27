-- 0009_temporal_engine.sql — introduce a second execution engine alongside the
-- native Postgres-queue worker, without disturbing it.
--
-- Vertical Slice 1 of the "reasoning runs inside durability" platform: a run can
-- now be executed by the Temporal + LangGraph reasoner (engine='temporal') instead
-- of the in-house Go worker (engine='native', the default and unchanged path).
--
-- The two engines share this one `runs` table (the single source of truth the UI
-- already reads), so the ONLY thing we must guarantee is that they never claim each
-- other's work: claim_next_run() / reap_stalled_runs() are scoped to engine='native'.
-- A temporal run is dispatched to Temporal by the Python reasoner's dispatcher
-- (workflow_id = run id → idempotent), and the reasoner writes run/agent/log/browser
-- state back into these same tables so the existing API + UI light up unchanged.

begin;

-- engine: which execution plane owns this run. Default 'native' keeps every
-- existing and future native run behaving exactly as before.
alter table runs add column if not exists engine text not null default 'native';

-- langfuse_trace_id: deep-trace handle written by the reasoner so the run-detail
-- page can deep-link into self-hosted Langfuse. Null for native runs.
alter table runs add column if not exists langfuse_trace_id text;

-- A native worker must never claim a temporal run. Re-scope the claim to native.
-- (Same body as 0005, with the engine guard added.)
create or replace function claim_next_run(p_worker_id text)
returns setof runs
language sql
as $$
  update runs
     set status       = 'running',
         started_at   = now(),
         claimed_by   = p_worker_id,
         heartbeat_at = now()
   where id = (
     select id
       from runs
      where status = 'queued'
        and engine = 'native'             -- temporal runs are dispatched elsewhere
      order by queued_at
      for update skip locked
      limit 1
   )
  returning *;
$$;

-- The reaper requeues stalled *native* runs (they carry a lease). Temporal runs
-- have claimed_by IS NULL so they were already excluded, but make the intent
-- explicit and future-proof: never reap a temporal run.
create or replace function reap_stalled_runs(max_silence interval default interval '15 seconds')
returns integer
language sql
as $$
  with stalled as (
    update runs
       set status = 'queued', started_at = null, heartbeat_at = null, claimed_by = null
     where status = 'running'
       and engine = 'native'
       and claimed_by is not null
       and coalesce(heartbeat_at, started_at) < now() - max_silence
    returning 1
  )
  select count(*)::int from stalled;
$$;

-- The dispatcher polls for queued temporal runs; index that lookup.
create index if not exists runs_temporal_dispatch_idx
  on runs (queued_at)
  where engine = 'temporal' and status = 'queued';

commit;
