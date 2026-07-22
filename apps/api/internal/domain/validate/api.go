package validate

import (
	"net/url"
	"strings"

	"github.com/agently/api/internal/domain"
)

// API input parsers. These mirror the zod schemas in
// packages/contracts/src/api.ts and primitives.ts, including defaults and bounds.

const (
	defaultLimit = 25
	minLimit     = 1
	maxLimit     = 100
)

// parseLimit reads + bounds the `limit` query param, defaulting to 25.
func (v *ValidationError) parseLimit(q url.Values) int {
	n, present := v.coerceInt("limit", q.Get("limit"))
	if !present {
		return defaultLimit
	}
	if n < minLimit {
		v.add("limit", "must be >= 1")
		return defaultLimit
	}
	if n > maxLimit {
		v.add("limit", "must be <= 100")
		return maxLimit
	}
	return n
}

// ParsePageQuery validates the shared cursor pagination query.
func ParsePageQuery(q url.Values) (domain.PageQuery, error) {
	v := &ValidationError{}
	pq := domain.PageQuery{Cursor: q.Get("cursor"), Limit: v.parseLimit(q)}
	if v.HasErrors() {
		return domain.PageQuery{}, v
	}
	return pq, nil
}

// ListRunsQuery extends PageQuery with status + workflowId filters.
type ListRunsQuery struct {
	domain.PageQuery
	Status     string
	WorkflowID string
}

func ParseListRunsQuery(q url.Values) (ListRunsQuery, error) {
	v := &ValidationError{}
	out := ListRunsQuery{
		PageQuery:  domain.PageQuery{Cursor: q.Get("cursor"), Limit: v.parseLimit(q)},
		Status:     q.Get("status"),
		WorkflowID: q.Get("workflowId"),
	}
	if v.HasErrors() {
		return ListRunsQuery{}, v
	}
	return out, nil
}

// ListLogsQuery extends PageQuery with log filters.
type ListLogsQuery struct {
	domain.PageQuery
	Severity string // all | warn | error (default all)
	Channel  string
	Source   string
	Q        string
	AfterSeq *int
}

var validLogSeverity = map[string]bool{"all": true, "warn": true, "error": true}

func ParseListLogsQuery(q url.Values) (ListLogsQuery, error) {
	v := &ValidationError{}
	severity := q.Get("severity")
	if severity == "" {
		severity = "all"
	}
	v.enumValue("severity", severity, validLogSeverity)

	out := ListLogsQuery{
		PageQuery: domain.PageQuery{Cursor: q.Get("cursor"), Limit: v.parseLimit(q)},
		Severity:  severity,
		Channel:   q.Get("channel"),
		Source:    q.Get("source"),
		Q:         q.Get("q"),
	}
	if n, present := v.coerceInt("afterSeq", q.Get("afterSeq")); present {
		if n < 0 {
			v.add("afterSeq", "must be >= 0")
		} else {
			out.AfterSeq = &n
		}
	}
	if v.HasErrors() {
		return ListLogsQuery{}, v
	}
	return out, nil
}

// ListNotificationsQuery extends PageQuery with unread + type filters.
type ListNotificationsQuery struct {
	domain.PageQuery
	Unread    *bool
	UnreadSet bool
	Type      string
}

func ParseListNotificationsQuery(q url.Values) (ListNotificationsQuery, error) {
	v := &ValidationError{}
	out := ListNotificationsQuery{
		PageQuery: domain.PageQuery{Cursor: q.Get("cursor"), Limit: v.parseLimit(q)},
		Type:      q.Get("type"),
	}
	v.enumValue("type", out.Type, domain.ValidNotificationTypes)
	if b, present := v.coerceBool(q.Get("unread")); present {
		out.Unread = &b
		out.UnreadSet = true
	}
	if v.HasErrors() {
		return ListNotificationsQuery{}, v
	}
	return out, nil
}

/* ----------------------------- bodies ---------------------------- */

// CreateWorkflowInput mirrors the zod CreateWorkflowInput.
type CreateWorkflowInput struct {
	Name        string
	Description string
	Trigger     domain.TriggerType
	Schedule    *string
	Tags        []string
	// Prompt is the natural-language description the planner compiles into an agent
	// graph. When present, Name/Description/graph are derived from it.
	Prompt string
	// Email is an optional recipient woven into the workflow's default run input.
	Email string
}

