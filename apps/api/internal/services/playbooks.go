package services

// playbooks.go serves the ready-made playbooks (packages/nodes/playbooks/*.json):
// marketing workflows people start from instead of a blank prompt.
//
//	GET  /api/playbooks                  the templates, with what each needs
//	GET  /api/playbooks/{id}             one template, with its graph
//	POST /api/playbooks/{id}/install     {name?, inputs} → a new playbook (workflow)
//
// Installing compiles nothing: the template's graph is used as written, with
// the person's inputs as its default run input. It is validated exactly like a
// planned graph (validateGraph — including "nothing publishes without
// approval" — and validateRefs), so a template that rots fails loudly.

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

type PlaybookInput struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Type     string `json:"type,omitempty"` // "list" accepts a list or comma-separated text
	Help     string `json:"help,omitempty"`
	Example  string `json:"example,omitempty"`
}

type Playbook struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Summary      string             `json:"summary"`
	Trigger      domain.TriggerType `json:"trigger"`
	Schedule     string             `json:"schedule"`
	PublishesTo  []string           `json:"publishesTo"`
	Inputs       []PlaybookInput    `json:"inputs"`
	DefaultInput map[string]any     `json:"defaultInput"`
	Nodes        []domain.GraphNode `json:"nodes"`
}

// PlaybookLogin is a sign-in the playbook's steps need, and whether the
// workspace has one stored.
type PlaybookLogin struct {
	Type      string `json:"type"`
	Label     string `json:"label"`
	Connected bool   `json:"connected"`
}

type PlaybookSummary struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Summary     string             `json:"summary"`
	Trigger     domain.TriggerType `json:"trigger"`
	Schedule    string             `json:"schedule"`
	PublishesTo []string           `json:"publishesTo"`
	Inputs      []PlaybookInput    `json:"inputs"`
	Steps       int                `json:"steps"`
	Approvals   int                `json:"approvals"`
	Logins      []PlaybookLogin    `json:"logins"`
}

var (
	playbooksOnce sync.Once
	playbooksVal  []Playbook
	playbooksErr  error
)

func playbooksDir() string {
	if dir := catalogDir(); dir != "" {
		return filepath.Join(filepath.Dir(dir), "playbooks")
	}
	return ""
}

func loadPlaybooks() ([]Playbook, error) {
	playbooksOnce.Do(func() {
		dir := playbooksDir()
		if dir == "" {
			playbooksErr = fmt.Errorf("packages/nodes/playbooks not found")
			return
		}
		files, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil || len(files) == 0 {
			playbooksErr = fmt.Errorf("no playbooks in %s", dir)
			return
		}
		sort.Strings(files)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				playbooksErr = err
				return
			}
			var pb Playbook
			if err := json.Unmarshal(raw, &pb); err != nil {
				playbooksErr = fmt.Errorf("%s: %w", filepath.Base(f), err)
				return
			}
			for i := range pb.Nodes {
				pb.Nodes[i].Role = roleForType(pb.Nodes[i].Type)
				if pb.Nodes[i].Config == nil {
					pb.Nodes[i].Config = map[string]any{}
				}
			}
			playbooksVal = append(playbooksVal, pb)
		}
	})
	return playbooksVal, playbooksErr
}

type PlaybookService struct {
	deps      Deps
	workflows *WorkflowService
}

func NewPlaybookService(deps Deps, workflows *WorkflowService) *PlaybookService {
	return &PlaybookService{deps: deps, workflows: workflows}
}

func (s *PlaybookService) find(id string) (Playbook, error) {
	all, err := loadPlaybooks()
	if err != nil {
		return Playbook{}, domain.Upstream("playbooks unavailable: " + err.Error())
	}
	for _, pb := range all {
		if pb.ID == id {
			return pb, nil
		}
	}
	return Playbook{}, domain.NotFound("Playbook")
}

// loginTypes: the credential types a graph's integration steps sign in with.
func loginTypes(nodes []domain.GraphNode) []string {
	cat := LoadCatalog()
	seen := map[string]bool{}
	var out []string
	for _, n := range nodes {
		if _, ok := cat.ByID[n.Type]; !ok || !strings.HasPrefix(n.Type, "pieces.") {
			continue
		}
		typ := "pieces." + pieceSlugOf(n.Type)
		if !seen[typ] {
			seen[typ] = true
			out = append(out, typ)
		}
	}
	sort.Strings(out)
	return out
}

