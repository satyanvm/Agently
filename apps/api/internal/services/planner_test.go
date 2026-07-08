package services

import (
	"context"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

// The compiler's deterministic floor must be correct with no network/key — with no
// LLM configured (the test env), CompilePrompt takes the fallback path and must
// still emit a valid TYPED graph. Structural validation, layout and routing logic
// are pure and tested directly.

func TestCompilePrompt_FallbackIsValidTypedGraph(t *testing.T) {
	p := CompilePrompt(context.Background(),
		"Every morning research the latest AI papers and email me at me@example.com", "", "", "")

	if len(p.Nodes) < 3 {
		t.Fatalf("expected fallback graph (trigger→agent→outputs), got %d nodes", len(p.Nodes))
	}
	if errs := validateGraph(p.Nodes, LoadCatalog()); len(errs) > 0 {
		t.Fatalf("fallback graph failed validation: %v", errs)
	}
	for _, n := range p.Nodes {
		if n.Type == "" {
			t.Errorf("node %q has no type — fallback must be fully typed", n.Key)
		}
	}
	// "Every morning" with no explicit time → the default morning slot, 09:00.
	if p.Schedule == nil || *p.Schedule != "daily 09:00" {
		t.Errorf("schedule = %v, want daily 09:00", p.Schedule)
	}
	if got := p.DefaultInput["email"]; got != "me@example.com" {
		t.Errorf("email = %v, want me@example.com", got)
	}
	// The schedule turns the trigger into trigger.schedule; email adds output.email.
	types := map[string]bool{}
	for _, n := range p.Nodes {
		types[n.Type] = true
	}
	if !types["trigger.schedule"] {
		t.Errorf("scheduled prompt should use trigger.schedule, got types %v", types)
	}
	if !types["output.email"] {
		t.Errorf("email in prompt should add output.email, got types %v", types)
	}
}

func TestCompilePrompt_AlwaysRunnable(t *testing.T) {
	// A vague prompt must still yield a runnable graph (never empty), so creation
	// can never produce a dead workflow.
	p := CompilePrompt(context.Background(), "keep me informed", "My Flow", "", "")
	if len(p.Nodes) < 2 {
		t.Fatalf("expected a fallback graph, got %d nodes", len(p.Nodes))
	}
	if p.Name != "My Flow" {
		t.Errorf("explicit name not honored: %q", p.Name)
	}
	if errs := validateGraph(p.Nodes, LoadCatalog()); len(errs) > 0 {
		t.Fatalf("graph failed validation: %v", errs)
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
