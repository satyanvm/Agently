package services

// nodecatalog.go loads the shared integration-node catalog (packages/nodes/catalog)
// and provides the two things the map-reduce prompt compiler needs from it:
//
//   - per-cluster COMPACT INDEXES ("id — label — description" lines) for the map
//     phase, where a small fast model routes the user's request to relevant nodes;
//   - FULL SCHEMAS (config fields, output fields, credential slots) for the reduce
//     phase, where the big model authors the graph against only the selected nodes.
//
// It also owns structural graph validation (unknown types, missing deps, duplicate
// keys, cycles) — the Go mirror of the reasoner's rules in reasoner/plan.py — and
// the layout pass that assigns (col,row) the same way the reasoner's _grid does.
//
// The catalog is DATA: adding an integration means adding a JSON entry, never code.
// If the catalog directory is missing (stripped deployment), we degrade to the
// built-in fifteen so prompt compilation still works.

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

// CatalogField mirrors one config entry of a node definition (also the shape the
// web inspector renders).
type CatalogField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Control     string   `json:"control"`
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// CatalogCredential names an env var a node resolves at execution time.
type CatalogCredential struct {
	Key  string `json:"key"`
	Help string `json:"help,omitempty"`
}

// CatalogNode is one node definition. The compiler cares about identity, prompt
// text and config/outputs; the execution template (http/browser/code blocks) is
// the reasoner's business and stays opaque here.
type CatalogNode struct {
	ID          string              `json:"id"`
	Label       string              `json:"label"`
	Description string              `json:"description"`
	Kind        string              `json:"kind"`    // trigger|action|logic|output
	Runtime     string              `json:"runtime"` // builtin|http|browser|code|llm
	Search      string              `json:"search"`
	Config      []CatalogField      `json:"config"`
	Outputs     []string            `json:"outputs"`
	Credentials []CatalogCredential `json:"credentials"`
	Cluster     string              `json:"-"` // filled at load from the file
}

