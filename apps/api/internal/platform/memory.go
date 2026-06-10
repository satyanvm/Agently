package platform

import (
	"sort"
	"sync"

	"github.com/agently/api/internal/domain"
)

// MemoryData is the dataset an in-memory store is constructed from (produced by
// the seed). Mirrors the TS MemoryData in repositories/memory.ts.
type MemoryData struct {
	Workspace       domain.Workspace
	Members         []domain.Member
	Agents          []domain.AgentDefinition
	Workflows       []domain.Workflow
	Versions        []domain.WorkflowVersion
	Runs            []domain.Run
	RunAgents       []domain.RunAgent
	Messages        []domain.AgentMessage
	Artifacts       []domain.Artifact
	Logs            []domain.LogEntry
	BrowserSessions []domain.BrowserSession
	BrowserActions  []domain.BrowserAction
	BrowserShots    []domain.BrowserShot
	BrowserConsole  []ConsoleRow
	Notifications   []domain.Notification
	Activity        []domain.ActivityEvent
}

// ConsoleRow pairs a console line with its session (the TS store keeps these
// separate from the line itself).
type ConsoleRow struct {
	SessionID domain.BrowserSessionId
	Line      domain.BrowserConsoleLine
}

// store holds all mutable state behind one RWMutex. The TS backend is
// single-threaded; the Go server is concurrent, so reads take RLock and writes
// take Lock. Each repo below is a thin facade over this shared store.
type store struct {
	mu sync.RWMutex

	workspace domain.Workspace
	members   []domain.Member
	agents    []domain.AgentDefinition
	workflows []domain.Workflow
	versions  []domain.WorkflowVersion
	runs      []domain.Run
	runAgents []domain.RunAgent
	messages  []domain.AgentMessage
	artifacts []domain.Artifact
	logs      []domain.LogEntry
	sessions  []domain.BrowserSession
	actions   []domain.BrowserAction
	shots     []domain.BrowserShot
	console   []ConsoleRow
	notifs    []domain.Notification
	activity  []domain.ActivityEvent
}

// NewMemoryRepositories builds a complete in-memory Repositories from seed data.
func NewMemoryRepositories(data MemoryData) *Repositories {
	s := &store{
		workspace: data.Workspace,
		members:   append([]domain.Member(nil), data.Members...),
		agents:    append([]domain.AgentDefinition(nil), data.Agents...),
		workflows: append([]domain.Workflow(nil), data.Workflows...),
		versions:  append([]domain.WorkflowVersion(nil), data.Versions...),
		runs:      append([]domain.Run(nil), data.Runs...),
		runAgents: append([]domain.RunAgent(nil), data.RunAgents...),
		messages:  append([]domain.AgentMessage(nil), data.Messages...),
		artifacts: append([]domain.Artifact(nil), data.Artifacts...),
		logs:      append([]domain.LogEntry(nil), data.Logs...),
		sessions:  append([]domain.BrowserSession(nil), data.BrowserSessions...),
		actions:   append([]domain.BrowserAction(nil), data.BrowserActions...),
		shots:     append([]domain.BrowserShot(nil), data.BrowserShots...),
		console:   append([]ConsoleRow(nil), data.BrowserConsole...),
		notifs:    append([]domain.Notification(nil), data.Notifications...),
		activity:  append([]domain.ActivityEvent(nil), data.Activity...),
	}
	return &Repositories{
		Workspace:     &workspaceRepo{s},
		Members:       &memberRepo{s},
		Agents:        &agentRepo{s},
		Workflows:     &workflowRepo{s},
		Versions:      &versionRepo{s},
		Runs:          &runRepo{s},
		RunAgents:     &runAgentRepo{s},
		Messages:      &messageRepo{s},
		Artifacts:     &artifactRepo{s},
		Logs:          &logRepo{s},
		Browser:       &browserRepo{s},
		Notifications: &notificationRepo{s},
		Activity:      &activityRepo{s},
	}
}

/* --------------------------- workspace --------------------------- */

type workspaceRepo struct{ s *store }

func (r *workspaceRepo) Get() domain.Workspace {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.workspace
}

/* ---------------------------- members ---------------------------- */

type memberRepo struct{ s *store }

func (r *memberRepo) All() []domain.Member {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return append([]domain.Member(nil), r.s.members...)
}

