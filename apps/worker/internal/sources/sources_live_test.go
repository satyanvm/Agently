package sources

import (
	"context"
	"testing"
	"time"
)

// Live network test (skipped in -short). Confirms the real adapters return data.
func TestLiveSources(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name string
		fn   func() ([]Item, error)
	}{
		{"arxiv", func() ([]Item, error) { return ArXiv(ctx, "", 3) }},
		{"hn", func() ([]Item, error) { return HackerNews(ctx, "AI", 3) }},
		{"reddit", func() ([]Item, error) { return Reddit(ctx, "MachineLearning", 3) }},
	} {
		items, err := tc.fn()
		if err != nil {
			// Reddit blocks datacenter/CI IPs (403) — environmental, not a code bug.
			// The agent treats a failing source as non-fatal, so we do too.
			if tc.name == "reddit" {
				t.Logf("reddit unavailable from this IP (expected on CI/datacenter): %v", err)
				continue
			}
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		t.Logf("%s: %d items", tc.name, len(items))
		for _, it := range items {
			t.Logf("   • %s", it.Title)
		}
		if len(items) == 0 && tc.name != "reddit" {
			t.Errorf("%s: expected items, got 0", tc.name)
		}
	}
}
