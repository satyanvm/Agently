-- 0015_marketing_loop.sql
--
-- Tables for the signal → draft → approval → publish loop
-- (docs/phase-2-plan.md):
--   * competitors        — who we watch, and which of their pages
--   * page_snapshots     — one row per distinct version of a watched page
--   * signals            — detected market events (page changes, news, posts)
--   * brand_context      — per-workspace voice, facts, positioning, claims to avoid
--   * approvals          — a draft waiting for (or decided by) a person
--   * approval_outcomes  — what ran after a decision (the published items)
-- Every table carries workspace_id so multi-user workspaces need no schema change.
-- Idempotent: the API also applies it at startup (platform.EnsureSchema).
begin;

create table if not exists competitors (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  name          text not null,
  website       text not null default '',
  -- [{ "kind": "pricing|changelog|blog|careers|other", "url": "...",
  --    "render": "http|browser" }]
  pages         jsonb not null default '[]'::jsonb,
  notes         text not null default '',
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  archived_at   timestamptz
);

create index if not exists competitors_workspace_idx on competitors (workspace_id);

-- A row is a distinct VERSION of a page: a fetch whose normalized text hashes
-- the same as the latest row only bumps last_seen_at. "What changed" is the
-- latest row against the one before it.
create table if not exists page_snapshots (
  id             text primary key,
  workspace_id   text not null references workspaces(id) on delete cascade,
  competitor_id  text references competitors(id) on delete set null,
  page_kind      text,
  url            text not null,
  run_id         text references runs(id) on delete set null,
  fetched_via    text not null check (fetched_via in ('http', 'browser')),
  status_code    int,
  title          text not null default '',
  content_hash   text not null,
  content        text not null,           -- normalized visible text
  fetched_at     timestamptz not null default now(),
  last_seen_at   timestamptz not null default now()
);

create index if not exists page_snapshots_url_idx
  on page_snapshots (workspace_id, url, fetched_at desc);
create index if not exists page_snapshots_competitor_idx
  on page_snapshots (competitor_id, fetched_at desc);

create table if not exists signals (
  id                    text primary key,
  workspace_id          text not null references workspaces(id) on delete cascade,
  -- page_change | news | rss | reddit | hackernews | review | other
  source                text not null,
  competitor_id         text references competitors(id) on delete set null,
  title                 text not null,
  summary               text not null default '',
  url                   text not null default '',
  detail                jsonb not null default '{}'::jsonb,  -- diff lines, article metadata, …
  snapshot_id           text references page_snapshots(id) on delete set null,
  previous_snapshot_id  text references page_snapshots(id) on delete set null,
  run_id                text references runs(id) on delete set null,
  -- Same item seen twice (a news article on two runs) records one signal.
  dedupe_key            text,
  detected_at           timestamptz not null default now()
);

create index if not exists signals_workspace_idx on signals (workspace_id, detected_at desc);
create index if not exists signals_run_idx on signals (run_id);
create unique index if not exists signals_dedupe_idx
  on signals (workspace_id, dedupe_key) where dedupe_key is not null;

create table if not exists brand_context (
  workspace_id   text primary key references workspaces(id) on delete cascade,
  voice          text not null default '',
  product_facts  text not null default '',
  positioning    text not null default '',
  avoid_claims   text not null default '',
  updated_at     timestamptz not null default now(),
  updated_by     text not null default ''
);

create table if not exists approvals (
  id              text primary key,
  workspace_id    text not null references workspaces(id) on delete cascade,
  run_id          text not null references runs(id) on delete cascade,
  node_key        text not null,
  iteration       int not null default 0,          -- logic.loop item index; 0 outside loops
  title           text not null default '',
  channel         text not null default '',        -- where it publishes, e.g. "LinkedIn post"
  draft           text not null,                   -- what the AI wrote
  final_content   text,                            -- what was approved (edited or not)
  status          text not null default 'pending'
                    check (status in ('pending', 'approved', 'rejected', 'timed_out', 'canceled')),
  assignee        text not null default '',        -- who it was routed to ("Rahul")
  notify          jsonb not null default '{}'::jsonb,  -- targets + delivery results
  signal_ids      text[] not null default '{}',
  decided_by      text,                            -- name entered when deciding
  decision_note   text,
  decided_at      timestamptz,
  signaled_at     timestamptz,                     -- decision delivered to the Temporal run
  expires_at      timestamptz,
  created_at      timestamptz not null default now(),
  unique (run_id, node_key, iteration)
);

create index if not exists approvals_status_idx on approvals (workspace_id, status, created_at desc);
-- The reasoner's relay polls decided-but-undelivered approvals.
create index if not exists approvals_undelivered_idx
  on approvals (decided_at) where decided_at is not null and signaled_at is null;

create table if not exists approval_outcomes (
  id           text primary key,
  approval_id  text not null references approvals(id) on delete cascade,
  run_id       text not null references runs(id) on delete cascade,
  node_key     text not null,
  node_type    text not null,
  status       text not null check (status in ('succeeded', 'failed', 'skipped')),
  summary      text not null default '',
  url          text not null default '',           -- link to the published item, when known
  output       jsonb,
  occurred_at  timestamptz not null default now()
);

create index if not exists approval_outcomes_approval_idx on approval_outcomes (approval_id, occurred_at);

commit;
