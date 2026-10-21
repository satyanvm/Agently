package services

// approval_service.go backs the Inbox. The reasoner's human.approval node
// opens approvals and waits; a person decides here; the reasoner's relay then
// wakes the waiting run (it reads signaled_at IS NULL rows).
//
//	GET  /api/approvals[?status&runId&limit]   newest first, with the causing signal
//	GET  /api/approvals/{id}                   + signals, run (cost, trace), outcomes
//	GET  /api/approvals/{id}/history           signal → run → draft → decision → published, oldest first
//	POST /api/approvals/{id}/decision          {decision, content?, decidedBy, note?}

import (
	"sort"
	"strconv"
	"strings"

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
	a, err := s.get(id)
	if err != nil {
		return ApprovalDetail{}, err
	}
	// A run that has ended can't be woken, so an approval here would publish
	// nothing while the Inbox said "Approved". Refuse it with the reason.
	if run, ok := s.deps.Repos.Runs.GetByID(a.RunID); ok && a.Status == domain.ApprovalPending &&
		domain.IsTerminalRunStatus(run.Status) {
		return ApprovalDetail{}, domain.Conflict("the run that drafted this has already " + endedAs(run.Status) +
			", so deciding now would publish nothing; run the playbook again for a new draft")
	}
	if _, err := s.deps.Repos.Approvals.Decide(id, domain.ApprovalDecision{
		Status: in.Status, FinalContent: in.Content, DecidedBy: in.DecidedBy, Note: in.Note, DecidedAt: s.now(),
	}); err != nil {
		return ApprovalDetail{}, err
	}
	return s.Get(id)
}

// HistoryEvent is one step of an approval's story, oldest first.
type HistoryEvent struct {
	At     domain.Timestamp `json:"at"`
	Kind   string           `json:"kind"` // signal | run_started | draft | decision | outcome | run_finished
	Title  string           `json:"title"`
	Detail string           `json:"detail,omitempty"`
	URL    string           `json:"url,omitempty"`
	Status string           `json:"status,omitempty"`
	Ref    string           `json:"ref,omitempty"` // id of the signal / run / approval / outcome
}

// ApprovalHistory answers, for one draft: which signal caused it, which run
// drafted it and what that run cost, who decided and when, and what was
// published as a result.
type ApprovalHistory struct {
	ApprovalDetail
	Edited bool `json:"edited"` // approved text differs from the draft
	// Outcomes that succeeded AND acted outside Agently (an integration, an
	// output.* or an HTTP call) — what actually went out. Every outcome,
	// internal steps included, is still in Outcomes and the timeline.
	Published []domain.ApprovalOutcome `json:"published"`
	Timeline  []HistoryEvent           `json:"timeline"`
}

func (s *ApprovalService) History(id domain.ApprovalId) (ApprovalHistory, error) {
	d, err := s.Get(id)
	if err != nil {
		return ApprovalHistory{}, err
	}
	h := ApprovalHistory{ApprovalDetail: d, Published: []domain.ApprovalOutcome{}, Timeline: []HistoryEvent{}}
	h.Edited = d.FinalContent != nil && *d.FinalContent != d.Draft
	add := func(e HistoryEvent) {
		if e.At != "" {
			h.Timeline = append(h.Timeline, e)
		}
	}
	for _, sg := range d.Signals {
		add(HistoryEvent{At: sg.DetectedAt, Kind: "signal", Title: sg.Title, Detail: sg.Summary, URL: sg.URL,
			Status: sg.Source, Ref: string(sg.ID)})
	}
	if d.Run != nil && d.Run.StartedAt != nil {
		add(HistoryEvent{At: *d.Run.StartedAt, Kind: "run_started",
			Title: "Run #" + itoa(d.Run.Number) + " of " + d.Run.WorkflowName, Ref: string(d.Run.ID)})
	}
	draft := HistoryEvent{At: d.CreatedAt, Kind: "draft", Title: "Draft sent for approval: " + d.Title, Ref: string(d.ID)}
	if d.Assignee != "" {
		draft.Detail = "Assigned to " + d.Assignee
	}
	add(draft)
	if d.DecidedAt != nil {
		e := HistoryEvent{At: *d.DecidedAt, Kind: "decision", Status: string(d.Status), Ref: string(d.ID)}
		switch d.Status {
		case domain.ApprovalApproved:
			e.Title = "Approved by " + deref(d.DecidedBy)
			if h.Edited {
				e.Title += " (edited)"
			}
		case domain.ApprovalRejected:
			e.Title = "Rejected by " + deref(d.DecidedBy)
		case domain.ApprovalTimedOut:
			e.Title = "Timed out with no decision"
		default:
			e.Title = string(d.Status)
		}
		e.Detail = deref(d.DecisionNote)
		add(e)
	}
	for _, o := range d.Outcomes {
		title := o.NodeKey + " (" + o.NodeType + ")"
		add(HistoryEvent{At: o.OccurredAt, Kind: "outcome", Title: title, Detail: o.Summary, URL: o.URL,
			Status: o.Status, Ref: string(o.ID)})
		if o.Status == "succeeded" && actsExternally(o.NodeType) {
			h.Published = append(h.Published, o)
		}
	}
	if d.Run != nil && d.Run.FinishedAt != nil {
		add(HistoryEvent{At: *d.Run.FinishedAt, Kind: "run_finished", Status: string(d.Run.Status),
			Title: "Run " + string(d.Run.Status) + ", cost $" + strconv.FormatFloat(d.Run.CostUsd, 'f', 4, 64),
			Ref:   string(d.Run.ID)})
	}
	// ISO timestamps from every repository share one format, so they sort as text.
	sort.SliceStable(h.Timeline, func(i, j int) bool { return h.Timeline[i].At < h.Timeline[j].At })
	return h, nil
}

// actsExternally: integration and output nodes reach the outside world;
// triggers, AI, logic, intel and approval steps don't.
func actsExternally(nodeType string) bool {
	switch strings.SplitN(nodeType, ".", 2)[0] {
	case "trigger", "agent", "logic", "intel", "human":
		return false
	case "tool":
		return nodeType == "tool.http"
	}
	return true
}

func endedAs(st domain.RunStatus) string {
	switch st {
	case domain.RunSucceeded:
		return "finished"
	case domain.RunCanceled:
		return "been canceled"
	}
	return "failed"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
