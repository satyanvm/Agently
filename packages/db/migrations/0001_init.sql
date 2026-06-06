-- 0001_init.sql — core schema for the Agently control plane.
--
-- This mirrors the canonical entity model in @agently/contracts. Column names
-- are snake_case; ids are prefixed text PKs (wf_…, run_…) matching the app.
-- It is intentionally execution-agnostic: no runner/orchestrator/browser-driver
-- assumptions leak in here, so the schema survives those decisions.

begin;

-- ───────────────────────── enums ─────────────────────────
create type run_status        as enum ('queued','running','succeeded','failed','canceled','paused');
create type agent_status      as enum ('idle','running','succeeded','failed','blocked','waiting');
create type agent_role        as enum ('orchestrator','researcher','browser','coder','analyst','writer','validator');
create type log_level         as enum ('debug','info','success','warn','error');
create type log_channel       as enum ('system','agent','tool','browser','model');
create type trigger_type      as enum ('manual','schedule','webhook','event');
create type artifact_kind     as enum ('file','json','image','dataset','report','url','code');
create type browser_act_type  as enum ('navigate','click','type','scroll','extract','wait','screenshot','submit');
create type notification_type as enum ('workflow.completed','workflow.failed','browser.error','cost.alert','agent.blocked','run.started');
create type severity          as enum ('info','success','warning','error');
create type activity_kind     as enum ('deploy','run','complete','fail','comment','scale');
create type member_role       as enum ('owner','admin','member');
create type workspace_plan    as enum ('free','pro','enterprise');

-- ─────────────────────── tenancy ─────────────────────────
create table workspaces (
  id              text primary key,
  name            text not null,
  slug            text not null unique,
  plan            workspace_plan not null default 'free',
  default_region  text not null default 'us-east-1',
  created_at      timestamptz not null default now()
);

create table members (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  name          text not null,
  email         text not null,
  initials      text not null,
  role          member_role not null default 'member',
  created_at    timestamptz not null default now(),
  unique (workspace_id, email)
);

create table api_keys (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  name          text not null,
  masked_key    text not null,
  -- The secret itself is stored only as a hash; never returned after creation.
  key_hash      text,
  scopes        text[] not null default '{}',
  last_used_at  timestamptz,
  created_at    timestamptz not null default now()
);

