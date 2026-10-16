package services

// preflight.go — "connect these accounts" before a run starts.
//
// A run used to discover a missing token only when it reached the node that
// needed it, after earlier nodes (some with side effects) had already run. The
// preflight walks the workflow's current graph and applies the same resolution
// rules the runtime uses, without executing anything:
//
//   - catalog nodes (reasoner nodes._integration): each declared key comes from
//     the selected credential row, else from the process env; a selected id
//     that no longer exists fails outright.
//   - pieces.* nodes (activities.prepare_piece_node + the pieces worker): a
//     selected row must exist and carry its type's required fields; with none
//     selected, the AP_<SLUG>_AUTH env var must be set.
//   - built-ins that declare a key (tool.db → TOOL_DB_URL) read env only.
//
// Env is read from this process. Every supported setup (pnpm agently, the
// compose stack) loads the same .env into the API, reasoner and pieces worker,
// so what the API sees is what the runtime sees. A deployment that splits env
// per service can turn the check off with RUN_CREDENTIAL_CHECK=off.

import (
	"os"
	"sort"
	"strings"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// credentialIDKey is the reserved node-config key holding the selected
// credential id (docs/credentials-contract.md).
const credentialIDKey = "__credentialId"

// Problems a node can have with its credentials.
const (
	ProblemNotConnected      = "not_connected"      // nothing selected, no env fallback
	ProblemCredentialDeleted = "credential_deleted" // the selected credential no longer exists
	ProblemIncomplete        = "incomplete"         // the selected credential lacks required values
)

// PreflightNode is one graph node that cannot run as configured.
type PreflightNode struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Problem      string   `json:"problem"`
	MissingKeys  []string `json:"missingKeys,omitempty"`
	CredentialID string   `json:"credentialId,omitempty"`
}

// PreflightAccount groups the nodes that need the same account. CredentialType
// is empty for a server setting (an env var only the operator can set).
type PreflightAccount struct {
	CredentialType string          `json:"credentialType"`
	Label          string          `json:"label"`
	AuthType       string          `json:"authType,omitempty"`
	EnvKeys        []string        `json:"envKeys"`
	Nodes          []PreflightNode `json:"nodes"`
}

// RunPreflight is the answer to "can this workflow run as configured?".
type RunPreflight struct {
	Ready    bool               `json:"ready"`
	Accounts []PreflightAccount `json:"accounts"`
}

// credentialCheckEnabled reports whether launches are gated on the preflight.
func credentialCheckEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUN_CREDENTIAL_CHECK"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// nodeCredentialNeed describes what one node type needs, independent of how a
// particular graph configures it.
type nodeCredentialNeed struct {
	credType string   // credential-type id; "" = env only
	label    string   // account display name
	authType string   // pieces auth type, "" for catalog/env
	envKeys  []string // env vars that satisfy it (catalog: per key; pieces: one JSON blob)
	keys     []string // catalog keys every one of which must resolve
	pieces   bool
}

// credentialNeedFor returns what node type typ needs, or ok=false when it
// needs nothing.
func credentialNeedFor(typ string) (nodeCredentialNeed, bool) {
	if typ == "" {
		return nodeCredentialNeed{}, false // prompt-compiled agent.llm
	}
	node, known := LoadCatalog().ByID[typ]
	if !known || len(node.Credentials) == 0 {
		return nodeCredentialNeed{}, false
	}
	types, _ := credentialTypes()
	if strings.HasPrefix(typ, "pieces.") {
		slug := pieceSlugOf(typ)
		if slug == "" || node.AuthOptional {
			return nodeCredentialNeed{}, false
		}
		credType := "pieces." + slug
		need := nodeCredentialNeed{
			credType: credType, authType: node.AuthType, pieces: true,
			label:   orStr(types[credType].Label, pieceDisplayName(slug)),
			envKeys: []string{node.Credentials[0].Key},
		}
		return need, true
	}
	need := nodeCredentialNeed{}
	for _, c := range node.Credentials {
		if c.Key != "" {
			need.keys = append(need.keys, c.Key)
		}
	}
	if len(need.keys) == 0 {
		return nodeCredentialNeed{}, false
	}
	need.envKeys = need.keys
	if node.Cluster == "builtin" {
		// Built-ins run code handlers that read env only; there is no
		// credential type to select (build-web.mjs skips builtin.json).
		need.label = node.Label
		return need, true
	}
	need.credType = strings.SplitN(typ, ".", 2)[0]
	label := types[need.credType].Label
	if label == "" {
		label, _, _ = strings.Cut(node.Label, ":")
	}
	need.label = strings.TrimSpace(label)
	return need, true
}