func (r *memberRepo) GetByID(id domain.MemberId) (domain.Member, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, m := range r.s.members {
		if m.ID == id {
			return m, true
		}
	}
	return domain.Member{}, false
}

/* ----------------------------- agents ---------------------------- */

type agentRepo struct{ s *store }

func (r *agentRepo) All() []domain.AgentDefinition {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return append([]domain.AgentDefinition(nil), r.s.agents...)
}

func (r *agentRepo) GetByID(id domain.AgentDefinitionId) (domain.AgentDefinition, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, a := range r.s.agents {
		if a.ID == id {
			return a, true
		}
	}
	return domain.AgentDefinition{}, false
}

func (r *agentRepo) Insert(agent domain.AgentDefinition) domain.AgentDefinition {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.agents = append(r.s.agents, agent)
	return agent
}

/* ---------------------------- workflows -------------------------- */

type workflowRepo struct{ s *store }

func (r *workflowRepo) All() []domain.Workflow {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return append([]domain.Workflow(nil), r.s.workflows...)
}

func (r *workflowRepo) GetByID(id domain.WorkflowId) (domain.Workflow, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, w := range r.s.workflows {
		if w.ID == id {
			return w, true
		}
	}
	return domain.Workflow{}, false
}

func (r *workflowRepo) GetBySlug(slug string) (domain.Workflow, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, w := range r.s.workflows {
		if w.Slug == slug {
			return w, true
		}
	}
	return domain.Workflow{}, false
}

func (r *workflowRepo) Insert(w domain.Workflow) domain.Workflow {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.workflows = append(r.s.workflows, w)
	return w
}

func (r *workflowRepo) Update(id domain.WorkflowId, patch WorkflowPatch) (domain.Workflow, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.workflows {
		if r.s.workflows[i].ID == id {
			w := &r.s.workflows[i]
			if patch.AgentCount != nil {
				w.AgentCount = *patch.AgentCount
			}
			if patch.CurrentVersionID != nil {
				w.CurrentVersionID = patch.CurrentVersionID
			}
			if patch.DefaultInput != nil {
				w.DefaultInput = *patch.DefaultInput
			}
			if patch.UpdatedAt != nil {
				w.UpdatedAt = *patch.UpdatedAt
			}
			if patch.ArchivedAt != nil {
				w.ArchivedAt = patch.ArchivedAt
			}
			return *w, nil
		}
	}
	return domain.Workflow{}, domain.NotFound("workflow")
}

/* ---------------------------- versions --------------------------- */

type versionRepo struct{ s *store }

func (r *versionRepo) GetByID(id domain.WorkflowVersionId) (domain.WorkflowVersion, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, v := range r.s.versions {
		if v.ID == id {
			return v, true
		}
	}
	return domain.WorkflowVersion{}, false
}

func (r *versionRepo) ListByWorkflow(workflowID domain.WorkflowId) []domain.WorkflowVersion {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.WorkflowVersion{}
	for _, v := range r.s.versions {
		if v.WorkflowID == workflowID {
			out = append(out, v)
		}
	}
	return out
}

func (r *versionRepo) Insert(v domain.WorkflowVersion) domain.WorkflowVersion {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.versions = append(r.s.versions, v)
	return v
}

/* ------------------------------ runs ----------------------------- */

type runRepo struct{ s *store }

func (r *runRepo) All() []domain.Run {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return append([]domain.Run(nil), r.s.runs...)
}

func (r *runRepo) GetByID(id domain.RunId) (domain.Run, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, run := range r.s.runs {
		if run.ID == id {
			return run, true
		}
	}
	return domain.Run{}, false
}

func (r *runRepo) ListByWorkflow(workflowID domain.WorkflowId) []domain.Run {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.Run{}
	for _, run := range r.s.runs {
		if run.WorkflowID == workflowID {
			out = append(out, run)
		}
	}
	return out
}

func (r *runRepo) Insert(run domain.Run) domain.Run {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.runs = append(r.s.runs, run)
	return run
}

