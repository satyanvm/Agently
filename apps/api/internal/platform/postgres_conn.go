package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed repositories. This is the production storage implementation of
// the same interfaces memory.go implements; services and handlers are unchanged.
// The whole point of the repository seam: swap the store, keep the logic.
//
// Design notes worth understanding:
//   - The interface methods don't take a context or return errors on the read
//     path (they mirror the in-memory store). So query failures are logged and
//     surfaced as zero-value / not-found. That's a deliberate MVP tradeoff,
//     recorded in architecture.md; a later pass threads ctx + errors through.
//   - Timestamps are domain.Timestamp (ISO strings) end to end. On write we pass
//     the string straight to a timestamptz column (Postgres casts it). On read we
//     scan time.Time and format back to ISO. No Date drift either way.
//   - jsonb columns are written as a marshaled JSON string and read as raw bytes
//     then unmarshaled. text[] columns map to Go []string natively in pgx.

// Connect opens a pooled connection to Postgres and verifies it with a ping.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// pgStore holds the connection pool and a logger shared by every repo. Mirrors
// the role of `store` in memory.go, but state lives in Postgres, not a struct.
type pgStore struct {
	pool *pgxpool.Pool
	log  Logger
}

// NewPostgresRepositories builds the full Repositories set backed by Postgres.
func NewPostgresRepositories(pool *pgxpool.Pool, log Logger) *Repositories {
	s := &pgStore{pool: pool, log: log}
	return &Repositories{
		Workspace:     &pgWorkspaceRepo{s},
		Members:       &pgMemberRepo{s},
		Agents:        &pgAgentRepo{s},
		Workflows:     &pgWorkflowRepo{s},
		Versions:      &pgVersionRepo{s},
		Runs:          &pgRunRepo{s},
		RunAgents:     &pgRunAgentRepo{s},
		Messages:      &pgMessageRepo{s},
		Artifacts:     &pgArtifactRepo{s},
		Logs:          &pgLogRepo{s},
		Browser:       &pgBrowserRepo{s},
		Notifications: &pgNotificationRepo{s},
		Activity:      &pgActivityRepo{s},
		Integrations:  &pgIntegrationRepo{s},
		Credentials:   &pgCredentialRepo{s},
	}
}

// bg is the context used by the (context-free) repository methods.
func bg() context.Context { return context.Background() }

func (s *pgStore) fail(op string, err error) {
	if err != nil {
		s.log.Error("postgres query failed", "op", op, "error", err.Error())
	}
}

/* ----------------------- conversion helpers ----------------------- */

// tsOf formats a DB timestamp as an ISO-8601 string.
func tsOf(t time.Time) domain.Timestamp {
	return domain.Timestamp(t.UTC().Format("2006-01-02T15:04:05.000Z"))
}

// tsPtrOf formats a nullable DB timestamp.
func tsPtrOf(t *time.Time) *domain.Timestamp {
	if t == nil {
		return nil
	}
	v := tsOf(*t)
	return &v
}

// anyTs converts a scanned column (pgx yields time.Time for timestamptz) to ISO.
func anyTs(v any) domain.Timestamp {
	if t, ok := v.(time.Time); ok {
		return tsOf(t)
	}
	return ""
}

// anyTsPtr converts a scanned nullable timestamp: nil → nil, time.Time → *ISO.
func anyTsPtr(v any) *domain.Timestamp {
	if t, ok := v.(time.Time); ok {
		ts := tsOf(t)
		return &ts
	}
	return nil
}

// tsArg passes an ISO string to a timestamptz column (Postgres casts text→ts).
func tsArg(t domain.Timestamp) any {
	if t == "" {
		return nil
	}
	return string(t)
}

// tsPtrArg passes a nullable timestamp arg.
func tsPtrArg(t *domain.Timestamp) any {
	if t == nil {
		return nil
	}
	return string(*t)
}

// jsonArg marshals a value for a jsonb column.
func jsonArg(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// jsonInto unmarshals jsonb bytes into dst, tolerating empty/null.
func jsonInto(raw []byte, dst any) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	_ = json.Unmarshal(raw, dst)
}

// strPtr / strFromPtr bridge nullable text columns and *string fields.
func strArg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// toRunAgentIds converts a text[] of ids into typed RunAgentIds.
func toRunAgentIds(ss []string) []domain.RunAgentId {
	out := make([]domain.RunAgentId, len(ss))
	for i, s := range ss {
		out[i] = domain.RunAgentId(s)
	}
	return out
}

// fromRunAgentIds converts typed ids back to a text[] for writes.
func fromRunAgentIds(ids []domain.RunAgentId) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}
