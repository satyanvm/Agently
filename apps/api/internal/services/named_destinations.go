package services

// named_destinations.go: when a prompt NAMES where something should go ("save
// it in a Google Doc", "post it on X", "on LinkedIn"), that integration is
// always a planner candidate. The embedding prefilter and the cheap router
// can drop it otherwise — the eval (cmd/planeval -graphs) caught the planner
// falling back to a report instead of a Google Doc, and to a raw HTTP call
// instead of the X integration.
//
// Deliberately a short, explicit list of unambiguous product names (not every
// piece's display name): words like "monday", "notion" in a sentence or "x"
// on its own would pin integrations nobody asked for.

import (
	"regexp"
	"strings"
)

var namedDestinations = []struct {
	pattern *regexp.Regexp
	cluster string
}{
	{regexp.MustCompile(`\bgoogle ?docs?\b`), "pieces.google-docs"},
	{regexp.MustCompile(`\bgoogle ?sheets?\b|\bspreadsheet\b`), "pieces.google-sheets"},
	{regexp.MustCompile(`\bgmail\b`), "pieces.gmail"},
	{regexp.MustCompile(`\blinked ?in\b`), "pieces.linkedin"},
	{regexp.MustCompile(`\btweets?\b|\btwitter\b|\b(?:on|to) x\b|\bx \(twitter\)`), "pieces.twitter"},
	{regexp.MustCompile(`\bwordpress\b`), "pieces.wordpress"},
	{regexp.MustCompile(`\bwebflow\b`), "pieces.webflow"},
	{regexp.MustCompile(`\bghost\b`), "pieces.ghostcms"},
	{regexp.MustCompile(`\bhubspot\b`), "pieces.hubspot"},
	{regexp.MustCompile(`\bmailchimp\b`), "pieces.mailchimp"},
	{regexp.MustCompile(`\bnotion\b`), "pieces.notion"},
	{regexp.MustCompile(`\bbuffer\b`), "pieces.buffer"},
	{regexp.MustCompile(`\btypefully\b`), "pieces.typefully"},
}

// namedClusters lists the clusters a prompt names, in the order above.
func namedClusters(prompt string, available map[string]bool) []string {
	lower := strings.ToLower(prompt)
	var out []string
	for _, d := range namedDestinations {
		if available[d.cluster] && d.pattern.MatchString(lower) {
			out = append(out, d.cluster)
		}
	}
	return out
}

// pinNamed puts the named clusters first in routed (most relevant: the user
// said so), keeps the router's order for the rest, and trims to max.
func pinNamed(routed, named []string, max int) []string {
	if len(named) == 0 {
		return routed
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(routed)+len(named))
	for _, k := range append(append([]string{}, named...), routed...) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}
