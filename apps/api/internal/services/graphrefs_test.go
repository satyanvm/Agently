package services

import (
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

func refGraph(llmPrompt string) []domain.GraphNode {
	return []domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "fetch", Type: "hackernews.topStories", DependsOn: []string{"t"}},
		{Key: "sum", Type: "agent.llm", DependsOn: []string{"fetch"},
			Config: map[string]any{"prompt": llmPrompt}},
	}
}

func TestValidateRefs(t *testing.T) {
	cat := LoadCatalog()
	inputs := map[string]bool{"topic": true}
	for _, tc := range []struct {
		prompt, wantErr string // wantErr "" == valid
	}{
		{"Summarize {{outputs.fetch.body}} about {{input.topic}}", ""},
		{"{{ json outputs.fetch.body }}", ""},
		// The exact references qwen3:8b invented in real runs:
		{"{{inputs.top_stories}}", "unknown root 'inputs'"},
		{"{{outputs.fetch.stories}}", "has no output 'stories'"},
		{"{{input.count}}", "input.count is not in defaultInput"},
		{"{{outputs.t.input}}", "not in its dependsOn"},
		{"{{outputs.nope.x}}", "there is no node 'nope'"},
	} {
		errs := validateRefs(refGraph(tc.prompt), cat, inputs)
		got := strings.Join(errs, "; ")
		if tc.wantErr == "" && got != "" {
			t.Errorf("%q: want valid, got %s", tc.prompt, got)
		}
		if tc.wantErr != "" && !strings.Contains(got, tc.wantErr) {
			t.Errorf("%q: want error containing %q, got %q", tc.prompt, tc.wantErr, got)
		}
	}
}

func TestValidateRefs_LoopItems(t *testing.T) {
	cat := LoadCatalog()
	g := []domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "fetch", Type: "hackernews.topStories", DependsOn: []string{"t"}},
		{Key: "each", Type: "logic.loop", DependsOn: []string{"fetch"},
			Config: map[string]any{"items": "outputs.fetch.stories"}},
	}
	got := strings.Join(validateRefs(g, cat, nil), "; ")
	if !strings.Contains(got, "has no output 'stories'") {
		t.Fatalf("loop items must be checked like templates; got %q", got)
	}
}

func TestValidateGraph_DottedKeyRejected(t *testing.T) {
	// qwen3:8b named a node after its type; outputs.<key>.<field> can't address it.
	g := []domain.GraphNode{{Key: "hackernews.fetch_top_stories", Type: "trigger.manual"}}
	got := strings.Join(validateGraph(g, LoadCatalog()), "; ")
	if !strings.Contains(got, "no dots") {
		t.Fatalf("want a key-format error, got %q", got)
	}
}

func TestValidateRefs_WebhookInputIsOpen(t *testing.T) {
	cat := LoadCatalog()
	g := []domain.GraphNode{
		{Key: "hook", Type: "trigger.webhook"},
		{Key: "sum", Type: "agent.llm", DependsOn: []string{"hook"},
			Config: map[string]any{"prompt": "{{input.anything}}"}},
	}
	if errs := validateRefs(g, cat, nil); len(errs) != 0 {
		t.Fatalf("webhook payloads are arbitrary; want no input errors, got %v", errs)
	}
}

func TestValidateRefs_PieceResultOutput(t *testing.T) {
	cat := LoadCatalog()
	var pieceID string
	for id, n := range cat.ByID {
		if n.Runtime == "piece" && n.Kind == "action" {
			pieceID = id
			break
		}
	}
	if pieceID == "" {
		t.Skip("no pieces index in this checkout")
	}
	g := []domain.GraphNode{
		{Key: "p", Type: pieceID},
		{Key: "sum", Type: "agent.llm", DependsOn: []string{"p"},
			Config: map[string]any{"prompt": "{{outputs.p.result}} {{outputs.p.stories}}"}},
	}
	got := strings.Join(validateRefs(g, cat, nil), "; ")
	if !strings.Contains(got, "has no output 'stories'") || strings.Contains(got, "'result'") {
		t.Fatalf("piece actions expose exactly `result`; got %q", got)
	}
}
