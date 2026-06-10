package services

import (
	"context"
	"testing"
)

// The planner's deterministic path must be correct with no network/key — it's the
// floor the whole prompt→workflow feature stands on. These tests exercise that path
// directly (overlayLLM is a no-op here because no key is set in the test env).

func TestCompilePrompt_DetectsSourcesAndEmail(t *testing.T) {
	p := CompilePrompt(context.Background(),
		"Every morning pull the latest AI research from arXiv and Reddit r/MachineLearning, "+
			"plus AI news from Google News, and email me at me@example.com", "", "", "")

	wantSources := map[string]bool{"arxiv": true, "reddit": true, "news": true}
	if len(p.Sources) != len(wantSources) {
		t.Fatalf("sources = %v, want %v", p.Sources, wantSources)
	}
	for _, s := range p.Sources {
		if !wantSources[s] {
			t.Errorf("unexpected source %q in %v", s, p.Sources)
		}
	}
	// A fetcher per source + the Editor.
	if len(p.Nodes) != len(wantSources)+1 {
		t.Fatalf("nodes = %d, want %d", len(p.Nodes), len(wantSources)+1)
	}
	editor := p.Nodes[len(p.Nodes)-1]
	if editor.Key != "editor" || len(editor.DependsOn) != len(wantSources) {
		t.Errorf("editor node wrong: %+v", editor)
	}
	if got := p.DefaultInput["email"]; got != "me@example.com" {
		t.Errorf("email = %v, want me@example.com", got)
	}
	if p.Schedule == nil || *p.Schedule != "daily 08:00" {
		t.Errorf("schedule = %v, want daily 08:00", p.Schedule)
	}
	subs, ok := p.DefaultInput["subreddits"].([]any)
	if !ok || len(subs) != 1 || subs[0] != "MachineLearning" {
		t.Errorf("subreddits = %v, want [MachineLearning]", p.DefaultInput["subreddits"])
	}
}

func TestCompilePrompt_AlwaysRunnable(t *testing.T) {
	// A vague prompt with no recognizable source must still yield a runnable graph
	// (never an empty graph), so creation can never produce a dead workflow.
	p := CompilePrompt(context.Background(), "keep me informed", "My Flow", "", "")
	if len(p.Nodes) < 2 {
		t.Fatalf("expected a fallback graph, got %d nodes", len(p.Nodes))
	}
	if p.Name != "My Flow" {
		t.Errorf("explicit name not honored: %q", p.Name)
	}
}

func TestCompilePrompt_ExplicitURLsGoToBrowser(t *testing.T) {
	p := CompilePrompt(context.Background(),
		"summarize what's new on https://example.com/blog", "", "", "")
	hasWeb := false
	for _, s := range p.Sources {
		if s == "web" {
			hasWeb = true
		}
	}
	if !hasWeb {
		t.Fatalf("expected a web/browser source, got %v", p.Sources)
	}
	urls, ok := p.DefaultInput["urls"].([]any)
	if !ok || len(urls) == 0 || urls[0] != "https://example.com/blog" {
		t.Errorf("urls = %v, want the extracted URL", p.DefaultInput["urls"])
	}
}
