-- 0011_credentials.sql — DB-backed credential store (docs/credentials-contract.md §5).
--
-- One row per named credential of a credential type (§3): the hand-written
-- catalog providers ("slack", "github", …) and Activepieces pieces
-- ("pieces.<slug>"). `data` holds the secret key/values the runtime resolves
-- (reasoner for http/builtin nodes, pieces-worker for pieces nodes); the API
-- only ever exposes WHICH keys are set, never the values.
begin;

create table if not exists credentials (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  type          text not null,                -- credential type id (§3)
  name          text not null,
  data          jsonb not null default '{}'::jsonb,  -- secret values; MVP plaintext, TODO encrypt at rest
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

create index if not exists credentials_workspace_idx on credentials (workspace_id);

commit;
