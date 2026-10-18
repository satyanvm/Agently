package services

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/agently/api/internal/domain"
)

// credavail.go tells the prompt compiler which credentials the user actually
// HAS, so reduce can prefer nodes that will run over ones that fail on a missing
// login (a local model once picked a paid scraping API over the free built-in
// scraper because nothing distinguished them).
//
// A node's credential counts as available when its env var is set (the runtime
// falls back to the env), or a stored credential row matches it: catalog nodes
// by field key (e.g. ACTIVECAMPAIGN_API_KEY), Activepieces nodes by credential
// TYPE, which is the piece's cluster ("pieces.webscraping-ai") — their stored
// field is just "value", the AP_* name being only the env fallback.

type credKeysCtx struct{}

// WithStoredCredentialKeys attaches the workspace's stored credential markers
// (field keys plus "type:<credential type>") to ctx for CompilePrompt.
func WithStoredCredentialKeys(ctx context.Context, keys []string) context.Context {
	return context.WithValue(ctx, credKeysCtx{}, keys)
}

// credentialAvailability returns a lookup for one compile. Env vars are read
// once here so every node sees the same answer.
func credentialAvailability(ctx context.Context) func(n CatalogNode, key string) bool {
	stored := map[string]bool{}
	if keys, ok := ctx.Value(credKeysCtx{}).([]string); ok {
		for _, k := range keys {
			stored[k] = true
		}
	}
	return func(n CatalogNode, key string) bool {
		return stored[key] || stored["type:"+n.Cluster] || strings.TrimSpace(os.Getenv(key)) != ""
	}
}

// storedCredentialKeys flattens every stored credential into its markers: each
// set field key, plus "type:<credential type>".
func storedCredentialKeys(creds []CredentialSummary) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range creds {
		for _, k := range append([]string{"type:" + c.Type}, c.SetKeys...) {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// credentialFingerprint is the part of the plan-cache key that changes when
// the user adds or removes a credential, so a cached plan built around a
// missing login is not served after they fix it.
func credentialFingerprint(ctx context.Context) string {
	keys, _ := ctx.Value(credKeysCtx{}).([]string)
	return strings.Join(keys, ",")
}

// validateCredentials rejects nodes whose credentials the user lacks, unless
// the prompt names that service — then a clear run-time "missing credential"
// failure is the right outcome (the user asked for Slack; they need a Slack
// login). Advisory prompt text alone did not stop a local 8B model from
// choosing a paid scraping API over the free built-in browser.
func validateCredentials(nodes []domain.GraphNode, cat *Catalog, has func(CatalogNode, string) bool, prompt string) []string {
	lower := strings.ToLower(prompt)
	var errs []string
	for _, gn := range nodes {
		n, ok := cat.ByID[gn.Type]
		if !ok || len(n.Credentials) == 0 {
			continue
		}
		var missing []string
		for _, c := range n.Credentials {
			if !has(n, c.Key) {
				missing = append(missing, c.Key)
			}
		}
		if len(missing) == 0 || promptNamesService(lower, n, cat) {
			continue
		}
		errs = append(errs, fmt.Sprintf("node '%s' (%s) needs credential %s, which the user has NOT configured, "+
			"and the request does not ask for that service — replace it with a node that needs no "+
			"credentials (to read web pages use tool.browser)", gn.Key, gn.Type, strings.Join(missing, ", ")))
	}
	return errs
}

// promptNamesService reports whether the request mentions the node's service:
// its id prefix ("slack" in slack.postMessage), its piece slug, or its cluster
// label ("WebScraping AI"), compared with spaces/dashes removed.
func promptNamesService(lowerPrompt string, n CatalogNode, cat *Catalog) bool {
	squash := func(s string) string {
		return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(strings.ToLower(s))
	}
	prompt := squash(lowerPrompt)
	var names []string
	if strings.HasPrefix(n.Cluster, "pieces.") {
		names = append(names, strings.TrimPrefix(n.Cluster, "pieces."))
		if f, ok := cat.Clusters[n.Cluster]; ok {
			names = append(names, strings.TrimSuffix(f.Label, " (Activepieces)"))
		}
	} else if i := strings.Index(n.ID, "."); i > 0 {
		names = append(names, n.ID[:i])
	}
	for _, name := range names {
		if s := squash(name); len(s) >= 3 && strings.Contains(prompt, s) {
			return true
		}
	}
	return false
}
