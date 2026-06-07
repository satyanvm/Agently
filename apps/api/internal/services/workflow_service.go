package services

import (
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

// Create inserts a new workflow.
func (s *WorkflowService) Create(input validate.CreateWorkflowInput) (domain.WorkflowSummary, error) {
	now := domain.Timestamp(s.deps.Clock.ISO())
	slug := slugify(input.Name)
	if _, exists := s.deps.Repos.Workflows.GetBySlug(slug); exists {
		return domain.WorkflowSummary{}, domain.Conflict(`a workflow with slug "` + slug + `" already exists`)
	}
	var ownerID *domain.MemberId
	if members := s.deps.Repos.Members.All(); len(members) > 0 {
		ownerID = &members[0].ID
	}
	wf := domain.Workflow{
		ID: domain.NewWorkflowId(), WorkspaceID: s.deps.Repos.Workspace.Get().ID, Slug: slug,
		Name: input.Name, Description: input.Description, Trigger: input.Trigger, Schedule: input.Schedule,
		Tags: input.Tags, OwnerID: ownerID, AgentCount: 0, CurrentVersionID: nil,
		CreatedAt: now, UpdatedAt: now, ArchivedAt: nil,
	}
	s.deps.Repos.Workflows.Insert(wf)
	return s.ToSummary(wf), nil
}
