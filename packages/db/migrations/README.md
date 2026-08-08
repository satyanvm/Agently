# Database migrations

Migrations are applied in filename order to the shared Postgres database.
`docker-compose.yml` mounts the portable migrations for a clean local database;
Supabase-only RLS/realtime migrations remain available for hosted deployments.

Important current migrations:

- `0001_init.sql`: core entities and run/log/browser tables.
- `0006`-`0008`: run input, workflow defaults, and integrations.
- `0009`-`0010`: historical Temporal/native transition; retained for upgrade order.
- `0011_credentials.sql`: durable credential rows.
- `0012_piece_trigger_state.sql`: durable Activepieces trigger cursors/state.
- `0013_drop_retired_execution_columns.sql`: removes the retired run engine and
  Postgres-queue lease columns.

Apply an upgrade without resetting data:

```bash
docker exec -i agently-postgres psql -U agently -d agently < packages/db/migrations/00XX_name.sql
```

For a disposable local database, `docker compose down -v && docker compose up -d`
recreates the volume and applies the mounted files from scratch.
