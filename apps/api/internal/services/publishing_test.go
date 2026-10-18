package services

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

// Every publishing.json entry must name a real piece action, or the rule
// silently guards nothing for it (a renamed action, a typo).
func TestPublishingListNamesRealActions(t *testing.T) {
	cat := LoadCatalog()
	raw, err := os.ReadFile(publishingPath())
	if err != nil {
		t.Fatal(err)
	}
	var list publishingList
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	for slug, actions := range list.Pieces {
		for _, a := range actions {
			if _, ok := cat.ByID["pieces."+slug+"."+a]; !ok {
				t.Errorf("publishing.json lists pieces.%s.%s, which is not in the pieces index", slug, a)
			}
		}
	}
	for _, typ := range list.Types {
		if _, ok := cat.ByID[typ]; !ok {
			t.Errorf("publishing.json lists %s, which is not a catalog node", typ)
		}
	}
	if err := RequirePublishingList(); err != nil {
		t.Fatal(err)
	}
}

func TestPublishingNeedsApprovalUpstream(t *testing.T) {
	cat := LoadCatalog()
	const post = "pieces.linkedin.create_share_update"
	base := []domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "draft", Type: "agent.llm", DependsOn: []string{"t"}, Config: map[string]any{"prompt": "write", "brand": "true"}},
	}
	with := func(extra ...domain.GraphNode) []domain.GraphNode {
		return append(append([]domain.GraphNode{}, base...), extra...)
	}
	approve := domain.GraphNode{Key: "ok", Type: "human.approval", DependsOn: []string{"draft"},
		Config: map[string]any{"draft": "{{outputs.draft.text}}"}}

	unguarded := with(domain.GraphNode{Key: "publish", Type: post, DependsOn: []string{"draft"}})
	errs := strings.Join(validateGraph(unguarded, cat), "; ")
	if !strings.Contains(errs, "node 'publish' (pieces.linkedin.create_share_update) publishes publicly") ||
		!strings.Contains(errs, "{{outputs.<approval node>.content}}") {
		t.Fatalf("unguarded publish should be rejected with a fixable message, got %q", errs)
	}

	direct := with(approve, domain.GraphNode{Key: "publish", Type: post, DependsOn: []string{"ok"}})
	if errs := publishingErrors(direct); len(errs) > 0 {
		t.Fatalf("approval directly upstream: %v", errs)
	}
	deeper := with(approve,
		domain.GraphNode{Key: "shorten", Type: "agent.llm", DependsOn: []string{"ok"}},
		domain.GraphNode{Key: "publish", Type: post, DependsOn: []string{"shorten"}})
	if errs := publishingErrors(deeper); len(errs) > 0 {
		t.Fatalf("approval two levels up: %v", errs)
	}
	// An approval on ANOTHER branch doesn't guard this one.
	sibling := with(approve, domain.GraphNode{Key: "publish", Type: post, DependsOn: []string{"draft"}})
	if errs := publishingErrors(sibling); len(errs) != 1 {
		t.Fatalf("approval on a sibling branch must not count, got %v", errs)
	}
	// Internal and one-to-one steps need no approval.
	internal := with(
		domain.GraphNode{Key: "slack", Type: "slack.sendMessage", DependsOn: []string{"draft"}},
		domain.GraphNode{Key: "mail", Type: "output.email", DependsOn: []string{"draft"}},
		domain.GraphNode{Key: "gmail", Type: "pieces.gmail.send_email", DependsOn: []string{"draft"}},
		domain.GraphNode{Key: "doc", Type: "pieces.google-docs.append_text", DependsOn: []string{"draft"}},
		domain.GraphNode{Key: "typefully_draft", Type: "pieces.typefully.typefully_create_draft", DependsOn: []string{"draft"}},
	)
	if errs := publishingErrors(internal); len(errs) > 0 {
		t.Fatalf("internal steps flagged: %v", errs)
	}
	if !isPublishing("pieces.mailchimp.send_campaign") || isPublishing("pieces.mailchimp.create_campaign") {
		t.Fatal("sending a campaign publishes; creating one doesn't")
	}
}

func TestSaveGraphRejectsUnguardedPublishing(t *testing.T) {
	plat := NewPlatform(Options{})
	if _, err := plat.Workflows.SaveGraph("ai-digest", []domain.GraphNode{
		{Key: "t", Type: "trigger.manual"},
		{Key: "post", Type: "pieces.twitter.create-tweet", DependsOn: []string{"t"}},
	}); err == nil || !strings.Contains(err.Error(), "publishes publicly") {
		t.Fatalf("expected a rejection, got %v", err)
	}
}

func TestReducePromptCarriesPlaybookRules(t *testing.T) {
	cat := LoadCatalog()
	prompt := reduceSystemPrompt(cat, []string{"pieces.linkedin.create_share_update", "slack.sendMessage"},
		func(CatalogNode, string) bool { return true })
	for _, want := range []string{
		"NOTHING PUBLISHES WITHOUT APPROVAL",
		`config.brand = "true"`,
		"intel.watchCompetitors",
		"THESE NODES PUBLISH PUBLICLY — each needs a human.approval upstream: pieces.linkedin.create_share_update\n",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("reduce prompt is missing %q", want)
		}
	}
}