func (r *runRepo) Update(id domain.RunId, patch RunPatch) (domain.Run, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.runs {
		if r.s.runs[i].ID == id {
			run := &r.s.runs[i]
			if patch.Status != nil {
				run.Status = *patch.Status
			}
			if patch.Steps != nil {
				run.Steps = *patch.Steps
			}
			if patch.CurrentStep != nil {
				run.CurrentStep = *patch.CurrentStep
			}
			if patch.CostUsd != nil {
				run.CostUsd = *patch.CostUsd
			}
			if patch.Usage != nil {
				run.Usage = *patch.Usage
			}
			if patch.Error != nil {
				run.Error = *patch.Error
			}
			if patch.StartedAt != nil {
				run.StartedAt = patch.StartedAt
			}
			if patch.FinishedAt != nil {
				run.FinishedAt = patch.FinishedAt
			}
			return *run, nil
		}
	}
	return domain.Run{}, domain.NotFound("run")
}

func (r *runRepo) NextNumber(workflowID domain.WorkflowId) int {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	max := 0
	for _, run := range r.s.runs {
		if run.WorkflowID == workflowID && run.Number > max {
			max = run.Number
		}
	}
	return max + 1
}

/* --------------------------- run agents -------------------------- */

type runAgentRepo struct{ s *store }

func (r *runAgentRepo) ListByRun(runID domain.RunId) []domain.RunAgent {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.RunAgent{}
	for _, a := range r.s.runAgents {
		if a.RunID == runID {
			out = append(out, a)
		}
	}
	return out
}

func (r *runAgentRepo) GetByID(id domain.RunAgentId) (domain.RunAgent, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, a := range r.s.runAgents {
		if a.ID == id {
			return a, true
		}
	}
	return domain.RunAgent{}, false
}

func (r *runAgentRepo) InsertMany(agents []domain.RunAgent) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.runAgents = append(r.s.runAgents, agents...)
}

func (r *runAgentRepo) Update(id domain.RunAgentId, patch RunAgentPatch) (domain.RunAgent, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.runAgents {
		if r.s.runAgents[i].ID == id {
			a := &r.s.runAgents[i]
			if patch.Status != nil {
				a.Status = *patch.Status
			}
			if patch.Summary != nil {
				a.Summary = *patch.Summary
			}
			if patch.Metrics != nil {
				a.Metrics = *patch.Metrics
			}
			if patch.StartedAt != nil {
				a.StartedAt = patch.StartedAt
			}
			if patch.FinishedAt != nil {
				a.FinishedAt = patch.FinishedAt
			}
			return *a, nil
		}
	}
	return domain.RunAgent{}, domain.NotFound("run agent")
}

/* ---------------------------- messages --------------------------- */

type messageRepo struct{ s *store }

func (r *messageRepo) ListByRun(runID domain.RunId) []domain.AgentMessage {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.AgentMessage{}
	for _, m := range r.s.messages {
		if m.RunID == runID {
			out = append(out, m)
		}
	}
	return out
}

func (r *messageRepo) Insert(m domain.AgentMessage) domain.AgentMessage {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.messages = append(r.s.messages, m)
	return m
}

/* ---------------------------- artifacts -------------------------- */

type artifactRepo struct{ s *store }

func (r *artifactRepo) ListByRun(runID domain.RunId) []domain.Artifact {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.Artifact{}
	for _, a := range r.s.artifacts {
		if a.RunID == runID {
			out = append(out, a)
		}
	}
	return out
}

func (r *artifactRepo) Insert(a domain.Artifact) domain.Artifact {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.artifacts = append(r.s.artifacts, a)
	return a
}

/* ------------------------------ logs ----------------------------- */

type logRepo struct{ s *store }

