-- 0006_run_input.sql — make a run parameterizable.
--
-- A run is no longer a generic placeholder: it carries an `input` jsonb (sources,
-- topic, recipient email, etc.) that the worker's agents read to do real, specific
-- work. This is what turns "run the workflow" into "run THIS task".

begin;

alter table runs add column if not exists input jsonb not null default '{}'::jsonb;

commit;
