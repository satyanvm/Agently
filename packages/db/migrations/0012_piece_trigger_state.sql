-- 0012: key-value state for Activepieces piece triggers (context.store +
-- polling dedupe, e.g. lastFetchEpochMS). Scoped per (workflow, trigger node)
-- so two workflows using the same trigger never share cursors. Written by
-- apps/pieces-worker (src/trigger-store.ts); no API surface reads it.
begin;

create table if not exists piece_trigger_state (
  workflow_id  text not null references workflows(id) on delete cascade,
  node_key     text not null,
  key          text not null,
  value        jsonb not null default 'null'::jsonb,
  updated_at   timestamptz not null default now(),
  primary key (workflow_id, node_key, key)
);

commit;