func (r *logRepo) ListByRun(runID domain.RunId) []domain.LogEntry {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.LogEntry{}
	for _, l := range r.s.logs {
		if l.RunID == runID {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func (r *logRepo) Insert(entry domain.LogEntry) domain.LogEntry {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.logs = append(r.s.logs, entry)
	return entry
}

func (r *logRepo) MaxSeq(runID domain.RunId) int {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	max := -1
	for _, l := range r.s.logs {
		if l.RunID == runID && l.Seq > max {
			max = l.Seq
		}
	}
	return max
}

/* ----------------------------- browser --------------------------- */

type browserRepo struct{ s *store }

func (r *browserRepo) GetByID(id domain.BrowserSessionId) (domain.BrowserSession, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, sess := range r.s.sessions {
		if sess.ID == id {
			return sess, true
		}
	}
	return domain.BrowserSession{}, false
}

func (r *browserRepo) GetByRun(runID domain.RunId) (domain.BrowserSession, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, sess := range r.s.sessions {
		if sess.RunID == runID {
			return sess, true
		}
	}
	return domain.BrowserSession{}, false
}

func (r *browserRepo) InsertSession(session domain.BrowserSession) domain.BrowserSession {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.sessions = append(r.s.sessions, session)
	return session
}

func (r *browserRepo) Update(id domain.BrowserSessionId, patch BrowserSessionPatch) (domain.BrowserSession, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.sessions {
		if r.s.sessions[i].ID == id {
			sess := &r.s.sessions[i]
			if patch.Status != nil {
				sess.Status = *patch.Status
			}
			if patch.CurrentURL != nil {
				sess.CurrentURL = *patch.CurrentURL
			}
			if patch.PageTitle != nil {
				sess.PageTitle = *patch.PageTitle
			}
			if patch.PagesVisited != nil {
				sess.PagesVisited = *patch.PagesVisited
			}
			if patch.ActionsCount != nil {
				sess.ActionsCount = *patch.ActionsCount
			}
			if patch.FinishedAt != nil {
				sess.FinishedAt = patch.FinishedAt
			}
			return *sess, nil
		}
	}
	return domain.BrowserSession{}, domain.NotFound("browser session")
}

func (r *browserRepo) ListActions(id domain.BrowserSessionId) []domain.BrowserAction {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.BrowserAction{}
	for _, a := range r.s.actions {
		if a.SessionID == id {
			out = append(out, a)
		}
	}
	return out
}

func (r *browserRepo) ListShots(id domain.BrowserSessionId) []domain.BrowserShot {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.BrowserShot{}
	for _, sh := range r.s.shots {
		if sh.SessionID == id {
			out = append(out, sh)
		}
	}
	return out
}

func (r *browserRepo) ListConsole(id domain.BrowserSessionId) []domain.BrowserConsoleLine {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	out := []domain.BrowserConsoleLine{}
	for _, c := range r.s.console {
		if c.SessionID == id {
			out = append(out, c.Line)
		}
	}
	return out
}

func (r *browserRepo) InsertAction(action domain.BrowserAction) domain.BrowserAction {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.actions = append(r.s.actions, action)
	return action
}

func (r *browserRepo) InsertShot(shot domain.BrowserShot) domain.BrowserShot {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.shots = append(r.s.shots, shot)
	return shot
}

func (r *browserRepo) InsertConsole(id domain.BrowserSessionId, line domain.BrowserConsoleLine) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.console = append(r.s.console, ConsoleRow{SessionID: id, Line: line})
}

/* -------------------------- notifications ------------------------ */

type notificationRepo struct{ s *store }

func (r *notificationRepo) All() []domain.Notification {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return append([]domain.Notification(nil), r.s.notifs...)
}

func (r *notificationRepo) GetByID(id domain.NotificationId) (domain.Notification, bool) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, n := range r.s.notifs {
		if n.ID == id {
			return n, true
		}
	}
	return domain.Notification{}, false
}

func (r *notificationRepo) Insert(n domain.Notification) domain.Notification {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.notifs = append(r.s.notifs, n)
	return n
}

func (r *notificationRepo) Update(id domain.NotificationId, patch NotificationPatch) (domain.Notification, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for i := range r.s.notifs {
		if r.s.notifs[i].ID == id {
			n := &r.s.notifs[i]
			if patch.ReadAt != nil {
				n.ReadAt = patch.ReadAt
			}
			return *n, nil
		}
	}
	return domain.Notification{}, domain.NotFound("notification")
}

func (r *notificationRepo) MarkAllRead(workspaceID domain.WorkspaceId, at domain.Timestamp) int {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	updated := 0
	for i := range r.s.notifs {
		n := &r.s.notifs[i]
		if n.WorkspaceID == workspaceID && n.ReadAt == nil {
			readAt := at
			n.ReadAt = &readAt
			updated++
		}
	}
	return updated
}

/* ---------------------------- activity --------------------------- */

type activityRepo struct{ s *store }

func (r *activityRepo) All() []domain.ActivityEvent {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return append([]domain.ActivityEvent(nil), r.s.activity...)
}

func (r *activityRepo) Insert(e domain.ActivityEvent) domain.ActivityEvent {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.activity = append(r.s.activity, e)
	return e
}
