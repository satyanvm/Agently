package services

// publishing.go enforces "nothing publishes without human approval": every
// node that publishes publicly (packages/nodes/publishing.json — social,
// blog/CMS, community, newsletter sends, social schedulers) must have a
// human.approval node among its ancestors. Checked in three places:
//
//   - validateGraph (nodecatalog.go): the planner's repair loop gets the
//     error and has to add the approval;
//   - SaveGraph (workflow_service.go): the builder can't save such a graph;
//   - the reasoner, when a run loads its graph (reasoner/publishing.py), so a
//     graph saved before this rule existed can't publish either.
//
// A missing or unreadable list fails closed: the rule can't be checked, so
// nothing validates and the server refuses to start (RequirePublishingList).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/agently/api/internal/domain"
)

const approvalType = "human.approval"

type publishingList struct {
	Pieces map[string][]string `json:"pieces"`
	Types  []string            `json:"types"`
}

var (
	publishingOnce sync.Once
	publishingSet  map[string]bool
	publishingErr  error
)

func publishingPath() string {
	if dir := catalogDir(); dir != "" {
		return filepath.Join(filepath.Dir(dir), "publishing.json")
	}
	return ""
}

func loadPublishing() (map[string]bool, error) {
	publishingOnce.Do(func() {
		path := publishingPath()
		if path == "" {
			publishingErr = fmt.Errorf("packages/nodes/publishing.json not found (no node catalog directory)")
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			publishingErr = fmt.Errorf("read %s: %w", path, err)
			return
		}
		var list publishingList
		if err := json.Unmarshal(raw, &list); err != nil {
			publishingErr = fmt.Errorf("parse %s: %w", path, err)
			return
		}
		set := map[string]bool{}
		for slug, actions := range list.Pieces {
			for _, a := range actions {
				set["pieces."+slug+"."+a] = true
			}
		}
		for _, t := range list.Types {
			set[t] = true
		}
		if len(set) == 0 {
			publishingErr = fmt.Errorf("%s lists no publishing node types", path)
			return
		}
		publishingSet = set
	})
	return publishingSet, publishingErr
}

// RequirePublishingList is a startup preflight (cmd/server/main.go).
func RequirePublishingList() error {
	_, err := loadPublishing()
	return err
}

// isPublishing reports whether a node type publishes publicly.
func isPublishing(nodeType string) bool {
	set, err := loadPublishing()
	return err == nil && set[nodeType]
}

// publishingErrors lists every publishing node with no human.approval
// upstream, phrased so the planner's model (or a person) can fix it.
func publishingErrors(nodes []domain.GraphNode) []string {
	if _, err := loadPublishing(); err != nil {
		return []string{"cannot check which nodes publish: " + err.Error()}
	}
	byKey := map[string]domain.GraphNode{}
	for _, n := range nodes {
		byKey[n.Key] = n
	}
	hasApprovalAbove := func(key string) bool {
		seen := map[string]bool{}
		stack := append([]string(nil), byKey[key].DependsOn...)
		for len(stack) > 0 {
			k := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[k] {
				continue
			}
			seen[k] = true
			n, ok := byKey[k]
			if !ok {
				continue
			}
			if n.Type == approvalType {
				return true
			}
			stack = append(stack, n.DependsOn...)
		}
		return false
	}
	var errs []string
	for _, n := range nodes {
		if !isPublishing(n.Type) || hasApprovalAbove(n.Key) {
			continue
		}
		errs = append(errs, fmt.Sprintf(
			"node '%s' (%s) publishes publicly, so it must come after a %s node: add one whose "+
				"config.draft is the text to publish (e.g. \"{{outputs.<drafting node>.text}}\"), make '%s' "+
				"depend on it, and publish {{outputs.<approval node>.content}} (the approved, possibly edited text)",
			n.Key, n.Type, approvalType, n.Key))
	}
	sort.Strings(errs)
	return errs
}

// PublishingTypes lists the publishing node types among ids (for the
// planner prompt: it is told which of ITS candidate nodes need approval).
func PublishingTypes(ids []string) []string {
	var out []string
	for _, id := range ids {
		if isPublishing(id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func joinErrors(errs []string) string { return strings.Join(errs, "; ") }