// checkNodeCredentials is the shared preflight over a graph.
func checkNodeCredentials(repo platform.CredentialRepo, workspace domain.WorkspaceId, nodes []domain.GraphNode) RunPreflight {
	types, haveFields := credentialTypes()
	byAccount := map[string]*PreflightAccount{}
	var order []string

	for _, n := range nodes {
		need, ok := credentialNeedFor(n.Type)
		if !ok {
			continue
		}
		credID := ""
		if v, isStr := n.Config[credentialIDKey].(string); isStr {
			credID = strings.TrimSpace(v)
		}
		if need.credType == "" {
			credID = "" // env-only built-in: a stray id is ignored at runtime too
		}

		var data map[string]any
		problem := ""
		if credID != "" {
			row, found := repo.GetByID(domain.CredentialId(credID))
			if !found || row.WorkspaceID != workspace {
				problem = ProblemCredentialDeleted
			} else {
				data = row.Data
			}
		}

		var missing []string
		if problem == "" {
			switch {
			case need.pieces && credID != "":
				if haveFields {
					for _, f := range types[need.credType].Fields {
						if f.Required && !hasValue(data[f.Key]) {
							missing = append(missing, f.Key)
						}
					}
				}
				if len(missing) > 0 {
					problem = ProblemIncomplete
				}
			case need.pieces:
				if os.Getenv(need.envKeys[0]) == "" {
					problem = ProblemNotConnected
				}
			default:
				for _, k := range need.keys {
					if !hasValue(data[k]) && os.Getenv(k) == "" {
						missing = append(missing, k)
					}
				}
				if len(missing) > 0 {
					problem = ProblemNotConnected
					if credID != "" {
						problem = ProblemIncomplete
					}
				}
			}
		}
		if problem == "" {
			continue
		}

		accountKey := need.credType
		if accountKey == "" {
			accountKey = "env:" + strings.Join(need.envKeys, ",")
		}
		acct, seen := byAccount[accountKey]
		if !seen {
			acct = &PreflightAccount{
				CredentialType: need.credType, Label: need.label,
				AuthType: need.authType, EnvKeys: append([]string(nil), need.envKeys...),
			}
			byAccount[accountKey] = acct
			order = append(order, accountKey)
		}
		for _, k := range need.envKeys {
			if !containsStr(acct.EnvKeys, k) {
				acct.EnvKeys = append(acct.EnvKeys, k)
			}
		}
		name := n.Name
		if name == "" {
			name = n.Key
		}
		acct.Nodes = append(acct.Nodes, PreflightNode{
			Key: n.Key, Name: name, Type: n.Type, Problem: problem,
			MissingKeys: missing, CredentialID: credID,
		})
	}

	out := RunPreflight{Ready: len(order) == 0, Accounts: []PreflightAccount{}}
	sort.SliceStable(order, func(i, j int) bool {
		return strings.ToLower(byAccount[order[i]].Label) < strings.ToLower(byAccount[order[j]].Label)
	})
	for _, k := range order {
		out.Accounts = append(out.Accounts, *byAccount[k])
	}
	return out
}

// preflightError turns a failed preflight into the launch error: one detail
// line per node, and a message naming the accounts to connect.
func preflightError(pf RunPreflight) *domain.DomainError {
	labels := make([]string, 0, len(pf.Accounts))
	var details []domain.ErrorDetail
	for _, a := range pf.Accounts {
		labels = append(labels, a.Label)
		for _, n := range a.Nodes {
			details = append(details, domain.ErrorDetail{Path: n.Key, Message: problemText(a, n)})
		}
	}
	return &domain.DomainError{
		Code:    domain.CodeCredentialsRequired,
		Message: "Connect " + strings.Join(labels, ", ") + " before running this workflow",
		Details: details,
	}
}

// problemText is the human sentence for one node's problem.
func problemText(a PreflightAccount, n PreflightNode) string {
	switch n.Problem {
	case ProblemCredentialDeleted:
		return n.Name + ": the selected " + a.Label + " credential was deleted"
	case ProblemIncomplete:
		return n.Name + ": the selected " + a.Label + " credential is missing " + strings.Join(n.MissingKeys, ", ")
	}
	if a.CredentialType == "" {
		return n.Name + ": set " + strings.Join(a.EnvKeys, ", ") + " on the server"
	}
	return n.Name + ": no " + a.Label + " account connected"
}

// Preflight checks the workflow's current graph without launching anything.
func (s *RunService) Preflight(slug string) (RunPreflight, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return RunPreflight{}, domain.NotFound("workflow")
	}
	return checkNodeCredentials(s.deps.Repos.Credentials, wf.WorkspaceID, currentNodes(s.deps.Repos, wf)), nil
}

// ApplyCredential selects credential id on every node of the workflow that
// needs its type and cannot run as configured, saving the result as a new
// graph version. Nodes that already resolve keep their own selection.
func (s *WorkflowService) ApplyCredential(slug string, id domain.CredentialId) (RunPreflight, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return RunPreflight{}, domain.NotFound("workflow")
	}
	cred, found := s.deps.Repos.Credentials.GetByID(id)
	if !found || cred.WorkspaceID != wf.WorkspaceID {
		return RunPreflight{}, domain.NotFound("Credential")
	}
	nodes := currentNodes(s.deps.Repos, wf)
	before := checkNodeCredentials(s.deps.Repos.Credentials, wf.WorkspaceID, nodes)
	broken := map[string]bool{}
	for _, a := range before.Accounts {
		if a.CredentialType != cred.Type {
			continue
		}
		for _, n := range a.Nodes {
			broken[n.Key] = true
		}
	}
	if len(broken) == 0 {
		return before, nil
	}
	updated := make([]domain.GraphNode, len(nodes))
	for i, n := range nodes {
		if broken[n.Key] {
			cfg := make(map[string]any, len(n.Config)+1)
			for k, v := range n.Config {
				cfg[k] = v
			}
			cfg[credentialIDKey] = string(id)
			n.Config = cfg
		}
		updated[i] = n
	}
	if _, err := s.SaveGraph(slug, updated); err != nil {
		return RunPreflight{}, err
	}
	return checkNodeCredentials(s.deps.Repos.Credentials, wf.WorkspaceID, updated), nil
}

// currentNodes returns the graph of the workflow's current version (empty
// when it has none).
func currentNodes(repos *platform.Repositories, wf domain.Workflow) []domain.GraphNode {
	if wf.CurrentVersionID == nil {
		return nil
	}
	version, ok := repos.Versions.GetByID(*wf.CurrentVersionID)
	if !ok {
		return nil
	}
	return version.Nodes
}

func hasValue(v any) bool {
	if v == nil {
		return false
	}
	if sv, isStr := v.(string); isStr {
		return strings.TrimSpace(sv) != ""
	}
	return true
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
