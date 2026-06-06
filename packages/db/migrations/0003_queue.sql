-- 0003_queue.sql — Postgres-as-queue for run execution.
--
-- No Redis, no external broker. A worker claims the next queued run atomically
-- with FOR UPDATE SKIP LOCKED, so many workers can run safely without ever
-- handing the same run to two of them. This is independent of *how* a run is
-- executed (agent framework, sandbox, etc.) — it only governs claiming.

begin;

-- Claim the oldest queued run and mark it running. Returns the claimed row, or
-- nothing if the queue is empty. Called via RPC from the worker.
create or replace function claim_next_run()
returns setof runs
language sql
as $$
  update runs
     set status = 'running',
         started_at = now()
   where id = (
     select id
       from runs
      where status = 'queued'
      order by queued_at
      for update skip locked
      limit 1
   )
  returning *;
$$;

-- A heartbeat column lets a supervisor detect dead workers and requeue runs.
alter table runs add column if not exists heartbeat_at timestamptz;

-- Requeue runs that have been 'running' with no heartbeat for too long.
create or replace function reap_stalled_runs(max_silence interval default interval '5 minutes')
returns integer
language sql
as $$
  with stalled as (
    update runs
       set status = 'queued', started_at = null, heartbeat_at = null
     where status = 'running'
       and coalesce(heartbeat_at, started_at) < now() - max_silence
    returning 1
  )
  select count(*)::int from stalled;
$$;

commit;
