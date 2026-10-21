package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// The embedded startup migration must stay a byte-for-byte copy of the file
// docker-compose and operators apply, or the two schemas drift.
func TestEmbeddedMigrationMatchesSource(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "db", "migrations", "0015_marketing_loop.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != migration0015 {
		t.Fatal("internal/platform/schema/0015_marketing_loop.sql differs from packages/db/migrations/0015_marketing_loop.sql — copy it again")
	}
}