func (s *PlaybookService) storedByType() map[string][]domain.Credential {
	by := map[string][]domain.Credential{}
	for _, c := range s.deps.Repos.Credentials.ListByWorkspace(s.deps.Repos.Workspace.Get().ID) {
		by[c.Type] = append(by[c.Type], c)
	}
	return by
}

func (s *PlaybookService) summarize(pb Playbook) PlaybookSummary {
	sum := PlaybookSummary{ID: pb.ID, Name: pb.Name, Summary: pb.Summary, Trigger: pb.Trigger, Schedule: pb.Schedule,
		PublishesTo: pb.PublishesTo, Inputs: pb.Inputs, Steps: len(pb.Nodes), Logins: []PlaybookLogin{}}
	for _, n := range pb.Nodes {
		if n.Type == approvalType {
			sum.Approvals++
		}
	}
	stored := s.storedByType()
	types, _ := credentialTypes()
	for _, t := range loginTypes(pb.Nodes) {
		label := t
		if def, ok := types[t]; ok && def.Label != "" {
			label = def.Label
		}
		sum.Logins = append(sum.Logins, PlaybookLogin{Type: t, Label: label, Connected: len(stored[t]) > 0})
	}
	return sum
}

func (s *PlaybookService) List() ([]PlaybookSummary, error) {
	all, err := loadPlaybooks()
	if err != nil {
		return nil, domain.Upstream("playbooks unavailable: " + err.Error())
	}
	out := make([]PlaybookSummary, 0, len(all))
	for _, pb := range all {
		out = append(out, s.summarize(pb))
	}
	return out, nil
}

type PlaybookDetail struct {
	PlaybookSummary
	Nodes []domain.GraphNode `json:"nodes"`
}

func (s *PlaybookService) Get(id string) (PlaybookDetail, error) {
	pb, err := s.find(id)
	if err != nil {
		return PlaybookDetail{}, err
	}
	return PlaybookDetail{PlaybookSummary: s.summarize(pb), Nodes: pb.Nodes}, nil
}

// Install turns a template into a playbook the person owns.
func (s *PlaybookService) Install(id, name string, inputs map[string]any) (domain.WorkflowSummary, error) {
	pb, err := s.find(id)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}
	defaultInput := map[string]any{}
	for k, v := range pb.DefaultInput {
		defaultInput[k] = v
	}
	var missing []string
	for _, in := range pb.Inputs {
		v, given := inputs[in.Key]
		if given {
			if in.Type == "list" {
				v = asStringList(v)
			} else if str, ok := v.(string); ok {
				v = strings.TrimSpace(str)
			}
			defaultInput[in.Key] = v
		}
		if in.Required && isBlank(defaultInput[in.Key]) {
			missing = append(missing, in.Label)
		}
	}
	if len(missing) > 0 {
		return domain.WorkflowSummary{}, domain.BadRequest("missing: " + strings.Join(missing, ", "))
	}

	// A fresh copy of the graph, with the one stored login of each needed
	// type attached (none or several: the builder asks the person to pick).
	stored := s.storedByType()
	nodes := make([]domain.GraphNode, len(pb.Nodes))
	for i, n := range pb.Nodes {
		cfg := make(map[string]any, len(n.Config)+1)
		for k, v := range n.Config {
			cfg[k] = v
		}
		if strings.HasPrefix(n.Type, "pieces.") {
			if creds := stored["pieces."+pieceSlugOf(n.Type)]; len(creds) == 1 {
				cfg["__credentialId"] = string(creds[0].ID)
			}
		}
		n.Config = cfg
		n.DependsOn = append([]string(nil), n.DependsOn...)
		nodes[i] = n
	}
	inputKeys := map[string]bool{}
	for k := range defaultInput {
		inputKeys[k] = true
	}
	cat := LoadCatalog()
	if errs := append(validateGraph(nodes, cat), validateRefs(nodes, cat, inputKeys)...); len(errs) > 0 {
		return domain.WorkflowSummary{}, domain.Upstream("playbook " + id + " is invalid: " + joinErrors(errs))
	}
	if strings.TrimSpace(name) == "" {
		name = pb.Name
	}
	var schedule *string
	if pb.Schedule != "" {
		sc := pb.Schedule
		schedule = &sc
	}
	return s.workflows.CreateFromGraph(name, pb.Summary, pb.Trigger, schedule, nodes, defaultInput,
		[]string{"playbook", pb.ID})
}

func asStringList(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, x := range t {
			if s := strings.TrimSpace(x); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, x := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
			if s := strings.TrimSpace(x); s != "" {
				out = append(out, s)
			}
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func isBlank(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []string:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}
