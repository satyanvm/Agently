package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agently/api/internal/domain"
)

// The pieces index (packages/nodes/pieces/index.json, contract §2) merges into
// the catalog as pieces.<slug> clusters. These tests exercise the loader against
// a fixture, its absence-tolerance, role derivation, and that validateGraph
// accepts planner output referencing pieces.* types.

const piecesFixture = `{
  "version": 1,
  "generatedAt": "2026-07-16T00:00:00Z",
  "nodes": [
    {
      "id": "pieces.slack.send_channel_message",
      "piece": "@activepieces/piece-slack",
      "pieceVersion": "0.11.4",
      "action": "send_channel_message",
      "label": "Send message to a channel",
      "description": "Sends a message to a Slack channel.",
      "kind": "action",
      "search": ["slack", "message", "chat"],
      "auth": {"type": "oauth2", "credentialKey": "AP_SLACK_AUTH", "required": true},
      "props": [
        {"key": "channel", "label": "Channel", "type": "dropdown", "required": true, "dynamic": true},
        {"key": "text", "label": "Message text", "type": "long_text", "required": true}
      ]
    },
    {
      "id": "pieces.slack.update_message",
      "piece": "@activepieces/piece-slack",
      "pieceVersion": "0.11.4",
      "action": "update_message",
      "label": "Update message",
      "description": "Updates an existing message.",
      "kind": "action",
      "search": ["slack", "update"],
      "auth": {"type": "oauth2", "credentialKey": "AP_SLACK_AUTH", "required": true},
      "props": [
        {"key": "ts", "label": "Timestamp", "type": "short_text", "required": true}
      ]
    },
    {
      "id": "pieces.brave-search.web_search",
      "piece": "@activepieces/piece-brave-search",
      "pieceVersion": "0.2.1",
      "action": "web_search",
      "label": "Web search",
      "description": "Searches the web with Brave.",
      "kind": "action",
      "search": ["search", "web"],
      "auth": {"type": "secret_text", "credentialKey": "AP_BRAVE_SEARCH_AUTH", "required": true},
      "props": [
        {"key": "query", "label": "Query", "type": "short_text", "required": true}
      ]
    }
  ]
}`

func fixtureCatalog(t *testing.T) *Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte(piecesFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := loadCatalogFrom(catalogDir())
	mergePiecesIndex(cat, path)
	return cat
}

func TestMergePiecesIndex_BuildsClustersAndSpecs(t *testing.T) {
	cat := fixtureCatalog(t)

	n, ok := cat.ByID["pieces.slack.send_channel_message"]
	if !ok {
		t.Fatal("piece node missing from ByID")
	}
	if n.Cluster != "pieces.slack" {
		t.Fatalf("cluster = %q, want pieces.slack", n.Cluster)
	}
	if n.Runtime != "piece" || n.Kind != "action" {
		t.Fatalf("runtime/kind = %q/%q", n.Runtime, n.Kind)
	}
	if len(n.Credentials) != 1 || n.Credentials[0].Key != "AP_SLACK_AUTH" {
		t.Fatalf("credentials = %+v", n.Credentials)
	}
	// Props → config fields, dynamic prop carries the literal-value hint.
	if len(n.Config) != 2 || n.Config[0].Key != "channel" || !n.Config[0].Required {
		t.Fatalf("config = %+v", n.Config)
	}

	// One cluster per piece slug; both slack actions share it.
	slack, ok := cat.Clusters["pieces.slack"]
	if !ok || len(slack.Nodes) != 2 {
		t.Fatalf("pieces.slack cluster = %+v", slack)
	}
	if slack.Label != "Slack (Activepieces)" {
		t.Fatalf("label = %q", slack.Label)
	}
	if _, ok := cat.Clusters["pieces.brave-search"]; !ok {
		t.Fatal("pieces.brave-search cluster missing")
	}
	// Hand-written catalog stays intact alongside.
	if _, ok := cat.Clusters["builtin"]; !ok {
		t.Fatal("builtin cluster lost during merge")
	}
}

func TestMergePiecesIndex_MissingOrBrokenFileIsNoop(t *testing.T) {
	cat := loadCatalogFrom(catalogDir())
	before := len(cat.ByID)

	mergePiecesIndex(cat, "") // no path resolved
	mergePiecesIndex(cat, filepath.Join(t.TempDir(), "nope.json")) // absent

	broken := filepath.Join(t.TempDir(), "broken.json")
	os.WriteFile(broken, []byte("{not json"), 0o644)
	mergePiecesIndex(cat, broken)

	if len(cat.ByID) != before {
		t.Fatalf("catalog changed on noop merges: %d → %d", before, len(cat.ByID))
	}
}

func TestRoleForType_PieceMatchesIntegrationRole(t *testing.T) {
	// Integration catalog types get RoleBrowser (external reach); pieces.* nodes
	// are the same class of thing and must render the same way.
	if got, want := roleForType("pieces.slack.send_channel_message"), roleForType("notion.createPage"); got != want {
		t.Fatalf("pieces role %q != integration role %q", got, want)
	}
	if roleForType("pieces.slack.send_channel_message") != domain.RoleBrowser {
		t.Fatal("pieces.* should derive the integration (browser) role")
	}
}

func TestValidateGraph_AcceptsPieceTypes(t *testing.T) {
	cat := fixtureCatalog(t)
	g := []domain.GraphNode{
		{Key: "t", Type: "trigger.manual", Config: map[string]any{}},
		{Key: "post", Type: "pieces.slack.send_channel_message", DependsOn: []string{"t"},
			Config: map[string]any{"channel": "#general", "text": "hi"}},
	}
	if errs := validateGraph(g, cat); len(errs) != 0 {
		t.Fatalf("valid pieces graph rejected: %v", errs)
	}

	// Required prop enforcement flows through like any catalog node.
	g[1].Config = map[string]any{"channel": "#general"}
	errs := validateGraph(g, cat)
	if len(errs) == 0 {
		t.Fatal("missing required piece prop not caught")
	}
}

func TestServicesOf_PieceSlug(t *testing.T) {
	got := servicesOf([]domain.GraphNode{
		{Key: "a", Type: "pieces.brave-search.web_search"},
		{Key: "b", Type: "slack.sendMessage"},
		{Key: "c", Type: "agent.llm"},
	})
	want := []string{"brave-search", "slack"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("servicesOf = %v, want %v", got, want)
	}
}
