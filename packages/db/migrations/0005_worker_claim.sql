-- 0005_worker_claim.sql — make the lease real: record WHO owns a claimed run.
--
-- 0003 gave us claim_next_run() + a heartbeat_at column, but a run didn't record
-- which worker holds it. That's fine for one worker, but the moment a stalled run
-- is requeued and re-claimed by a second worker, the original (merely slow, not
-- dead) worker could still heartbeat it — two workers think they own one run.
-- This is "split-brain". The fix: stamp claimed_by on claim, and scope every
-- heartbeat/finish to (id, claimed_by). A worker can only touch a run it still owns.

begin;

alter table runs add column if not exists claimed_by text;

-- Recreate claim with a worker-id argument. (Different arg list than the 0003
-- version, so drop the old one first to avoid an ambiguous overload.)
drop function if exists claim_next_run();

create or replace function claim_next_run(p_worker_id text)
returns setof runs
language sql
as $$
  update runs
     set status       = 'running',
         started_at   = now(),
         claimed_by   = p_worker_id,
         heartbeat_at = now()          -- stamp the lease immediately on claim
   where id = (
     select id
       from runs
      where status = 'queued'
      order by queued_at
      for update skip locked            -- the line that makes many workers safe
      limit 1
   )
  returning *;
$$;

-- Requeue runs whose lease has lapsed (no heartbeat within max_silence), and
-- release ownership so another worker can cleanly take over. started_at is reset
-- so the freshly-queued run isn't instantly re-reaped on its stale timestamp.
create or replace function reap_stalled_runs(max_silence interval default interval '15 seconds')
returns integer
language sql
as $$
  with stalled as (
    update runs
       set status = 'queued', started_at = null, heartbeat_at = null, claimed_by = null
     where status = 'running'
       and claimed_by is not null            -- only worker-owned runs have a lease
       and coalesce(heartbeat_at, started_at) < now() - max_silence
    returning 1
  )
  select count(*)::int from stalled;
$$;

commit;