-- ──────────────────── agent definitions ──────────────────
create table agent_definitions (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  name          text not null,
  role          agent_role not null,
  model         text not null,
  description   text not null default '',
  tools         text[] not null default '{}',
  config        jsonb not null default '{}'::jsonb,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

-- ───────────────────────── workflows ─────────────────────
create table workflows (
  id                  text primary key,
  workspace_id        text not null references workspaces(id) on delete cascade,
  slug                text not null,
  name                text not null,
  description         text not null default '',
  trigger             trigger_type not null default 'manual',
  schedule            text,
  tags                text[] not null default '{}',
  owner_id            text references members(id) on delete set null,
  agent_count         int not null default 0,
  current_version_id  text,
  created_at          timestamptz not null default now(),
  updated_at          timestamptz not null default now(),
  archived_at         timestamptz,
  unique (workspace_id, slug)
);

create table workflow_versions (
  id            text primary key,
  workflow_id   text not null references workflows(id) on delete cascade,
  version       int not null,
  -- The agent graph (nodes + dependencies) captured definitionally.
  nodes         jsonb not null default '[]'::jsonb,
  note          text,
  created_by    text references members(id) on delete set null,
  created_at    timestamptz not null default now(),
  unique (workflow_id, version)
);

alter table workflows
  add constraint workflows_current_version_fk
  foreign key (current_version_id) references workflow_versions(id) on delete set null;

-- ─────────────────────────── runs ────────────────────────
create table runs (
  id                   text primary key,
  workspace_id         text not null references workspaces(id) on delete cascade,
  workflow_id          text not null references workflows(id) on delete cascade,
  workflow_version_id  text references workflow_versions(id) on delete set null,
  number               int not null,
  status               run_status not null default 'queued',
  trigger              trigger_type not null default 'manual',
  triggered_by         jsonb not null,                 -- { name, initials }
  region               text not null,
  steps_done           int not null default 0,
  steps_total          int not null default 0,
  current_step         text not null default '',
  cost_usd             numeric(12,4) not null default 0,
  tokens_in            bigint not null default 0,
  tokens_out           bigint not null default 0,
  error                text,
  browser_session_id   text,
  queued_at            timestamptz not null default now(),
  started_at           timestamptz,
  finished_at          timestamptz,
  unique (workflow_id, number)
);

create table run_agents (
  id                   text primary key,
  run_id               text not null references runs(id) on delete cascade,
  agent_definition_id  text references agent_definitions(id) on delete set null,
  name                 text not null,
  role                 agent_role not null,
  model                text not null,
  status               agent_status not null default 'idle',
  depends_on           text[] not null default '{}',
  col                  int not null default 0,
  "row"                int not null default 0,
  summary              text not null default '',
  metrics              jsonb not null default '{}'::jsonb, -- tokens, costUsd, runtimeMs, toolCalls, progress
  started_at           timestamptz,
  finished_at          timestamptz
);

create table agent_messages (
  id             text primary key,
  run_id         text not null references runs(id) on delete cascade,
  from_agent_id  text not null references run_agents(id) on delete cascade,
  to_agent_id    text not null references run_agents(id) on delete cascade,
  label          text not null,
  at             timestamptz not null default now()
);

create table artifacts (
  id                  text primary key,
  run_id              text not null references runs(id) on delete cascade,
  name                text not null,
  kind                artifact_kind not null,
  size_bytes          bigint,
  produced_by_agent_id text references run_agents(id) on delete set null,
  produced_by_name    text not null default '',
  storage_key         text,
  preview             text,
  created_at          timestamptz not null default now()
);

-- Append-only run logs. `seq` gives stable ordering even when timestamps collide.
create table run_logs (
  id         text primary key,
  run_id     text not null references runs(id) on delete cascade,
  seq        int not null,
  ts         timestamptz not null default now(),
  offset_ms  bigint not null default 0,
  level      log_level not null,
  channel    log_channel not null,
  source     text not null,
  message    text not null,
  detail     text,
  reasoning  boolean not null default false,
  unique (run_id, seq)
);

-- ──────────────────── browser sessions ───────────────────
create table browser_sessions (
  id             text primary key,
  run_id         text not null references runs(id) on delete cascade,
  agent_name     text not null,
  status         run_status not null default 'running',
  current_url    text not null default '',
  page_title     text not null default '',
  viewport_w     int not null default 1440,
  viewport_h     int not null default 900,
  pages_visited  int not null default 0,
  actions_count  int not null default 0,
  started_at     timestamptz not null default now(),
  finished_at    timestamptz
);

alter table runs
  add constraint runs_browser_session_fk
  foreign key (browser_session_id) references browser_sessions(id) on delete set null;

create table browser_actions (
  id          text primary key,
  session_id  text not null references browser_sessions(id) on delete cascade,
  ts          timestamptz not null default now(),
  type        browser_act_type not null,
  target      text not null,
  value       text,
  status      text not null default 'ok',         -- ok | error | pending
  duration_ms bigint not null default 0
);

create table browser_shots (
  id          text primary key,
  session_id  text not null references browser_sessions(id) on delete cascade,
  ts          timestamptz not null default now(),
  url         text not null,
  title       text not null,
  label       text not null,
  storage_key text
);

create table browser_console (
  id          bigserial primary key,
  session_id  text not null references browser_sessions(id) on delete cascade,
  ts          timestamptz not null default now(),
  level       log_level not null,
  text        text not null
);

-- ─────────────── notifications & activity feed ───────────
create table notifications (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  recipient_id  text references members(id) on delete set null,
  type          notification_type not null,
  severity      severity not null,
  title         text not null,
  body          text not null,
  workflow_slug text,
  run_id        text references runs(id) on delete set null,
  run_number    int,
  read_at       timestamptz,
  created_at    timestamptz not null default now()
);

create table activity_events (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  kind          activity_kind not null,
  actor         text not null,
  text          text not null,
  workflow_slug text,
  run_id        text references runs(id) on delete set null,
  at            timestamptz not null default now()
);

-- Durable log of domain events — the source for realtime, webhooks, and audit.
create table domain_events (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  type          text not null,
  occurred_at   timestamptz not null default now(),
  data          jsonb not null
);

-- ───────────────────────── indexes ───────────────────────
create index runs_workspace_started_idx   on runs (workspace_id, started_at desc);
create index runs_workflow_number_idx      on runs (workflow_id, number desc);
create index runs_status_idx               on runs (status) where status in ('queued','running');
create index run_agents_run_idx            on run_agents (run_id);
create index agent_messages_run_idx        on agent_messages (run_id, at);
create index artifacts_run_idx             on artifacts (run_id, created_at);
create index run_logs_run_seq_idx          on run_logs (run_id, seq);
create index browser_actions_session_idx   on browser_actions (session_id, ts);
create index browser_shots_session_idx     on browser_shots (session_id, ts);
create index notifications_ws_created_idx   on notifications (workspace_id, created_at desc);
create index notifications_unread_idx       on notifications (workspace_id) where read_at is null;
create index activity_ws_at_idx             on activity_events (workspace_id, at desc);
create index domain_events_ws_idx           on domain_events (workspace_id, occurred_at desc);

commit;
