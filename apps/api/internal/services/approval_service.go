package services

// approval_service.go backs the Inbox. The reasoner's human.approval node
// opens approvals and waits; a person decides here; the reasoner's relay then
// wakes the waiting run (it reads signaled_at IS NULL rows).
//
//	GET  /api/approvals[?status&runId&limit]   newest first, with the causing signal
//	GET  /api/approvals/{id}                   + signals, run (cost, trace), outcomes
//	POST /api/approvals/{id}/decision          {decision, content?, decidedBy, note?}

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

type ApprovalService struct {
	deps Deps
	now  func() domain.Timestamp
}

func NewApprovalService(deps Deps, intel *IntelService) *ApprovalService {
	return &ApprovalService{deps: deps, now: intel.now}
}

// SignalRef is the short form of a signal shown next to a draft.
type SignalRef struct {
	ID     domain.SignalId `json:"id"`
	Source string          `json:"source"`
	Title  string          `json:"title"`
	URL    string          `json:"url"`
}

// ApprovalListItem is an approval plus what the Inbox list shows beside it.
type ApprovalListItem struct {
	domain.Approval
	WorkflowName string     `json:"workflowName"`
	WorkflowSlug string     `json:"workflowSlug"`
	Signal       *SignalRef `json:"signal"` // the first causing signal, if any
	SignalCount  int        `json:"signalCount"`
}

// ApprovalRun is the slice of the run that drafted it that traceability needs.
type ApprovalRun struct {
	ID              domain.RunId      `json:"id"`
	Number          int               `json:"number"`
	Status          domain.RunStatus  `json:"status"`
	WorkflowName    string            `json:"workflowName"`
	WorkflowSlug    string            `json:"workflowSlug"`
	CostUsd         float64           `json:"costUsd"`
	LangfuseTraceID *string           `json:"langfuseTraceId"`
	StartedAt       *domain.Timestamp `json:"startedAt"`
	FinishedAt      *domain.Timestamp `json:"finishedAt"`
}

type ApprovalDetail struct {
	domain.Approval
	Signals  []domain.Signal          `json:"signals"`
	Run      *ApprovalRun             `json:"run"`
	Outcomes []domain.ApprovalOutcome `json:"outcomes"`
}

func (s *ApprovalService) ws() domain.WorkspaceId { return s.deps.Repos.Workspace.Get().ID }

func (s *ApprovalService) List(q validate.ListApprovalsQuery) []ApprovalListItem {
	query := platform.ApprovalQuery{Limit: q.Limit}
	if q.Status != "" {
		st := domain.ApprovalStatus(q.Status)
		query.Status = &st
	}
	if q.RunID != "" {
		id := domain.RunId(q.RunID)
		query.RunID = &id
	}
	rows := s.deps.Repos.Approvals.List(s.ws(), query)
	out := make([]ApprovalListItem, 0, len(rows))
	runs := map[domain.RunId]domain.Run{}
	for _, a := range rows {
		item := ApprovalListItem{Approval: a, SignalCount: len(a.SignalIDs)}
		run, seen := runs[a.RunID]
		if !seen {
			run, _ = s.deps.Repos.Runs.GetByID(a.RunID)
			runs[a.RunID] = run
		}
		item.WorkflowName, item.WorkflowSlug = run.WorkflowName, run.WorkflowSlug
		if len(a.SignalIDs) > 0 {
			if sg, ok := s.deps.Repos.Signals.GetByID(a.SignalIDs[0]); ok {
				item.Signal = &SignalRef{ID: sg.ID, Source: sg.Source, Title: sg.Title, URL: sg.URL}
			}
		}
		out = append(out, item)
	}
	return out
}

func (s *ApprovalService) get(id domain.ApprovalId) (domain.Approval, error) {
	a, ok := s.deps.Repos.Approvals.GetByID(id)
	if !ok || a.WorkspaceID != s.ws() {
		return domain.Approval{}, domain.NotFound("Approval")
	}
	return a, nil
}

func (s *ApprovalService) Get(id domain.ApprovalId) (ApprovalDetail, error) {
	a, err := s.get(id)
	if err != nil {
		return ApprovalDetail{}, err
	}
	d := ApprovalDetail{Approval: a, Signals: []domain.Signal{}, Outcomes: s.deps.Repos.Approvals.ListOutcomes(id)}
	for _, sid := range a.SignalIDs {
		if sg, ok := s.deps.Repos.Signals.GetByID(sid); ok {
			d.Signals = append(d.Signals, sg)
		}
	}
	if run, ok := s.deps.Repos.Runs.GetByID(a.RunID); ok {
		d.Run = &ApprovalRun{
			ID: run.ID, Number: run.Number, Status: run.Status, WorkflowName: run.WorkflowName,
			WorkflowSlug: run.WorkflowSlug, CostUsd: run.CostUsd, LangfuseTraceID: run.LangfuseTraceID,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		}
	}
	return d, nil
}

// Decide records a person's decision. The waiting run is woken by the
// reasoner's relay within a couple of seconds; nothing is published here.
func (s *ApprovalService) Decide(id domain.ApprovalId, in validate.DecisionInput) (ApprovalDetail, error) {
	if _, err := s.get(id); err != nil {
		return ApprovalDetail{}, err
	}
	if _, err := s.deps.Repos.Approvals.Decide(id, domain.ApprovalDecision{
		Status: in.Status, FinalContent: in.Content, DecidedBy: in.DecidedBy, Note: in.Note, DecidedAt: s.now(),
	}); err != nil {
		return ApprovalDetail{}, err
	}
	return s.Get(id)
}
