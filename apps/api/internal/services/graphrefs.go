package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/agently/api/internal/domain"
)

// graphrefs.go checks the {{...}} references inside a compiled graph's config
// against what the reasoner will actually have at run time, so a bad reference
// is caught by the reduce repair loop instead of failing the user's first run.
//
// The rules mirror reasoner/nodes.py render(): a node sees the run input and the
// outputs of its DIRECT dependencies only (engine.py / workflow.py build
// `upstream` from dependsOn), and an unknown reference raises.

var tplRef = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// validateRefs returns one human-readable complaint per bad reference. inputKeys
// are the run-input fields the plan seeds (defaultInput); input.* references are
// not checked when the graph starts from a webhook, whose payload is arbitrary.
func validateRefs(nodes []domain.GraphNode, cat *Catalog, inputKeys map[string]bool) []string {
	typeOf := map[string]string{}
	webhook := false
	for _, n := range nodes {
		typeOf[n.Key] = n.Type
		if n.Type == "trigger.webhook" {
			webhook = true
		}
	}
	var errs []string
	seen := map[string]bool{}
	for _, n := range nodes {
		deps := map[string]bool{}
		for _, d := range n.DependsOn {
			deps[d] = true
		}
		refs := configRefs(n.Config)
		if n.Type == "logic.loop" {
			// items is a bare dotted ref ("outputs.fetch.result"), not a template.
			if items, ok := n.Config["items"].(string); ok && strings.TrimSpace(items) != "" {
				refs = append(refs, strings.Trim(strings.TrimSpace(items), "{} "))
			}
		}
		for _, expr := range refs {
			if msg := checkRef(n, expr, deps, typeOf, cat, inputKeys, webhook); msg != "" && !seen[msg] {
				seen[msg] = true
				errs = append(errs, msg)
			}
		}
	}
	return errs
}

func checkRef(n domain.GraphNode, expr string, deps map[string]bool, typeOf map[string]string,
	cat *Catalog, inputKeys map[string]bool, webhook bool) string {
	for _, helper := range []string{"json ", "urlencode "} {
		expr = strings.TrimSpace(strings.TrimPrefix(expr, helper))
	}
	parts := strings.Split(expr, ".")
	where := fmt.Sprintf("node '%s' references {{%s}}", n.Key, expr)
	switch parts[0] {
	case "input":
		if webhook || len(parts) < 2 || inputKeys[parts[1]] {
			return ""
		}
		return where + ": input." + parts[1] + " is not in defaultInput (" + keyList(inputKeys) +
			") — add it to defaultInput or reference an upstream output"
	case "outputs":
		if len(parts) < 2 {
			return where + ": use outputs.<dependency key>.<field>"
		}
		key := parts[1]
		if !deps[key] {
			if _, exists := typeOf[key]; exists {
				return where + ": '" + key + "' is not in its dependsOn — a node only sees its direct dependencies' outputs"
			}
			return where + ": there is no node '" + key + "'"
		}
		if len(parts) < 3 {
			return ""
		}
		spec, ok := cat.ByID[typeOf[key]]
		if !ok || len(spec.Outputs) == 0 {
			return "" // undeclared outputs: nothing to check against
		}
		for _, o := range spec.Outputs {
			if o == parts[2] {
				return ""
			}
		}
		return where + ": '" + typeOf[key] + "' has no output '" + parts[2] + "' (its outputs: " +
			strings.Join(spec.Outputs, ", ") + ")"
	case "item", "loop":
		return "" // loop-body roots, injected by logic.loop
	}
	return where + ": unknown root '" + parts[0] + "' — use input.<field> or outputs.<dependency key>.<field>"
}

// configRefs collects every template expression in string config values,
// descending into nested maps and lists.
func configRefs(v any) []string {
	var out []string
	switch t := v.(type) {
	case string:
		for _, m := range tplRef.FindAllStringSubmatch(t, -1) {
			out = append(out, m[1])
		}
	case map[string]any:
		for _, x := range t {
			out = append(out, configRefs(x)...)
		}
	case []any:
		for _, x := range t {
			out = append(out, configRefs(x)...)
		}
	}
	return out
}

func keyList(m map[string]bool) string {
	if len(m) == 0 {
		return "empty"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