func ParseCreateWorkflowInput(body map[string]any) (CreateWorkflowInput, error) {
	v := &ValidationError{}
	name := getString(body, "name")
	prompt := getString(body, "prompt")
	// Either a name or a prompt must identify the workflow.
	if strings.TrimSpace(name) == "" && strings.TrimSpace(prompt) == "" {
		v.add("name", "name or prompt is required")
	}
	v.stringMaxLen("name", name, 120)
	v.stringMaxLen("prompt", prompt, 4000)

	description := getString(body, "description")
	v.stringMaxLen("description", description, 2000)

	trigger := getString(body, "trigger")
	if trigger == "" {
		trigger = "manual"
	}
	v.enumValue("trigger", trigger, domain.ValidTriggerTypes)

	out := CreateWorkflowInput{
		Name:        name,
		Description: description,
		Trigger:     domain.TriggerType(trigger),
		Tags:        getStringSlice(body, "tags"),
		Prompt:      prompt,
		Email:       getString(body, "email"),
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if hasKey(body, "schedule") {
		s := getString(body, "schedule")
		out.Schedule = &s
	}
	if v.HasErrors() {
		return CreateWorkflowInput{}, v
	}
	return out, nil
}

// ParseSaveGraphInput parses the visual builder's Save payload: {nodes: [...]}.
// Each node carries a builder type + per-type config alongside the existing graph
// fields. Validation is intentionally lenient (the builder is the source of truth
// for shape) — we guarantee a stable key, a valid role, and non-nil collections so
// the persisted version is always well-formed and runnable.
func ParseSaveGraphInput(body map[string]any) ([]domain.GraphNode, error) {
	v := &ValidationError{}
	raw, _ := body["nodes"].([]any)
	if len(raw) == 0 {
		v.add("nodes", "at least one node is required")
		return nil, v
	}
	if len(raw) > 200 {
		v.add("nodes", "too many nodes (max 200)")
		return nil, v
	}

	nodes := make([]domain.GraphNode, 0, len(raw))
	seen := map[string]bool{}
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			v.add("nodes", "node "+itoa(i)+" is not an object")
			continue
		}
		key := getString(obj, "key")
		if strings.TrimSpace(key) == "" {
			v.add("nodes."+itoa(i)+".key", "key is required")
			continue
		}
		if seen[key] {
			v.add("nodes."+itoa(i)+".key", "duplicate key "+key)
			continue
		}
		seen[key] = true

		role := getString(obj, "role")
		if !domain.ValidAgentRoles[role] {
			role = "orchestrator" // typed builder nodes don't all map to an agent role
		}
		nodeType := getString(obj, "type")
		if strings.TrimSpace(nodeType) == "" {
			nodeType = "agent.llm"
		}
		config := getMap(obj, "config")
		if config == nil {
			config = map[string]any{}
		}

		nodes = append(nodes, domain.GraphNode{
			Key:       key,
			Name:      getString(obj, "name"),
			Role:      domain.AgentRole(role),
			Model:     getString(obj, "model"),
			Col:       getInt(obj, "col"),
			Row:       getInt(obj, "row"),
			DependsOn: getStringSlice(obj, "dependsOn"),
			Type:      nodeType,
			Config:    config,
		})
	}
	if v.HasErrors() {
		return nil, v
	}
	// Reject edges to non-existent nodes — keeps the persisted DAG self-consistent.
	for i := range nodes {
		clean := nodes[i].DependsOn[:0]
		for _, dep := range nodes[i].DependsOn {
			if seen[dep] {
				clean = append(clean, dep)
			}
		}
		nodes[i].DependsOn = clean
		if nodes[i].DependsOn == nil {
			nodes[i].DependsOn = []string{}
		}
	}
	return nodes, nil
}

// CreateAgentInput mirrors the zod CreateAgentInput.
type CreateAgentInput struct {
	Name        string
	Role        domain.AgentRole
	Model       string
	Description string
	Tools       []string
	Config      map[string]any
}

func ParseCreateAgentInput(body map[string]any) (CreateAgentInput, error) {
	v := &ValidationError{}
	name := getString(body, "name")
	v.stringRequired("name", name)
	v.stringMaxLen("name", name, 120)

	role := getString(body, "role")
	v.stringRequired("role", role)
	v.enumValue("role", role, domain.ValidAgentRoles)

	model := getString(body, "model")
	v.stringRequired("model", model)

	description := getString(body, "description")
	v.stringMaxLen("description", description, 2000)

	out := CreateAgentInput{
		Name:        name,
		Role:        domain.AgentRole(role),
		Model:       model,
		Description: description,
		Tools:       getStringSlice(body, "tools"),
		Config:      getMap(body, "config"),
	}
	if out.Tools == nil {
		out.Tools = []string{}
	}
	if out.Config == nil {
		out.Config = map[string]any{}
	}
	if v.HasErrors() {
		return CreateAgentInput{}, v
	}
	return out, nil
}

// LaunchRunInput mirrors the zod LaunchRunInput.
type LaunchRunInput struct {
	Input   map[string]any
	Trigger domain.TriggerType
	Region  *string
	// Engine is accepted for backward compatibility but every run now executes on
	// the Temporal + LangGraph reasoner — the native Go worker was retired
	// (archived under archive/worker). Only "temporal" (or empty) is valid.
	Engine string
}

var validEngines = map[string]bool{"temporal": true}

func ParseLaunchRunInput(body map[string]any) (LaunchRunInput, error) {
	v := &ValidationError{}
	trigger := getString(body, "trigger")
	if trigger == "" {
		trigger = "manual"
	}
	v.enumValue("trigger", trigger, domain.ValidTriggerTypes)

	engine := getString(body, "engine")
	if engine != "" {
		v.enumValue("engine", engine, validEngines)
	}

	out := LaunchRunInput{
		Input:   getMap(body, "input"),
		Trigger: domain.TriggerType(trigger),
		Engine:  engine,
	}
	if hasKey(body, "region") {
		r := getString(body, "region")
		out.Region = &r
	}
	if v.HasErrors() {
		return LaunchRunInput{}, v
	}
	return out, nil
}
