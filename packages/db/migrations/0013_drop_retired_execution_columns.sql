-- 0013_drop_retired_execution_columns.sql
--
-- Temporal is the only execution plane. Remove the run-level engine selector
-- and the lease columns that belonged to the retired Postgres-queue worker.
begin;

drop index if exists runs_temporal_dispatch_idx;
alter table runs
  drop column if exists engine,
  drop column if exists claimed_by,
  drop column if exists heartbeat_at;

commit;
