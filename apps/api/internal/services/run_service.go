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

// ArtifactContent returns one artifact's filename + full content for download. The
// content lives in the artifact's preview column (the worker writes the whole digest
// there). Returns NotFound if the run has no artifact with that id.
func (s *RunService) ArtifactContent(runID domain.RunId, artifactID domain.ArtifactId) (name, content string, err error) {
	if _, err := s.requireRun(runID); err != nil {
		return "", "", err
	}
	for _, a := range s.deps.Repos.Artifacts.ListByRun(runID) {
		if a.ID == artifactID {
			body := ""
			if a.Preview != nil {
				body = *a.Preview
			}
			return a.Name, body, nil
		}
	}
	return "", "", domain.NotFound("artifact")
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
	// Merge the workflow's default input (the planned template: topic/email/sources/
	// urls) UNDER the per-run input, so a one-click "Run now" inherits the plan while
	// an explicit per-run value still wins.
	runInput := mergeInput(wf.DefaultInput, input.Input)
	total := wf.AgentCount * 4
	if total < 1 {
		total = 1
	}

	// Created as QUEUED: the control plane (this API) only enqueues; the data
	// plane (worker) claims it via claim_next_run() and drives execution. The old
	// flow marked it RUNNING here because there was no worker — that's now wrong.
	//
	// Engine: an explicit request wins; otherwise route by the graph itself.
	// TYPED graphs (every node carries a catalog type — the compiler and the
	// visual builder both emit these) can only be interpreted by the Temporal
	// reasoner's dynamic engine. Legacy untyped digest graphs keep the native
	// Go worker.
	engine := input.Engine
	if engine == "" {
		var nodes []domain.GraphNode
		if wf.CurrentVersionID != nil {
			if version, ok := s.deps.Repos.Versions.GetByID(*wf.CurrentVersionID); ok {
				nodes = version.Nodes
			}
		}
		engine = engineForNodes(nodes)
	}
	queuedStep := "Queued"
	if engine == "temporal" {
		// The Temporal reasoner is dispatched from a separate poller and reports its
		// own progress; flag the wait state distinctly so the UI reads honestly.
		queuedStep = "Queued (temporal)"
	}
	run := domain.Run{
		ID: runID, WorkspaceID: wf.WorkspaceID, WorkflowID: wf.ID, WorkflowVersionID: wf.CurrentVersionID,
		WorkflowName: wf.Name, WorkflowSlug: wf.Slug, Number: s.deps.Repos.Runs.NextNumber(wf.ID),
		Status: domain.RunQueued, Trigger: input.Trigger, Input: runInput, TriggeredBy: triggeredBy, Region: region,
		Steps: domain.StepProgress{Done: 0, Total: total}, CurrentStep: queuedStep,
		CostUsd: 0, Usage: domain.Usage{TokensIn: 0, TokensOut: 0}, Error: nil, BrowserSessionID: nil,
		Engine: engine,
		QueuedAt: now, StartedAt: nil, FinishedAt: nil,
	}
	s.deps.Repos.Runs.Insert(run)

	// Temporal runs are driven by the LangGraph reasoner, which materializes its own
	// graph nodes (plan/browse/synthesize/deliver) as it executes — so the control
	// plane does NOT pre-materialize run agents from the stored workflow graph here.
	if engine == "temporal" {
		s.emit(domain.RunQueuedEvent{RunID: runID, WorkflowID: wf.ID})
		s.logs.Append(runID, AppendLogInput{Level: domain.LevelInfo, Channel: domain.ChannelSystem, Source: "scheduler", Message: "Run #" + itoa(run.Number) + " launched on the Temporal reasoner (" + string(input.Trigger) + ")"})
		s.logs.Append(runID, AppendLogInput{Level: domain.LevelInfo, Channel: domain.ChannelSystem, Source: "runtime", Message: "Awaiting reasoner dispatch · " + region})
		s.deps.Repos.Activity.Insert(domain.ActivityEvent{
			ID: domain.NewActivityId(), WorkspaceID: wf.WorkspaceID, Kind: domain.ActivityRun,
			Actor: triggeredBy.Name, Text: "started run #" + itoa(run.Number) + " of " + wf.Name + " (temporal)",
			WorkflowSlug: domain.Ptr(wf.Slug), RunID: domain.Ptr(runID), At: now,
		})
		return s.detail(run), nil
	}

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
		// All agents start idle; the worker (data plane) drives their status as it
		// executes the DAG. (Pre-worker, entry nodes were marked running here — the
		// same control/data-plane leak we fixed for run status.)
		status := domain.AgentIdle
		var startedAt *domain.Timestamp
		var runtimeMs *int
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

// engineForNodes routes a run to its execution plane: any typed node means the
// Temporal reasoner (the only engine that interprets catalog node types); a legacy
// untyped digest graph keeps the native Go worker. Mirrors the dispatcher's
// dynamic-vs-static split on the reasoner side.
func engineForNodes(nodes []domain.GraphNode) string {
	for _, n := range nodes {
		if n.Type != "" {
			return "temporal"
		}
	}
	return "native"
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

// mergeInput overlays per-run values on top of the workflow's default input. A key
// present in both takes the per-run value; defaults fill in the rest. Returns a fresh
// map (never aliases either argument), always non-nil.
func mergeInput(defaults, perRun map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(perRun))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range perRun {
		out[k] = v
	}
	return out
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
