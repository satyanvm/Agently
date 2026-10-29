package platform

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Whole-file migrations applied at startup, embedded from schema/. Each is a
// byte-for-byte copy of its packages/db/migrations file (schema_test.go checks
// that), idempotent (`if not exists`), and runs in its own transaction.
//
//go:embed schema/0015_marketing_loop.sql
var migration0015 string

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
	// 0016_run_cancel_relay.sql
	`alter table runs add column if not exists cancel_signaled_at timestamptz`,
}

// EnsureSchema applies schemaUpgrades, then the embedded whole-file migrations.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	for _, stmt := range schemaUpgrades {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	// No arguments → pgx uses the simple protocol, which accepts the file's
	// several statements (and its begin/commit) in one call.
	if _, err := pool.Exec(ctx, migration0015); err != nil {
		return fmt.Errorf("apply 0015_marketing_loop.sql: %w", err)
	}
	return nil
}
