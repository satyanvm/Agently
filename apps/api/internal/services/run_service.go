package services

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

// RunService owns the run lifecycle: list/get/launch/cancel plus the write-path
// methods (start/progress/transitionAgent/finish) a real runner will call.
type RunService struct {
	deps Deps
	emit Emitter
	logs *LogService
}

func NewRunService(deps Deps, emit Emitter, logs *LogService) *RunService {
	return &RunService{deps: deps, emit: emit, logs: logs}
}

func (s *RunService) detail(run domain.Run) domain.RunDetail {
	return domain.RunDetail{
		Run:       run,
		Agents:    s.deps.Repos.RunAgents.ListByRun(run.ID),
		Messages:  s.deps.Repos.Messages.ListByRun(run.ID),
		Artifacts: s.deps.Repos.Artifacts.ListByRun(run.ID),
	}
}

func (s *RunService) requireRun(id domain.RunId) (domain.Run, error) {
	run, ok := s.deps.Repos.Runs.GetByID(id)
	if !ok {
		return domain.Run{}, domain.NotFound("run")
	}
	return run, nil
}

// List returns runs filtered by status/workflowId, newest first, paginated.
func (s *RunService) List(query validate.ListRunsQuery) domain.Page[domain.Run] {
	items := s.deps.Repos.Runs.All()
	platform.SortByNewest(items, func(r domain.Run) string {
		if r.StartedAt != nil {
			return string(*r.StartedAt)
		}
		return string(r.QueuedAt)
	})
	filtered := items[:0:0]
	for _, r := range items {
		if query.Status != "" && string(r.Status) != query.Status {
			continue
		}
		if query.WorkflowID != "" && string(r.WorkflowID) != query.WorkflowID {
			continue
		}
		filtered = append(filtered, r)
	}
	return platform.Paginate(filtered, query.PageQuery)
}

// Get returns a run with its expanded children.
func (s *RunService) Get(id domain.RunId) (domain.RunDetail, error) {
	run, err := s.requireRun(id)
	if err != nil {
		return domain.RunDetail{}, err
	}
	return s.detail(run), nil
}

// Launch creates a run, materializes its agent graph, emits events, appends
// bootstrap logs, and records activity.
func (s *RunService) Launch(slug string, input validate.LaunchRunInput) (domain.RunDetail, error) {
	wf, ok := s.deps.Repos.Workflows.GetBySlug(slug)
	if !ok {
		return domain.RunDetail{}, domain.NotFound("workflow")
	}

	now := domain.Timestamp(s.deps.Clock.ISO())
	runID := domain.NewRunId()

	triggeredBy := domain.Principal{Name: "Manual", Initials: "MA"}
	if members := s.deps.Repos.Members.All(); len(members) > 0 {
		triggeredBy = domain.Principal{Name: members[0].Name, Initials: members[0].Initials}
	}
	region := s.deps.Repos.Workspace.Get().DefaultRegion
	if input.Region != nil {
		region = *input.Region
	}
	total := wf.AgentCount * 4
	if total < 1 {
		total = 1
	}

	run := domain.Run{
		ID: runID, WorkspaceID: wf.WorkspaceID, WorkflowID: wf.ID, WorkflowVersionID: wf.CurrentVersionID,
		WorkflowName: wf.Name, WorkflowSlug: wf.Slug, Number: s.deps.Repos.Runs.NextNumber(wf.ID),
		Status: domain.RunRunning, Trigger: input.Trigger, TriggeredBy: triggeredBy, Region: region,
		Steps: domain.StepProgress{Done: 0, Total: total}, CurrentStep: "Provisioning sandbox",
		CostUsd: 0, Usage: domain.Usage{TokensIn: 0, TokensOut: 0}, Error: nil, BrowserSessionID: nil,
		QueuedAt: now, StartedAt: domain.Ptr(now), FinishedAt: nil,
	}
	s.deps.Repos.Runs.Insert(run)

	// Materialize run agents from the workflow's current version graph.
	var nodes []domain.GraphNode
	if wf.CurrentVersionID != nil {
		if version, ok := s.deps.Repos.Versions.GetByID(*wf.CurrentVersionID); ok {
			nodes = version.Nodes
		}
	}
	keyToID := make(map[string]domain.RunAgentId, len(nodes))
	for _, n := range nodes {
		keyToID[n.Key] = domain.NewRunAgentId()
	}
	runAgents := make([]domain.RunAgent, 0, len(nodes))
	for _, n := range nodes {
		entry := len(n.DependsOn) == 0
		status := domain.AgentIdle
		var startedAt *domain.Timestamp
		var runtimeMs *int
		if entry {
			status = domain.AgentRunning
			startedAt = domain.Ptr(now)
			runtimeMs = domain.Ptr(0)
		}
		deps := make([]domain.RunAgentId, 0, len(n.DependsOn))
		for _, k := range n.DependsOn {
			if id, ok := keyToID[k]; ok {
				deps = append(deps, id)
			}
		}
		runAgents = append(runAgents, domain.RunAgent{
			ID: keyToID[n.Key], RunID: runID, AgentDefinitionID: n.AgentDefinitionID, Name: n.Name,
			Role: n.Role, Model: n.Model, Status: status, DependsOn: deps, Col: n.Col, Row: n.Row,
			Summary: "", Metrics: domain.AgentMetrics{Tokens: 0, CostUsd: 0, RuntimeMs: runtimeMs, ToolCalls: 0, Progress: 0},
			StartedAt: startedAt, FinishedAt: nil,
		})
	}
	s.deps.Repos.RunAgents.InsertMany(runAgents)

	s.emit(domain.RunQueuedEvent{RunID: runID, WorkflowID: wf.ID})
	s.emit(domain.RunStartedEvent{RunID: runID})

	s.logs.Append(runID, AppendLogInput{Level: domain.LevelInfo, Channel: domain.ChannelSystem, Source: "scheduler", Message: "Run #" + itoa(run.Number) + " launched (" + string(input.Trigger) + ")"})
	s.logs.Append(runID, AppendLogInput{Level: domain.LevelInfo, Channel: domain.ChannelSystem, Source: "runtime", Message: "Provisioning sandbox · " + region})

	s.deps.Repos.Activity.Insert(domain.ActivityEvent{
		ID: domain.NewActivityId(), WorkspaceID: wf.WorkspaceID, Kind: domain.ActivityRun,
		Actor: triggeredBy.Name, Text: "started run #" + itoa(run.Number) + " of " + wf.Name,
		WorkflowSlug: domain.Ptr(wf.Slug), RunID: domain.Ptr(runID), At: now,
	})

	return s.detail(run), nil
}

