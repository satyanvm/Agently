package services

import (
	"context"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
)

func TestCredentialAvailability(t *testing.T) {
	t.Setenv("AP_WEBSCRAPING_AI_AUTH", "")
	t.Setenv("SOME_ENV_ONLY_KEY", "x")
	piece := CatalogNode{ID: "pieces.webscraping-ai.scrapeWebsiteText", Cluster: "pieces.webscraping-ai"}
	catalogNode := CatalogNode{ID: "activecampaign.addContact", Cluster: "crm-marketing"}

	none := credentialAvailability(context.Background())
	if none(piece, "AP_WEBSCRAPING_AI_AUTH") {
		t.Fatal("no env, no stored credential: must be unavailable")
	}
	if !none(catalogNode, "SOME_ENV_ONLY_KEY") {
		t.Fatal("an env var alone counts as available")
	}

	stored := storedCredentialKeys([]CredentialSummary{
		{Type: "pieces.webscraping-ai", SetKeys: []string{"value"}},
		{Type: "activecampaign", SetKeys: []string{"ACTIVECAMPAIGN_API_KEY"}},
	})
	has := credentialAvailability(WithStoredCredentialKeys(context.Background(), stored))
	if !has(piece, "AP_WEBSCRAPING_AI_AUTH") {
		t.Fatal("a stored piece credential matches by type (the piece's cluster)")
	}
	if !has(catalogNode, "ACTIVECAMPAIGN_API_KEY") {
		t.Fatal("a stored catalog credential matches by field key")
	}
}

func TestNodeSchemaMarksMissingCredentials(t *testing.T) {
	n := CatalogNode{ID: "pieces.x.y", Cluster: "pieces.x", Credentials: []CatalogCredential{{Key: "AP_X_AUTH"}}}
	missing := nodeSchema(n, func(CatalogNode, string) bool { return false })
	if !strings.Contains(missing, "NOT CONFIGURED") {
		t.Fatalf("want NOT CONFIGURED, got %q", missing)
	}
	present := nodeSchema(n, func(CatalogNode, string) bool { return true })
	if !strings.Contains(present, "— CONFIGURED") {
		t.Fatalf("want CONFIGURED, got %q", present)
	}
}

func TestValidateCredentials(t *testing.T) {
	cat := LoadCatalog()
	var scraper string
	for id, n := range cat.ByID {
		if n.Cluster == "pieces.webscraping-ai" && len(n.Credentials) > 0 {
			scraper = id
			break
		}
	}
	if scraper == "" {
		t.Skip("no pieces index in this checkout")
	}
	g := []domain.GraphNode{{Key: "scrape", Type: scraper}}
	none := func(CatalogNode, string) bool { return false }

	// The real failure: user asked to scrape articles, never named the service.
	errs := validateCredentials(g, cat, none, "open each story URL and scrape the article text")
	if len(errs) != 1 || !strings.Contains(errs[0], "NOT configured") {
		t.Fatalf("unrequested credentialed node must be rejected, got %v", errs)
	}
	// Naming the service means the user wants it; the run-time error will guide them.
	if errs := validateCredentials(g, cat, none, "scrape it with WebScraping AI"); len(errs) != 0 {
		t.Fatalf("explicitly named service must be allowed, got %v", errs)
	}
	// Configured credentials are always fine.
	all := func(CatalogNode, string) bool { return true }
	if errs := validateCredentials(g, cat, all, "scrape the article"); len(errs) != 0 {
		t.Fatalf("configured credential must be allowed, got %v", errs)
	}
}
