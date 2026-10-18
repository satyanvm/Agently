package services

import (
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

// The intel.* nodes are catalog built-ins: a competitor-watch graph must pass
// the same structural and reference checks the planner applies.
func TestIntelNodesValidate(t *testing.T) {
	cat := LoadCatalog()
	g := []domain.GraphNode{
		{Key: "t", Type: "trigger.schedule"},
		{Key: "watch", Type: "intel.watchCompetitors", DependsOn: []string{"t"},
			Config: map[string]any{"pageKinds": "pricing"}},
		{Key: "snap", Type: "intel.snapshotPage", DependsOn: []string{"t"},
			Config: map[string]any{"url": "https://acme.test/changelog", "render": "browser"}},
		{Key: "diff", Type: "intel.diffPage", DependsOn: []string{"snap"},
			Config: map[string]any{"snapshotId": "{{outputs.snap.snapshotId}}"}},
		{Key: "news", Type: "gdelt.searchDocs", DependsOn: []string{"t"},
			Config: map[string]any{"query": "acme"}},
		{Key: "record", Type: "intel.recordSignal", DependsOn: []string{"news"},
			Config: map[string]any{"source": "news", "items": "outputs.news.articles", "titleField": "title", "urlField": "url"}},
		{Key: "gate", Type: "logic.branch", DependsOn: []string{"watch", "diff"},
			Config: map[string]any{"condition": "outputs.watch.changed == true"}},
		{Key: "analyse", Type: "agent.llm", DependsOn: []string{"gate", "watch", "diff"},
			Config: map[string]any{"prompt": "What changed: {{outputs.watch.text}}\n{{outputs.diff.summary}}"}},
	}
	if errs := validateGraph(g, cat); len(errs) > 0 {
		t.Fatalf("validateGraph: %s", strings.Join(errs, "; "))
	}
	if errs := validateRefs(g, cat, map[string]bool{}); len(errs) > 0 {
		t.Fatalf("validateRefs: %s", strings.Join(errs, "; "))
	}

	bad := []domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "snap", Type: "intel.snapshotPage", DependsOn: []string{"t"}, Config: map[string]any{"url": "https://x.test"}},
		{Key: "a", Type: "agent.llm", DependsOn: []string{"snap"}, Config: map[string]any{"prompt": "{{outputs.snap.diff}}"}},
	}
	if errs := strings.Join(validateRefs(bad, cat, map[string]bool{}), "; "); !strings.Contains(errs, "has no output 'diff'") {
		t.Fatalf("expected an unknown-output error, got %q", errs)
	}
	if got := servicesOf(g); len(got) != 1 || got[0] != "gdelt" {
		t.Fatalf("servicesOf should list only integrations, got %v", got)
	}
}