// Cancel requests cancellation of a non-terminal run.
func (s *RunService) Cancel(id domain.RunId) (domain.Run, error) {
	run, err := s.requireRun(id)
	if err != nil {
		return domain.Run{}, err
	}
	if domain.IsTerminalRunStatus(run.Status) {
		return domain.Run{}, domain.Conflict("run is already " + string(run.Status))
	}
	now := domain.Timestamp(s.deps.Clock.ISO())
	canceledStep := "Canceled"
	updated, err := s.deps.Repos.Runs.Update(id, platform.RunPatch{
		Status: domain.Ptr(domain.RunCanceled), FinishedAt: &now, CurrentStep: &canceledStep,
	})
	if err != nil {
		return domain.Run{}, err
	}
	s.logs.Append(id, AppendLogInput{Level: domain.LevelWarn, Channel: domain.ChannelSystem, Source: "runtime", Message: "Run canceled by user"})
	s.emit(domain.RunFinishedEvent{RunID: id, Status: domain.RunCanceled, Error: nil})
	return updated, nil
}

/* ---- lifecycle write-path (what a real runner/worker will call) ---- */

// Start marks a run running.
func (s *RunService) Start(id domain.RunId) (domain.Run, error) {
	run, err := s.requireRun(id)
	if err != nil {
		return domain.Run{}, err
	}
	patch := platform.RunPatch{Status: domain.Ptr(domain.RunRunning)}
	if run.StartedAt == nil {
		patch.StartedAt = domain.Ptr(domain.Timestamp(s.deps.Clock.ISO()))
	}
	updated, err := s.deps.Repos.Runs.Update(id, patch)
	if err != nil {
		return domain.Run{}, err
	}
	s.emit(domain.RunStartedEvent{RunID: id})
	return updated, nil
}

// Progress updates the run's step counter and current step.
func (s *RunService) Progress(id domain.RunId, steps domain.StepProgress, currentStep string) (domain.Run, error) {
	if _, err := s.requireRun(id); err != nil {
		return domain.Run{}, err
	}
	updated, err := s.deps.Repos.Runs.Update(id, platform.RunPatch{Steps: &steps, CurrentStep: &currentStep})
	if err != nil {
		return domain.Run{}, err
	}
	s.emit(domain.RunProgressEvent{RunID: id, StepsDone: steps.Done, StepsTotal: steps.Total, CurrentStep: currentStep})
	return updated, nil
}

// TransitionAgent moves a run agent to a new status.
func (s *RunService) TransitionAgent(runID domain.RunId, agentID domain.RunAgentId, to domain.AgentStatus) (domain.RunAgent, error) {
	agent, ok := s.deps.Repos.RunAgents.GetByID(agentID)
	if !ok {
		return domain.RunAgent{}, domain.NotFound("run agent")
	}
	from := agent.Status
	patch := platform.RunAgentPatch{Status: &to}
	if to == domain.AgentSucceeded || to == domain.AgentFailed {
		patch.FinishedAt = domain.Ptr(domain.Timestamp(s.deps.Clock.ISO()))
	}
	updated, err := s.deps.Repos.RunAgents.Update(agentID, patch)
	if err != nil {
		return domain.RunAgent{}, err
	}
	s.emit(domain.RunAgentTransitionedEvent{RunID: runID, AgentID: agentID, From: from, To: to})
	return updated, nil
}

// Finish settles a run as succeeded or failed.
func (s *RunService) Finish(id domain.RunId, status domain.RunStatus, errMsg *string) (domain.Run, error) {
	if _, err := s.requireRun(id); err != nil {
		return domain.Run{}, err
	}
	now := domain.Timestamp(s.deps.Clock.ISO())
	step := "Completed"
	if status != domain.RunSucceeded {
		step = "Failed"
		if errMsg != nil {
			step = *errMsg
		}
	}
	updated, err := s.deps.Repos.Runs.Update(id, platform.RunPatch{
		Status: &status, FinishedAt: &now, Error: &errMsg, CurrentStep: &step,
	})
	if err != nil {
		return domain.Run{}, err
	}
	s.emit(domain.RunFinishedEvent{RunID: id, Status: status, Error: errMsg})
	return updated, nil
}
