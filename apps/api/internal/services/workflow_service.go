package services

import (
	"context"
	"strings"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

var fallbackStats = domain.WorkflowStats{
	SuccessRate: 1, AvgRuntimeMs: 0, AvgCostUsd: 0, TotalRuns: 0, Recent: []int{}, Trend: []float64{},
}

// WorkflowService lists/gets/creates workflows and derives their view fields.
type WorkflowService struct {
	deps Deps
}

func NewWorkflowService(deps Deps) *WorkflowService {
	return &WorkflowService{deps: deps}
}

func (s *WorkflowService) ownerOf(wf domain.Workflow) *domain.Principal {
	if wf.OwnerID == nil {
		return nil
	}
	if m, ok := s.deps.Repos.Members.GetByID(*wf.OwnerID); ok {
		return &domain.Principal{Name: m.Name, Initials: m.Initials}
	}
	return nil
}

func (s *WorkflowService) latestRun(workflowID domain.WorkflowId) (domain.Run, bool) {
	wfRuns := s.deps.Repos.Runs.ListByWorkflow(workflowID)
	if len(wfRuns) == 0 {
		return domain.Run{}, false
	}
	platform.SortByNewest(wfRuns, func(r domain.Run) string {
		if r.StartedAt != nil {
			return string(*r.StartedAt)
		}
		return string(r.QueuedAt)
	})
	return wfRuns[0], true
}

func (s *WorkflowService) deriveStatus(workflowID domain.WorkflowId) domain.RunStatus {
	for _, r := range s.deps.Repos.Runs.ListByWorkflow(workflowID) {
		if r.Status == domain.RunRunning {
			return domain.RunRunning
		}
	}
	if latest, ok := s.latestRun(workflowID); ok {
		return latest.Status
	}
	return domain.RunQueued
}

func (s *WorkflowService) lastRunAt(workflowID domain.WorkflowId) *domain.Timestamp {
	latest, ok := s.latestRun(workflowID)
	if !ok {
		return nil
	}
	if latest.StartedAt != nil {
		return latest.StartedAt
	}
	return &latest.QueuedAt
}

// ToSummary turns a workflow into the list/detail read model.
func (s *WorkflowService) ToSummary(wf domain.Workflow) domain.WorkflowSummary {
	stats, ok := s.deps.SeedStats.WorkflowStats[string(wf.ID)]
	if !ok {
		stats = fallbackStats
	}
	return domain.WorkflowSummary{
		Workflow:  wf,
		Status:    s.deriveStatus(wf.ID),
		Owner:     s.ownerOf(wf),
		LastRunAt: s.lastRunAt(wf.ID),
		Stats:     stats,
	}
}

// List returns non-archived workflows as summaries, paginated.
func (s *WorkflowService) List(query domain.PageQuery) domain.Page[domain.WorkflowSummary] {
	items := []domain.WorkflowSummary{}
	for _, wf := range s.deps.Repos.Workflows.All() {
		if wf.ArchivedAt != nil {
			continue
		}
		items = append(items, s.ToSummary(wf))
	}
	return platform.Paginate(items, query)
}

// GetBySlug returns one workflow summary.
func (s *WorkflowService) GetBySlug(slug string) (domain.WorkflowSummary, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return domain.WorkflowSummary{}, domain.NotFound("workflow")
	}
	return s.ToSummary(wf), nil
}