type catalogFile struct {
	Cluster     string        `json:"cluster"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	Nodes       []CatalogNode `json:"nodes"`
}

// Catalog is the loaded universe of node types.
type Catalog struct {
	Clusters map[string]catalogFile // cluster key → file (nodes carry Cluster)
	ByID     map[string]CatalogNode
}

var (
	catalogOnce sync.Once
	catalogVal  *Catalog
)

// LoadCatalog loads packages/nodes/catalog once per process. Never fails: with no
// catalog on disk it returns the built-in fallback so compilation always works.
// Activepieces piece actions (packages/nodes/pieces/index.json) merge in as their
// own `pieces.<slug>` clusters — same planning surface, different executor.
func LoadCatalog() *Catalog {
	catalogOnce.Do(func() {
		catalogVal = loadCatalogFrom(catalogDir())
		mergePiecesIndex(catalogVal, piecesIndexPath())
	})
	return catalogVal
}

// catalogDir resolves the catalog location: NODES_CATALOG_DIR wins; otherwise walk
// up from the working directory looking for packages/nodes/catalog (works from the
// repo root, apps/api, or a test's package dir).
func catalogDir() string {
	if v := os.Getenv("NODES_CATALOG_DIR"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "packages", "nodes", "catalog")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func loadCatalogFrom(dir string) *Catalog {
	cat := &Catalog{Clusters: map[string]catalogFile{}, ByID: map[string]CatalogNode{}}
	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				var f catalogFile
				if err := json.Unmarshal(raw, &f); err != nil || f.Cluster == "" {
					continue // a malformed cluster file must not break the platform
				}
				for i := range f.Nodes {
					f.Nodes[i].Cluster = f.Cluster
					if f.Nodes[i].ID == "" {
						continue
					}
					cat.ByID[f.Nodes[i].ID] = f.Nodes[i]
				}
				cat.Clusters[f.Cluster] = f
			}
		}
	}
	// Guarantee the built-in fifteen exist even with no catalog on disk — the
	// compiler can always author trigger/agent/tool/logic/output graphs.
	if _, ok := cat.Clusters["builtin"]; !ok {
		f := builtinFallback()
		for i := range f.Nodes {
			f.Nodes[i].Cluster = f.Cluster
			cat.ByID[f.Nodes[i].ID] = f.Nodes[i]
		}
		cat.Clusters[f.Cluster] = f
	}
	return cat
}

// builtinFallback hardcodes minimal defs for the fifteen code-backed types so a
// deployment without packages/nodes still compiles prompts. Mirrors builtin.json.
func builtinFallback() catalogFile {
	mk := func(id, label, desc, kind string, cfg ...CatalogField) CatalogNode {
		return CatalogNode{ID: id, Label: label, Description: desc, Kind: kind, Runtime: "builtin", Search: id, Config: cfg}
	}
	txt := func(key, label string, req bool) CatalogField {
		return CatalogField{Key: key, Label: label, Control: "text", Required: req}
	}
	area := func(key, label string, req bool) CatalogField {
		return CatalogField{Key: key, Label: label, Control: "textarea", Required: req}
	}
	return catalogFile{
		Cluster: "builtin", Label: "Core",
		Description: "Code-backed platform primitives.",
		Nodes: []CatalogNode{
			mk("trigger.manual", "Manual trigger", "Start on demand; seeds run input.", "trigger"),
			mk("trigger.webhook", "Webhook trigger", "Start from an HTTP request.", "trigger", txt("path", "Path", false)),
			mk("trigger.schedule", "Schedule trigger", "Run on a cron schedule.", "trigger", txt("cron", "Cron", false)),
			mk("agent.llm", "AI agent", "Reason/summarize/extract with an LLM.", "action", area("system", "System prompt", false), area("prompt", "Prompt", true), txt("model", "Model", false)),
			mk("agent.chat", "Chat completion", "Single-turn prompt → response.", "action", area("system", "System prompt", false), area("prompt", "Prompt", true), txt("model", "Model", false)),
			mk("tool.browser", "Browser", "Drive a real browser over URLs.", "action", area("urls", "URLs (one per line)", true)),
			mk("tool.http", "HTTP request", "Call any REST API or URL.", "action", txt("method", "Method", false), txt("url", "URL", true), area("headers", "Headers (JSON)", false), area("body", "Body", false)),
			mk("tool.code", "Run code", "Sandboxed Python/JS snippet.", "action", txt("language", "Language", true), area("source", "Source", true)),
			mk("tool.db", "Database query", "SQL against TOOL_DB_URL.", "action", area("query", "SQL", true)),
			mk("logic.branch", "If / Else", "Gate downstream on a condition.", "logic", txt("condition", "Condition", true)),
			mk("logic.filter", "Filter", "Pass through only when condition holds.", "logic", txt("condition", "Keep when", true)),
			mk("logic.loop", "Loop", "Fan out downstream once per item.", "logic", txt("items", "Items expression", true)),
			mk("output.email", "Send email", "Deliver an email via SMTP seam.", "output", txt("to", "To", true), txt("subject", "Subject", false), area("body", "Body", false)),
			mk("output.slack", "Send to Slack", "Post to a Slack webhook.", "output", txt("webhookUrl", "Webhook URL", false), area("message", "Message", false)),
			mk("output.report", "Generate report", "Compose a markdown/PDF artifact.", "output", txt("title", "Title", true), txt("format", "Format", false)),
		},
	}
}

/* ------------------------------ prompt material ------------------------------ */

// clusterIndex renders the compact map-phase index for one cluster.
func clusterIndex(f catalogFile) string {
	var b strings.Builder
	for _, n := range f.Nodes {
		fmt.Fprintf(&b, "%s — %s — %s\n", n.ID, n.Label, n.Description)
	}
	return b.String()
}

// nodeSchema renders one node's full schema for the reduce prompt: everything the
// model needs to fill the node correctly and reference its outputs downstream.
func nodeSchema(n CatalogNode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s (%s, runtime=%s)\n%s\n", n.ID, n.Label, n.Runtime, n.Description)
	if len(n.Config) > 0 {
		b.WriteString("config:\n")
		for _, f := range n.Config {
			req := ""
			if f.Required {
				req = " (required)"
			}
			opts := ""
			if len(f.Options) > 0 {
				opts = " options=[" + strings.Join(f.Options, ", ") + "]"
			}
			help := f.Help
			if help == "" {
				help = f.Placeholder
			}
			fmt.Fprintf(&b, "  - %s%s%s: %s\n", f.Key, req, opts, help)
		}
	}
	if len(n.Outputs) > 0 {
		fmt.Fprintf(&b, "outputs: %s\n", strings.Join(n.Outputs, ", "))
	}
	if len(n.Credentials) > 0 {
		keys := make([]string, len(n.Credentials))
		for i, c := range n.Credentials {
			keys[i] = c.Key
		}
		fmt.Fprintf(&b, "credentials (env, resolved at run time — degrade to record-intent if unset): %s\n", strings.Join(keys, ", "))
	}
	return b.String()
}

// sortedClusters returns cluster keys with "builtin" first (always in context),
// then alphabetical — stable prompt ordering keeps compilation reproducible.
func (c *Catalog) sortedClusters() []string {
	keys := make([]string, 0, len(c.Clusters))
	for k := range c.Clusters {
		if k != "builtin" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return append([]string{"builtin"}, keys...)
}

/* ------------------------------ graph validation ------------------------------ */

// validateGraph applies the structural rules the reasoner enforces at load time
// (reasoner/plan.py topo_order) plus catalog-type checks, returning human-readable
// error strings the repair loop can feed back to the model. Empty slice == valid.
func validateGraph(nodes []domain.GraphNode, cat *Catalog) []string {
	var errs []string
	if len(nodes) == 0 {
		return []string{"graph has no nodes"}
	}
	byKey := map[string]bool{}
	for _, n := range nodes {
		if strings.TrimSpace(n.Key) == "" {
			errs = append(errs, "a node is missing its key")
			continue
		}
		if byKey[n.Key] {
			errs = append(errs, "duplicate node key: "+n.Key)
		}
		byKey[n.Key] = true
		if n.Type == "" {
			errs = append(errs, "node '"+n.Key+"' has no type")
			continue
		}
		spec, ok := cat.ByID[n.Type]
		if !ok {
			errs = append(errs, "node '"+n.Key+"' uses unknown type '"+n.Type+"'")
			continue
		}
		for _, f := range spec.Config {
			if !f.Required {
				continue
			}
			v, present := n.Config[f.Key]
			if !present || v == nil || fmt.Sprintf("%v", v) == "" {
				errs = append(errs, "node '"+n.Key+"' ("+n.Type+") is missing required config '"+f.Key+"'")
			}
		}
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if !byKey[dep] {
				errs = append(errs, "node '"+n.Key+"' depends on unknown node '"+dep+"'")
			}
		}
	}
	if cycle := findCycle(nodes); cycle != "" {
		errs = append(errs, "cycle detected among nodes: "+cycle)
	}
	return errs
}

// findCycle runs Kahn's algorithm; returns the stuck keys ("a, b") or "".
func findCycle(nodes []domain.GraphNode) string {
	indeg := map[string]int{}
	dependents := map[string][]string{}
	byKey := map[string]bool{}
	for _, n := range nodes {
		byKey[n.Key] = true
		if _, ok := indeg[n.Key]; !ok {
			indeg[n.Key] = 0
		}
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if !byKey[dep] {
				continue // reported separately by validateGraph
			}
			indeg[n.Key]++
			dependents[dep] = append(dependents[dep], n.Key)
		}
	}
	var ready []string
	for k, d := range indeg {
		if d == 0 {
			ready = append(ready, k)
		}
	}
	seen := 0
	for len(ready) > 0 {
		k := ready[0]
		ready = ready[1:]
		seen++
		for _, child := range dependents[k] {
			indeg[child]--
			if indeg[child] == 0 {
				ready = append(ready, child)
			}
		}
	}
	if seen == len(indeg) {
		return ""
	}
	var stuck []string
	for k, d := range indeg {
		if d > 0 {
			stuck = append(stuck, k)
		}
	}
	sort.Strings(stuck)
	return strings.Join(stuck, ", ")
}

/* --------------------------------- layout --------------------------------- */

// layoutGraph assigns (col,row): col = longest-path depth from the roots, row =
// running index within the column. The Go mirror of reasoner/plan.py _grid, so
// prompt-compiled graphs render identically in the builder and the run view.
func layoutGraph(nodes []domain.GraphNode) []domain.GraphNode {
	byKey := map[string]*domain.GraphNode{}
	for i := range nodes {
		byKey[nodes[i].Key] = &nodes[i]
	}
	depth := map[string]int{}
	var colOf func(key string, seen map[string]bool) int
	colOf = func(key string, seen map[string]bool) int {
		if d, ok := depth[key]; ok {
			return d
		}
		n, ok := byKey[key]
		if !ok {
			return 0
		}
		d := 0
		for _, dep := range n.DependsOn {
			if _, exists := byKey[dep]; !exists || seen[dep] {
				continue
			}
			seen[key] = true
			if c := colOf(dep, seen) + 1; c > d {
				d = c
			}
			delete(seen, key)
		}
		depth[key] = d
		return d
	}
	rowPerCol := map[int]int{}
	for i := range nodes {
		c := colOf(nodes[i].Key, map[string]bool{})
		nodes[i].Col = c
		nodes[i].Row = rowPerCol[c]
		rowPerCol[c]++
	}
	return nodes
}

// roleForType derives the run_agent role from a node type's kind prefix, matching
// KIND_ROLE in apps/web/lib/builder-graph.ts and _KIND_ROLE in reasoner/plan.py.
func roleForType(nodeType string) domain.AgentRole {
	switch strings.SplitN(nodeType, ".", 2)[0] {
	case "trigger", "agent":
		return domain.RoleOrchestrator
	case "tool":
		return domain.RoleBrowser
	case "logic":
		return domain.RoleAnalyst
	case "output":
		return domain.RoleWriter
	}
	// Integration nodes ("slack.sendMessage", "github.createIssue", …) execute
	// via tools; browser-tone keeps the run view honest about external reach.
	return domain.RoleBrowser
}
