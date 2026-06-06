-- 0004_realtime.sql — realtime delivery for live UI.
--
-- The log viewer, agent graph, and dashboard update live. With Supabase, the
-- frontend subscribes to row changes; this migration adds the tables to the
-- realtime publication. (The in-memory mock backend uses the EventBus instead;
-- this is the production path and mirrors it 1:1.)

begin;

-- Stream new log lines, run state changes, agent transitions, browser actions,
-- and notifications straight to subscribed clients.
alter publication supabase_realtime add table run_logs;
alter publication supabase_realtime add table runs;
alter publication supabase_realtime add table run_agents;
alter publication supabase_realtime add table browser_actions;
alter publication supabase_realtime add table notifications;
alter publication supabase_realtime add table domain_events;

-- Emit a NOTIFY on every domain event for non-Supabase consumers (a standalone
-- SSE/WebSocket gateway can LISTEN on this channel instead of polling).
create or replace function notify_domain_event()
returns trigger
language plpgsql
as $$
begin
  perform pg_notify('domain_events', new.id);
  return new;
end;
$$;

create trigger domain_events_notify
  after insert on domain_events
  for each row execute function notify_domain_event();

commit;
