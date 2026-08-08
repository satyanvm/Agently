package services

// piecesrouter.go selects which catalog clusters enter the map phase — both
// pieces.<slug> clusters and the hand-written dozen.
//
// The cluster universe (~700 pieces + 12 hand-written) is far too large for
// one map call per cluster, but NOT too large to describe whole: one line per
// cluster listing every action label is only ~35k tokens. So selection is a
// single small-model ROUTING call over that full directory — semantic, unlike
// the removed lexical term-overlap prescreen.
//
// The directory is prefiltered by the embedding sidecar (piecesembed.go) to the
// top-scoring pieces first, shrinking the router prompt (clusters absent from
// the sidecar — the hand-written ones — ride along unranked). Neither stage
// degrades: a missing sidecar or a router failure fails the compile with its
// reason. The lexical term-overlap prescreen that used to catch these is gone;
// it could not see synonyms ("spreadsheet" never found google-sheets), so
// falling back to it produced a confidently wrong graph rather than an error.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// routerPrefilterTop bounds the directory when embeddings are available: the
// top-N pieces by best-matching action similarity. Generous on purpose — the
// prefilter exists for recall, the router for precision.
const routerPrefilterTop = 100

// routerTimeout allows for the larger directory prompt (~35k tokens full,
// ~5k prefiltered) while keeping plan latency interactive.
const routerTimeout = 20 * time.Second

// customAPICall is boilerplate present in most pieces; stated once in the
// directory header instead of repeated ~455 times.
const customAPICall = "Custom API Call"

// clusterDirectory renders one entry per cluster: slug, display name,
// categories, then EVERY action label (and trigger labels, marked) — no
// truncation, so no capability is invisible to the router. Keys are rendered
// in the given order (prefilter relevance order, or alphabetical for the full
// directory). Hand-written cluster keys ("communication", …) render the same
// way; their key doubles as the slug.
func clusterDirectory(cat *Catalog, keys []string) string {
	var b strings.Builder
	b.WriteString("Every piece also exposes a \"" + customAPICall + "\" action (arbitrary authenticated call to that service's API); it is omitted from the lists below.\n\n")
	for _, key := range keys {
		f, ok := cat.Clusters[key]
		if !ok {
			continue
		}
		slug := strings.TrimPrefix(key, "pieces.")
		b.WriteString(slug)
		b.WriteString(" — ")
		b.WriteString(strings.TrimSuffix(f.Label, " (Activepieces)"))
		if len(f.Categories) > 0 {
			b.WriteString(" [" + strings.Join(f.Categories, ", ") + "]")
		}
		b.WriteString(": ")
		var actions, triggers []string
		for _, n := range f.Nodes {
			if n.Label == customAPICall {
				continue
			}
			if n.Kind == "trigger" {
				triggers = append(triggers, n.Label)
			} else {
				actions = append(actions, n.Label)
			}
		}
		b.WriteString(strings.Join(actions, "; "))
		if len(triggers) > 0 {
			if len(actions) > 0 {
				b.WriteString(" | ")
			}
			b.WriteString("triggers: " + strings.Join(triggers, "; "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// routeClusters picks up to max cluster keys (piece AND hand-written) for the
// map phase with one small-model call over the cluster directory. The returned
// order is the model's own ranking — downstream (capFairly) uses it to decide
// which cluster contributes the marginal node, so it is deliberately NOT
// re-sorted. Any failure (no key, timeout, junk output, nothing valid) is
// returned as an error and fails the compile.
func routeClusters(ctx context.Context, cat *Catalog, prompt string, clusterKeys []string, max int) ([]string, error) {
	if len(clusterKeys) == 0 {
		return nil, nil // nothing to route is a valid answer, not a failure
	}

	// Recall layer: embeddings shrink the directory before the router reads it.
	keys, err := prefilterPieceClusters(ctx, cat, prompt, clusterKeys, routerPrefilterTop)
	if err != nil {
		return nil, fmt.Errorf("piece prefilter: %w", err)
	}
	if len(keys) == 0 {
		keys = clusterKeys
	}

	system := `You route a user's automation request to integration clusters (each is one service
or capability area, offering the listed actions/triggers).
Given the request and a directory of clusters (one entry per cluster: "slug — Name [categories]: action labels | triggers: ..."),
return JSON: {"clusters": ["slug", ...]} listing ONLY slugs from the directory whose actions or
triggers could plausibly serve the request, MOST relevant first. Include every plausibly relevant
cluster (a later stage narrows precisely), but never pad with irrelevant ones. At most ` + itoa(max) + `.
Return {"clusters": []} if none apply.`

	user := "Request: " + prompt + "\n\nCluster directory:\n" + clusterDirectory(cat, keys)
	raw, err := planLLMWith(ctx, mapModel(), 1024, routerTimeout, system, []llmMsg{{Role: "user", Content: user}})
	if err != nil {
		return nil, fmt.Errorf("cluster router: %w", err)
	}
	var out struct {
		Clusters []string `json:"clusters"`
		Pieces   []string `json:"pieces"` // tolerated: the field name used by an earlier prompt
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
		return nil, fmt.Errorf("cluster router returned unparseable JSON: %w", err)
	}
	picked := out.Clusters
	if len(picked) == 0 {
		picked = out.Pieces
	}

	// Validate against the offered directory (not the whole catalog): the model
	// may only pick from what it was shown. A hand-written cluster's slug IS its
	// key; a piece slug maps to pieces.<slug>.
	offered := map[string]bool{}
	for _, k := range keys {
		offered[k] = true
	}
	var selected []string
	seen := map[string]bool{}
	for _, slug := range picked {
		key := strings.TrimSpace(slug)
		if !offered[key] {
			key = "pieces." + strings.TrimPrefix(key, "pieces.")
		}
		if offered[key] && !seen[key] {
			seen[key] = true
			selected = append(selected, key)
			if len(selected) == max {
				break
			}
		}
	}
	if len(selected) == 0 && len(picked) > 0 {
		// The model answered but named nothing in the directory it was shown.
		return nil, fmt.Errorf("cluster router picked %d cluster(s), none of them in the offered directory: %s",
			len(picked), truncateStr(strings.Join(picked, ", "), 200))
	}
	return selected, nil
}

// PieceSelectionMethods runs each piece-selection strategy against prompt and
// returns slug lists keyed by method: "embeddings" (the prefilter alone) and
// "router" (the primary path, prefilter + model). Restricted to piece clusters
// so the recall numbers stay comparable across strategies. For cmd/planeval's
// recall measurement — compilation itself uses mapPhase.
//
// A strategy that errors is omitted from the map and reported through errs, so
// planeval can print WHY a row is missing instead of scoring it as zero recall.
func PieceSelectionMethods(ctx context.Context, prompt string, max int) (map[string][]string, map[string]error) {
	cat := LoadCatalog()
	var pieceKeys []string
	for _, key := range cat.sortedClusters() {
		if strings.HasPrefix(key, "pieces.") {
			pieceKeys = append(pieceKeys, key)
		}
	}
	slugs := func(keys []string) []string {
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = strings.TrimPrefix(k, "pieces.")
		}
		return out
	}
	out := map[string][]string{}
	errs := map[string]error{}
	if top, err := prefilterPieceClusters(ctx, cat, prompt, pieceKeys, max); err != nil {
		errs["embeddings"] = err
	} else {
		out["embeddings"] = slugs(top)
	}
	if sel, err := routeClusters(ctx, cat, prompt, pieceKeys, max); err != nil {
		errs["router"] = err
	} else {
		out["router"] = slugs(sel)
	}
	return out, errs
}
