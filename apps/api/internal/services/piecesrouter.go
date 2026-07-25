package services

// piecesrouter.go selects which pieces.<slug> clusters enter the map phase.
//
// The piece universe (~700 clusters) is far too large for one map call per
// cluster, but NOT too large to describe whole: one line per piece listing
// every action label is only ~35k tokens. So selection is a single small-model
// ROUTING call over that full directory — semantic, unlike the lexical
// term-overlap prescreen it replaces (kept in planner.go as the fallback).
//
// When an embedding sidecar is present (piecesembed.go), the directory is
// prefiltered to the top-scoring pieces first, shrinking the router prompt;
// with no sidecar the router reads the full directory. Both paths degrade
// silently — a router failure falls back to the lexical prescreen, never
// failing compilation.

import (
	"context"
	"encoding/json"
	"sort"
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

// pieceDirectory renders one entry per piece cluster: slug, display name,
// categories, then EVERY action label (and trigger labels, marked) — no
// truncation, so no capability is invisible to the router. Keys are rendered
// in the given order (prefilter relevance order, or alphabetical for the full
// directory).
func pieceDirectory(cat *Catalog, keys []string) string {
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

// routePieceClusters picks up to max pieces.<slug> cluster keys for the map
// phase with one small-model call over the piece directory. ok=false on any
// failure (no key, timeout, junk output, nothing valid) — the caller falls
// back to the lexical prescreen.
func routePieceClusters(ctx context.Context, cat *Catalog, prompt string, pieceKeys []string, max int) ([]string, bool) {
	if len(pieceKeys) == 0 {
		return nil, true // nothing to route is a valid answer, not a failure
	}

	// Recall layer: embeddings shrink the directory when available.
	keys := pieceKeys
	if top, ok := prefilterPieceClusters(ctx, cat, prompt, pieceKeys, routerPrefilterTop); ok && len(top) > 0 {
		keys = top
	}

	system := `You route a user's automation request to third-party integration pieces.
Given the request and a directory of pieces (one entry per piece: "slug — Name [categories]: action labels | triggers: ..."),
return JSON: {"pieces": ["slug", ...]} listing ONLY slugs from the directory whose actions or
triggers could plausibly serve the request. Include every plausibly relevant piece (a later
stage narrows precisely), but never pad with irrelevant ones. At most ` + itoa(max) + `.
Return {"pieces": []} if none apply.`

	user := "Request: " + prompt + "\n\nPiece directory:\n" + pieceDirectory(cat, keys)
	raw, err := planLLMWith(ctx, mapModel(), 1024, routerTimeout, system, []llmMsg{{Role: "user", Content: user}})
	if err != nil {
		return nil, false
	}
	var out struct {
		Pieces []string `json:"pieces"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &out); err != nil {
		return nil, false
	}

	// Validate against the offered directory (not the whole catalog): the model
	// may only pick from what it was shown.
	offered := map[string]bool{}
	for _, k := range keys {
		offered[k] = true
	}
	var selected []string
	seen := map[string]bool{}
	for _, slug := range out.Pieces {
		key := "pieces." + strings.TrimPrefix(strings.TrimSpace(slug), "pieces.")
		if offered[key] && !seen[key] {
			seen[key] = true
			selected = append(selected, key)
			if len(selected) == max {
				break
			}
		}
	}
	if len(selected) == 0 && len(out.Pieces) > 0 {
		return nil, false // the model answered but with junk — distrust it entirely
	}
	sort.Strings(selected) // stable prompt ordering keeps compilation reproducible
	return selected, true
}

// PieceSelectionMethods runs every piece-selection strategy against prompt and
// returns slug lists keyed by method: "lexical" (fallback prescreen, always),
// "embeddings" (prefilter alone, when the sidecar + key exist), "router" (the
// primary path, when an LLM key exists). For cmd/planeval's recall measurement
// — compilation itself uses mapPhase.
func PieceSelectionMethods(ctx context.Context, prompt string, max int) map[string][]string {
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
	out := map[string][]string{"lexical": slugs(topPieceClusters(cat, prompt, pieceKeys, max))}
	if top, ok := prefilterPieceClusters(ctx, cat, prompt, pieceKeys, max); ok {
		out["embeddings"] = slugs(top)
	}
	if sel, ok := routePieceClusters(ctx, cat, prompt, pieceKeys, max); ok {
		out["router"] = slugs(sel)
	}
	return out
}
