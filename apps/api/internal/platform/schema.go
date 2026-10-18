package platform

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// schemaUpgrades are additive, idempotent column additions the code depends
// on, applied at API startup so an existing database never runs against code
// that expects columns it lacks. Each mirrors a file in packages/db/migrations
// (fresh databases get them from there via docker-compose).
var schemaUpgrades = []string{
	// 0014_node_data_and_failure_settings.sql
	`alter table run_agents
	   add column if not exists node_key text,
	   add column if not exists output   jsonb,
	   add column if not exists error    text,
	   add column if not exists attempts int not null default 0`,
	`create index if not exists run_agents_run_node_key_idx on run_agents (run_id, node_key)`,
	`alter table workflows add column if not exists settings jsonb not null default '{}'::jsonb`,
	`alter table runs add column if not exists failure_handled_at timestamptz`,
}

// EnsureSchema applies schemaUpgrades.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	for _, stmt := range schemaUpgrades {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
