-- 0002_rls.sql — Row Level Security: every user sees only their workspace's rows.
--
-- The web app uses the anon/auth Supabase client (RLS enforced); the worker and
-- trusted server code use the service-role client (RLS bypassed). Policies key
-- off a `workspace_members` mapping from auth.uid() → workspace.
--
-- This is forward-looking scaffolding: it documents the access model the schema
-- assumes. Enable once Supabase Auth is wired (the in-memory mock backend does
-- not use it).

begin;

-- Helper: the set of workspace ids the current user belongs to.
create or replace function current_workspace_ids()
returns setof text
language sql stable
as $$
  select m.workspace_id
  from members m
  where m.id = auth.uid()::text
$$;

-- Enable RLS on every tenant-scoped table.
do $$
declare t text;
begin
  foreach t in array array[
    'workspaces','members','api_keys','agent_definitions','workflows',
    'workflow_versions','runs','run_agents','agent_messages','artifacts',
    'run_logs','browser_sessions','browser_actions','browser_shots',
    'browser_console','notifications','activity_events','domain_events'
  ]
  loop
    execute format('alter table %I enable row level security;', t);
  end loop;
end $$;

-- Direct workspace-scoped tables.
create policy ws_select on workspaces        for select using (id in (select current_workspace_ids()));
create policy mem_select on members          for select using (workspace_id in (select current_workspace_ids()));
create policy ak_select on api_keys          for select using (workspace_id in (select current_workspace_ids()));
create policy ad_select on agent_definitions for select using (workspace_id in (select current_workspace_ids()));
create policy wf_select on workflows         for select using (workspace_id in (select current_workspace_ids()));
create policy run_select on runs             for select using (workspace_id in (select current_workspace_ids()));
create policy ntf_select on notifications    for select using (workspace_id in (select current_workspace_ids()));
create policy act_select on activity_events  for select using (workspace_id in (select current_workspace_ids()));
create policy de_select on domain_events     for select using (workspace_id in (select current_workspace_ids()));

-- Child tables inherit visibility through their parent run/workflow.
create policy wfv_select on workflow_versions for select using (
  workflow_id in (select id from workflows where workspace_id in (select current_workspace_ids()))
);
create policy ra_select on run_agents     for select using (run_id in (select id from runs));
create policy am_select on agent_messages for select using (run_id in (select id from runs));
create policy art_select on artifacts     for select using (run_id in (select id from runs));
create policy rl_select on run_logs       for select using (run_id in (select id from runs));
create policy bs_select on browser_sessions for select using (run_id in (select id from runs));
create policy ba_select on browser_actions  for select using (
  session_id in (select id from browser_sessions)
);
create policy bsh_select on browser_shots   for select using (
  session_id in (select id from browser_sessions)
);
create policy bc_select on browser_console  for select using (
  session_id in (select id from browser_sessions)
);

commit;