// Create inserts a new workflow. When the input carries a prompt, it compiles that
// prompt into an agent graph (fetchers → Editor), persists a workflow version, links
// it as the current version, and stores a default run input — so the workflow is
// immediately RUNNABLE. Without a prompt it falls back to a name-only graph derived
// from the name/description (still runnable).
func (s *WorkflowService) Create(input validate.CreateWorkflowInput) (domain.WorkflowSummary, error) {
	now := domain.Timestamp(s.deps.Clock.ISO())

	// Compile the prompt (or the name as a degenerate prompt) into a plan.
	promptText := input.Prompt
	if strings.TrimSpace(promptText) == "" {
		promptText = input.Name + ". " + input.Description
	}
	schedule := ""
	if input.Schedule != nil {
		schedule = *input.Schedule
	}
	plan := CompilePrompt(context.Background(), promptText, input.Name, input.Email, schedule)

	name := plan.Name
	if strings.TrimSpace(input.Name) != "" {
		name = input.Name
	}
	// Auto-disambiguate the slug: "ai-digest" → "ai-digest-2" → "ai-digest-3"… so two
	// workflows compiled from similar prompts (or matching the seeded AI Digest) never
	// block each other. Creating should always succeed, n8n-style.
	slug := s.uniqueSlug(slugify(name))
	var ownerID *domain.MemberId
	if members := s.deps.Repos.Members.All(); len(members) > 0 {
		ownerID = &members[0].ID
	}

	description := input.Description
	if strings.TrimSpace(description) == "" {
		description = plan.Description
	}
	tags := input.Tags
	if len(tags) == 0 {
		tags = plan.Tags
	}
	trigger := input.Trigger
	if plan.Schedule != nil && (trigger == "" || trigger == domain.TriggerManual) {
		trigger = domain.TriggerSchedule
	}

	wfID := domain.NewWorkflowId()
	// FK cycle (workflows.current_version_id ↔ workflow_versions.workflow_id): insert
	// the workflow with a nil version first, insert the version, then link it back.
	wf := domain.Workflow{
		ID: wfID, WorkspaceID: s.deps.Repos.Workspace.Get().ID, Slug: slug,
		Name: name, Description: description, Trigger: trigger, Schedule: plan.Schedule,
		Tags: tags, OwnerID: ownerID, AgentCount: len(plan.Nodes),
		CurrentVersionID: nil, DefaultInput: plan.DefaultInput,
		CreatedAt: now, UpdatedAt: now, ArchivedAt: nil,
	}
	if input.Schedule != nil {
		wf.Schedule = input.Schedule
	}
	s.deps.Repos.Workflows.Insert(wf)

	versionID := domain.NewWorkflowVersionId()
	s.deps.Repos.Versions.Insert(domain.WorkflowVersion{
		ID: versionID, WorkflowID: wfID, Version: 1, Note: "Compiled from prompt",
		CreatedBy: ownerID, CreatedAt: now, Nodes: plan.Nodes,
	})
	agentCount := len(plan.Nodes)
	if _, err := s.deps.Repos.Workflows.Update(wfID, platform.WorkflowPatch{
		CurrentVersionID: &versionID, AgentCount: &agentCount, UpdatedAt: &now,
	}); err != nil {
		return domain.WorkflowSummary{}, err
	}

	wf.CurrentVersionID = &versionID
	wf.AgentCount = agentCount
	return s.ToSummary(wf), nil
}

// Plan compiles a prompt into a plan WITHOUT persisting anything — the dry-run that
// powers the "preview the agents before you create" step in the UI.
func (s *WorkflowService) Plan(prompt, name, email, schedule string) Plan {
	return CompilePrompt(context.Background(), prompt, name, email, schedule)
}

// GraphNodes returns the workflow's current-version graph (the planned agents),
// independent of any run. Lets the UI show a workflow's agents the moment it's
// created, before it has ever run. Empty (not an error) if it has no version.
func (s *WorkflowService) GraphNodes(slug string) ([]domain.GraphNode, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return nil, domain.NotFound("workflow")
	}
	if wf.CurrentVersionID == nil {
		return []domain.GraphNode{}, nil
	}
	version, ok := s.deps.Repos.Versions.GetByID(*wf.CurrentVersionID)
	if !ok {
		return []domain.GraphNode{}, nil
	}
	return version.Nodes, nil
}

// SaveGraph persists a new version of a workflow's graph from the visual builder
// and links it as the current version, so the next run executes exactly what the
// user assembled. Versioning is append-only: every save mints version N+1 and never
// mutates an existing version (run history keeps pointing at the version it used).
func (s *WorkflowService) SaveGraph(slug string, nodes []domain.GraphNode) (domain.WorkflowSummary, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return domain.WorkflowSummary{}, domain.NotFound("workflow")
	}

	// Next version = max existing + 1 (versions are 1-based and contiguous-ish).
	next := 1
	for _, ver := range s.deps.Repos.Versions.ListByWorkflow(wf.ID) {
		if ver.Version >= next {
			next = ver.Version + 1
		}
	}

	now := domain.Timestamp(s.deps.Clock.ISO())
	var createdBy *domain.MemberId
	if members := s.deps.Repos.Members.All(); len(members) > 0 {
		createdBy = &members[0].ID
	}

	versionID := domain.NewWorkflowVersionId()
	s.deps.Repos.Versions.Insert(domain.WorkflowVersion{
		ID: versionID, WorkflowID: wf.ID, Version: next, Note: "Saved from builder",
		CreatedBy: createdBy, CreatedAt: now, Nodes: nodes,
	})

	agentCount := len(nodes)
	if _, err := s.deps.Repos.Workflows.Update(wf.ID, platform.WorkflowPatch{
		CurrentVersionID: &versionID, AgentCount: &agentCount, UpdatedAt: &now,
	}); err != nil {
		return domain.WorkflowSummary{}, err
	}

	wf.CurrentVersionID = &versionID
	wf.AgentCount = agentCount
	wf.UpdatedAt = now
	return s.ToSummary(wf), nil
}


// uniqueSlug returns base if free, else base-2, base-3, … so Create never fails on a
// slug collision. Bounded so a pathological case can't loop forever.
func (s *WorkflowService) uniqueSlug(base string) string {
	if base == "" {
		base = "workflow"
	}
	if _, exists := s.deps.Repos.Workflows.GetBySlug(base); !exists {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := base + "-" + itoa(i)
		if _, exists := s.deps.Repos.Workflows.GetBySlug(candidate); !exists {
			return candidate
		}
	}
	return base + "-" + domain.NewID(domain.KindWorkflow) // astronomically unlikely fallback
}
