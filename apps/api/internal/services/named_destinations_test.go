package services

import (
	"reflect"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

func TestNamedDestinations(t *testing.T) {
	all := map[string]bool{}
	for _, d := range namedDestinations {
		all[d.cluster] = true
	}
	for prompt, want := range map[string][]string{
		"save a blog outline for each into a Google Doc":              {"pieces.google-docs"},
		"draft a tweet and post it on X after marketing approves":     {"pieces.twitter"},
		"post it on our LinkedIn page and keep a copy in google docs": {"pieces.google-docs", "pieces.linkedin"},
		"save it as a Gmail draft":                                    {"pieces.gmail"},
		"check the box next to each item":                             {}, // no "x" on its own
		"track mentions on Hacker News, alert the team on Slack":      {},
		"publish to our ghost blog and send the mailchimp campaign":   {"pieces.ghostcms", "pieces.mailchimp"},
	} {
		got := namedClusters(prompt, all)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("namedClusters(%q) = %v, want %v", prompt, got, want)
		}
	}
	// Named clusters go first, nothing is duplicated, and max still holds.
	got := pinNamed([]string{"pieces.reddit", "pieces.google-docs", "pieces.rss"}, []string{"pieces.google-docs", "pieces.twitter"}, 3)
	if !reflect.DeepEqual(got, []string{"pieces.google-docs", "pieces.twitter", "pieces.reddit"}) {
		t.Fatalf("pinNamed = %v", got)
	}
	if got := namedClusters("post it on LinkedIn", map[string]bool{}); len(got) != 0 {
		t.Fatalf("an integration that isn't installed can't be pinned: %v", got)
	}
}

// The credential check (credavail.go) lets a step without a login stay in
// the graph when the prompt names its service. "On X" and "a Google Doc"
// name twitter and google-docs, even though neither spells the slug.
func TestPromptNamesServiceUsesNamedDestinations(t *testing.T) {
	cat := LoadCatalog()
	tweet, docs := cat.ByID["pieces.twitter.create-tweet"], cat.ByID["pieces.google-docs.create_document"]
	if !promptNamesService("draft a tweet and post it on x after approval", tweet, cat) {
		t.Error(`"on X" should name twitter`)
	}
	if !promptNamesService("save an outline into a google doc", docs, cat) {
		t.Error(`"a Google Doc" should name google-docs`)
	}
	if promptNamesService("summarise the news and email me", tweet, cat) {
		t.Error("a prompt that doesn't mention X must not keep the twitter step")
	}
}

func TestRawHTTPPublishingRejected(t *testing.T) {
	base := []domain.GraphNode{{Key: "t", Type: "trigger.manual"},
		{Key: "ok", Type: "human.approval", DependsOn: []string{"t"}, Config: map[string]any{"draft": "x"}}}
	post := append(base, domain.GraphNode{Key: "publish", Type: "tool.http", DependsOn: []string{"ok"},
		Config: map[string]any{"method": "POST", "url": "https://api.x.com/2/tweets"}})
	if errs := strings.Join(publishingErrors(post), "; "); !strings.Contains(errs, "publishes through a raw API call") {
		t.Fatalf("raw POST to X should be rejected even after an approval, got %q", errs)
	}
	read := append(base, domain.GraphNode{Key: "read", Type: "tool.http", DependsOn: []string{"ok"},
		Config: map[string]any{"method": "GET", "url": "https://api.linkedin.com/v2/me"}})
	if errs := publishingErrors(read); len(errs) > 0 {
		t.Fatalf("a GET is a read, not a publish: %v", errs)
	}
}
