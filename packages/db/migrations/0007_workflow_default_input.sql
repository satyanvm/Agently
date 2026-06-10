-- 0007_workflow_default_input.sql — give a workflow a reusable default task input.
--
-- A prompt-built workflow captures WHAT to do once (topic, sources, urls, recipient
-- email) so every launch inherits it without re-typing. A run's own `input` (0006)
-- still overrides per-launch — workflow.default_input is the template, run.input is
-- the instance.
begin;

alter table workflows add column if not exists default_input jsonb not null default '{}'::jsonb;

commit;
