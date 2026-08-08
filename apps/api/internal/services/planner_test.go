package services

import (
	"context"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

// A missing provider must be visible to the caller. Structural validation, layout
// and routing logic remain pure and are tested directly.
//
// These replace two tests that asserted the OPPOSITE: that a keyless CompilePrompt
// still returned a valid runnable graph. It did — a fixed trigger→research→report
// shape — and that was the bug, not the feature.

func TestCompilePrompt_MissingKeyNamesTheKey(t *testing.T) {
	// Each credential must name ITSELF, so an operator with one of the two set
	// learns which one is missing rather than "compilation failed".
	for _, tc := range []struct{ anthropic, voyage, want string }{
		{"", "vk", "ANTHROPIC_API_KEY"},
		{"sk-ant-test", "", "VOYAGE_API_KEY"},
	} {
		t.Setenv("ANTHROPIC_API_KEY", tc.anthropic)
		t.Setenv("VOYAGE_API_KEY", tc.voyage)
		_, err := CompilePrompt(context.Background(),
			"Every morning research the latest AI papers and email me at me@example.com", "", "", "")
		if err == nil {
			t.Fatalf("%s unset: expected an error, got a compiled plan", tc.want)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s unset: error must name it, got %v", tc.want, err)
		}
	}
}

func TestCompilePrompt_FailuresAreNotCached(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("VOYAGE_API_KEY", "")
	const prompt = "keep me informed"
	if _, err := CompilePrompt(context.Background(), prompt, "My Flow", "", ""); err == nil {
		t.Fatal("expected a configuration error")
	}
	// A transient failure must not pin itself to the prompt: the same prompt has
	// to be retried for real once the key is set, not served from cache.
	planCache.Lock()
	_, cached := planCache.m[prompt+"\x00My Flow\x00\x00"]
	planCache.Unlock()
	if cached {
		t.Fatal("a failed compile must not populate the plan cache")
	}
}

func TestLoadCatalog_HasBuiltinsAndIntegrations(t *testing.T) {
	cat := LoadCatalog()
	for _, id := range []string{"trigger.manual", "agent.llm", "tool.http", "logic.loop", "output.report"} {
		if _, ok := cat.ByID[id]; !ok {
			t.Errorf("builtin %q missing from catalog", id)
		}
	}
	// The generated catalog should be present in the repo checkout; tolerate a
	// stripped deployment (builtin-only) but require builtins either way.
	if len(cat.ByID) < 15 {
		t.Fatalf("catalog too small: %d types", len(cat.ByID))
	}
}

func TestValidateGraph_CatchesStructuralErrors(t *testing.T) {
	cat := LoadCatalog()
	nodes := []domain.GraphNode{
		{Key: "a", Type: "agent.llm", Config: map[string]any{"prompt": "x"}, DependsOn: []string{"b"}},
		{Key: "b", Type: "agent.llm", Config: map[string]any{"prompt": "x"}, DependsOn: []string{"a"}},
		{Key: "b", Type: "nope.unknown", DependsOn: []string{"ghost"}},
	}
	errs := validateGraph(nodes, cat)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"duplicate node key: b", "unknown type", "unknown node 'ghost'", "cycle detected"} {
		if !strings.Contains(joined, want) {
			t.Errorf("validation missing %q in:\n%s", want, joined)
		}
	}

	valid := []domain.GraphNode{
		{Key: "t", Type: "trigger.manual", Config: map[string]any{}},
		{Key: "r", Type: "agent.llm", Config: map[string]any{"prompt": "go"}, DependsOn: []string{"t"}},
	}
	if errs := validateGraph(valid, cat); len(errs) != 0 {
		t.Errorf("valid graph rejected: %v", errs)
	}
}

func TestValidateGraph_RequiredConfig(t *testing.T) {
	cat := LoadCatalog()
	nodes := []domain.GraphNode{
		{Key: "a", Type: "agent.llm", Config: map[string]any{}}, // prompt is required
	}
	errs := validateGraph(nodes, cat)
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, " "), "required config 'prompt'") {
		t.Errorf("missing required config not caught: %v", errs)
	}
}

func TestLayoutGraph_DepthColumns(t *testing.T) {
	nodes := layoutGraph([]domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "a", DependsOn: []string{"t"}},
		{Key: "b", DependsOn: []string{"t"}},
		{Key: "join", DependsOn: []string{"a", "b"}},
	})
	byKey := map[string]domain.GraphNode{}
	for _, n := range nodes {
		byKey[n.Key] = n
	}
	if byKey["t"].Col != 0 || byKey["a"].Col != 1 || byKey["b"].Col != 1 || byKey["join"].Col != 2 {
		t.Errorf("cols wrong: t=%d a=%d b=%d join=%d",
			byKey["t"].Col, byKey["a"].Col, byKey["b"].Col, byKey["join"].Col)
	}
	if byKey["a"].Row == byKey["b"].Row {
		t.Errorf("parallel nodes share a row: %d", byKey["a"].Row)
	}
}

func TestServicesOf_SkipsBuiltins(t *testing.T) {
	got := servicesOf([]domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "s", Type: "slack.sendMessage"},
		{Key: "g", Type: "github.createIssue"},
		{Key: "g2", Type: "github.listIssues"},
		{Key: "o", Type: "output.report"},
	})
	want := []string{"github", "slack"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("servicesOf = %v, want %v", got, want)
	}
}

func TestScheduleFrom_TimeParsing(t *testing.T) {
	cases := map[string]string{
		"every day at 9:30pm": "daily 21:30",
		"each morning":        "daily 09:00",
		"hourly":              "hourly",
	}
	for prompt, want := range cases {
		got := scheduleFrom(prompt)
		if got == nil || *got != want {
			t.Errorf("scheduleFrom(%q) = %v, want %q", prompt, got, want)
		}
	}
	if got := scheduleFrom("just once please"); got != nil {
		t.Errorf("scheduleFrom(no cadence) = %v, want nil", *got)
	}
}
