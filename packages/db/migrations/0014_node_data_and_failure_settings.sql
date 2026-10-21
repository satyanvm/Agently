-- 0014_node_data_and_failure_settings.sql
--
-- Builder debugging + failure handling:
--   * run_agents keep each node's key, full output and error so the builder can
--     show a node's data, reuse it for "Run step", and pin it.
--   * workflows.settings holds workflow-level failure handling: the error
--     workflow to start when a run fails, and alert targets (email / Slack).
-- Idempotent: the API also applies it at startup (platform.EnsureSchema).
begin;

alter table run_agents
  add column if not exists node_key text,
  add column if not exists output   jsonb,
  add column if not exists error    text,
  add column if not exists attempts int not null default 0;

create index if not exists run_agents_run_node_key_idx on run_agents (run_id, node_key);

alter table workflows
  add column if not exists settings jsonb not null default '{}'::jsonb;

-- Set when the API's failure watcher has started the error workflow / sent
-- alerts for a failed run, so each failure is handled once.
alter table runs add column if not exists failure_handled_at timestamptz;

commit;
