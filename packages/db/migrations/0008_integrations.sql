-- 0008_integrations.sql — third-party account connections (OAuth2).
--
-- When a workspace connects Google (the "Connect Gmail" button), the OAuth callback
-- stores the refresh token here so the worker can send mail AS that account (option 2,
-- send-as-user) without ever holding a password. One row per (workspace, provider).
--
-- The refresh token is a long-lived secret; in production it should be encrypted at
-- rest (KMS / pgcrypto). Stored plaintext here for the MVP — noted as a follow-up.
begin;

create table if not exists integrations (
  id            text primary key,
  workspace_id  text not null references workspaces(id) on delete cascade,
  provider      text not null,                 -- 'google'
  account_email text not null default '',      -- the connected account address (the sender)
  refresh_token text not null default '',      -- long-lived OAuth2 refresh token
  scopes        text not null default '',      -- space-delimited granted scopes
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  unique (workspace_id, provider)
);

create index if not exists integrations_workspace_idx on integrations (workspace_id);

commit;
