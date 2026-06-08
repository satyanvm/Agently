package platform

import (
	"fmt"
	"strings"

	"github.com/agently/api/internal/domain"
	"github.com/jackc/pgx/v5"
)

// One file, 13 repositories — each a thin SQL facade implementing the same
// interface as its in-memory twin in memory.go. Read the patterns once and the
// rest is repetition:
//   - read path: a `scanX(row)` helper turns a row into a domain struct;
//     list methods Query+loop, get methods QueryRow.
//   - write path: Insert execs and returns the input; Update builds a dynamic
//     SET list from the non-nil patch fields, then re-reads the row.

/* ----------------------------- workspace ----------------------------- */

type pgWorkspaceRepo struct{ s *pgStore }

func (r *pgWorkspaceRepo) Get() domain.Workspace {
	row := r.s.pool.QueryRow(bg(),
		`select id, name, slug, plan, default_region, created_at from workspaces limit 1`)
	var w domain.Workspace
	var id, name, slug, plan, region string
	var created any
	if err := row.Scan(&id, &name, &slug, &plan, &region, &created); err != nil {
		// "no rows" is expected on an empty DB (the seeder uses it as its check),
		// so don't log it as an error; other scan failures still surface.
		if err != pgx.ErrNoRows {
			r.s.fail("workspace.Get", err)
		}
		return domain.Workspace{}
	}
	w.ID = domain.WorkspaceId(id)
	w.Name, w.Slug = name, slug
	w.Plan = domain.WorkspacePlan(plan)
	w.DefaultRegion = region
	w.CreatedAt = anyTs(created)
	return w
}

/* ------------------------------ members ------------------------------ */

type pgMemberRepo struct{ s *pgStore }

func scanMember(row pgx.Row) (domain.Member, error) {
	var m domain.Member
	var id, ws, name, email, initials, role string
	var created any
	if err := row.Scan(&id, &ws, &name, &email, &initials, &role, &created); err != nil {
		return m, err
	}
	m.ID = domain.MemberId(id)
	m.WorkspaceID = domain.WorkspaceId(ws)
	m.Name, m.Email, m.Initials = name, email, initials
	m.Role = domain.MemberRole(role)
	m.CreatedAt = anyTs(created)
	return m, nil
}

const memberCols = `id, workspace_id, name, email, initials, role, created_at`

func (r *pgMemberRepo) All() []domain.Member {
	rows, err := r.s.pool.Query(bg(), `select `+memberCols+` from members order by created_at`)
	if err != nil {
		r.s.fail("member.All", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			r.s.fail("member.All scan", err)
			return out
		}
		out = append(out, m)
	}
	return out
}

func (r *pgMemberRepo) GetByID(id domain.MemberId) (domain.Member, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+memberCols+` from members where id=$1`, string(id))
	m, err := scanMember(row)
	if err != nil {
		return domain.Member{}, false
	}
	return m, true
}

/* ------------------------- agent definitions ------------------------- */

type pgAgentRepo struct{ s *pgStore }

func scanAgent(row pgx.Row) (domain.AgentDefinition, error) {
	var a domain.AgentDefinition
	var id, ws, name, role, model, desc string
	var tools []string
	var config []byte
	var created, updated any
	if err := row.Scan(&id, &ws, &name, &role, &model, &desc, &tools, &config, &created, &updated); err != nil {
		return a, err
	}
	a.ID = domain.AgentDefinitionId(id)
	a.WorkspaceID = domain.WorkspaceId(ws)
	a.Name, a.Role, a.Model, a.Description = name, domain.AgentRole(role), model, desc
	a.Tools = tools
	a.Config = map[string]any{}
	jsonInto(config, &a.Config)
	a.CreatedAt, a.UpdatedAt = anyTs(created), anyTs(updated)
	return a, nil
}

const agentCols = `id, workspace_id, name, role, model, description, tools, config, created_at, updated_at`

func (r *pgAgentRepo) All() []domain.AgentDefinition {
	rows, err := r.s.pool.Query(bg(), `select `+agentCols+` from agent_definitions order by created_at`)
	if err != nil {
		r.s.fail("agent.All", err)
		return nil
	}
	defer rows.Close()
	out := []domain.AgentDefinition{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			r.s.fail("agent.All scan", err)
			return out
		}
		out = append(out, a)
	}
	return out
}

func (r *pgAgentRepo) GetByID(id domain.AgentDefinitionId) (domain.AgentDefinition, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+agentCols+` from agent_definitions where id=$1`, string(id))
	a, err := scanAgent(row)
	if err != nil {
		return domain.AgentDefinition{}, false
	}
	return a, true
}

func (r *pgAgentRepo) Insert(a domain.AgentDefinition) domain.AgentDefinition {
	_, err := r.s.pool.Exec(bg(),
		`insert into agent_definitions (`+agentCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		string(a.ID), string(a.WorkspaceID), a.Name, string(a.Role), a.Model, a.Description,
		a.Tools, jsonArg(a.Config), tsArg(a.CreatedAt), tsArg(a.UpdatedAt))
	r.s.fail("agent.Insert", err)
	return a
}

/* ------------------------------ workflows ---------------------------- */

type pgWorkflowRepo struct{ s *pgStore }

func scanWorkflow(row pgx.Row) (domain.Workflow, error) {
	var w domain.Workflow
	var id, ws, slug, name, desc, trigger string
	var schedule, ownerID, currentVer *string
	var tags []string
	var agentCount int
	var created, updated any
	var archived any
	if err := row.Scan(&id, &ws, &slug, &name, &desc, &trigger, &schedule, &tags,
		&ownerID, &agentCount, &currentVer, &created, &updated, &archived); err != nil {
		return w, err
	}
	w.ID = domain.WorkflowId(id)
	w.WorkspaceID = domain.WorkspaceId(ws)
	w.Slug, w.Name, w.Description = slug, name, desc
	w.Trigger = domain.TriggerType(trigger)
	w.Schedule = schedule
	w.Tags = tags
	if ownerID != nil {
		oid := domain.MemberId(*ownerID)
		w.OwnerID = &oid
	}
	w.AgentCount = agentCount
	if currentVer != nil {
		cv := domain.WorkflowVersionId(*currentVer)
		w.CurrentVersionID = &cv
	}
	w.CreatedAt, w.UpdatedAt = anyTs(created), anyTs(updated)
	w.ArchivedAt = anyTsPtr(archived)
	return w, nil
}

const workflowCols = `id, workspace_id, slug, name, description, trigger, schedule, tags, owner_id, agent_count, current_version_id, created_at, updated_at, archived_at`

func (r *pgWorkflowRepo) All() []domain.Workflow {
	rows, err := r.s.pool.Query(bg(), `select `+workflowCols+` from workflows order by created_at`)
	if err != nil {
		r.s.fail("workflow.All", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Workflow{}
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			r.s.fail("workflow.All scan", err)
			return out
		}
		out = append(out, w)
	}
	return out
}

func (r *pgWorkflowRepo) GetByID(id domain.WorkflowId) (domain.Workflow, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+workflowCols+` from workflows where id=$1`, string(id))
	w, err := scanWorkflow(row)
	if err != nil {
		return domain.Workflow{}, false
	}
	return w, true
}

func (r *pgWorkflowRepo) GetBySlug(slug string) (domain.Workflow, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+workflowCols+` from workflows where slug=$1`, slug)
	w, err := scanWorkflow(row)
	if err != nil {
		return domain.Workflow{}, false
	}
	return w, true
}

func (r *pgWorkflowRepo) Insert(w domain.Workflow) domain.Workflow {
	_, err := r.s.pool.Exec(bg(),
		`insert into workflows (`+workflowCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		string(w.ID), string(w.WorkspaceID), w.Slug, w.Name, w.Description, string(w.Trigger),
		strArg(w.Schedule), w.Tags, idPtrArg(w.OwnerID), w.AgentCount, idPtrArg(w.CurrentVersionID),
		tsArg(w.CreatedAt), tsArg(w.UpdatedAt), tsPtrArg(w.ArchivedAt))
	r.s.fail("workflow.Insert", err)
	return w
}

func (r *pgWorkflowRepo) Update(id domain.WorkflowId, patch WorkflowPatch) (domain.Workflow, error) {
	b := newSetBuilder()
	if patch.AgentCount != nil {
		b.add("agent_count", *patch.AgentCount)
	}
	if patch.CurrentVersionID != nil {
		b.add("current_version_id", string(*patch.CurrentVersionID))
	}
	if patch.UpdatedAt != nil {
		b.add("updated_at", tsArg(*patch.UpdatedAt))
	}
	if patch.ArchivedAt != nil {
		b.add("archived_at", tsArg(*patch.ArchivedAt))
	}
	if err := b.exec(r.s, "workflows", string(id)); err != nil {
		return domain.Workflow{}, err
	}
	w, ok := r.GetByID(id)
	if !ok {
		return domain.Workflow{}, domain.NotFound("workflow")
	}
	return w, nil
}

/* --------------------------- workflow versions ----------------------- */

type pgVersionRepo struct{ s *pgStore }

func scanVersion(row pgx.Row) (domain.WorkflowVersion, error) {
	var v domain.WorkflowVersion
	var id, wf string
	var version int
	var nodes []byte
	var note, createdBy *string
	var created any
	if err := row.Scan(&id, &wf, &version, &nodes, &note, &createdBy, &created); err != nil {
		return v, err
	}
	v.ID = domain.WorkflowVersionId(id)
	v.WorkflowID = domain.WorkflowId(wf)
	v.Version = version
	v.Nodes = []domain.GraphNode{}
	jsonInto(nodes, &v.Nodes)
	if note != nil {
		v.Note = *note
	}
	if createdBy != nil {
		cb := domain.MemberId(*createdBy)
		v.CreatedBy = &cb
	}
	v.CreatedAt = anyTs(created)
	return v, nil
}

const versionCols = `id, workflow_id, version, nodes, note, created_by, created_at`

func (r *pgVersionRepo) GetByID(id domain.WorkflowVersionId) (domain.WorkflowVersion, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+versionCols+` from workflow_versions where id=$1`, string(id))
	v, err := scanVersion(row)
	if err != nil {
		return domain.WorkflowVersion{}, false
	}
	return v, true
}

func (r *pgVersionRepo) ListByWorkflow(workflowID domain.WorkflowId) []domain.WorkflowVersion {
	rows, err := r.s.pool.Query(bg(),
		`select `+versionCols+` from workflow_versions where workflow_id=$1 order by version`, string(workflowID))
	if err != nil {
		r.s.fail("version.ListByWorkflow", err)
		return nil
	}
	defer rows.Close()
	out := []domain.WorkflowVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			r.s.fail("version.ListByWorkflow scan", err)
			return out
		}
		out = append(out, v)
	}
	return out
}

func (r *pgVersionRepo) Insert(v domain.WorkflowVersion) domain.WorkflowVersion {
	_, err := r.s.pool.Exec(bg(),
		`insert into workflow_versions (`+versionCols+`) values ($1,$2,$3,$4,$5,$6,$7)`,
		string(v.ID), string(v.WorkflowID), v.Version, jsonArg(v.Nodes),
		noteArg(v.Note), idPtrArg(v.CreatedBy), tsArg(v.CreatedAt))
	r.s.fail("version.Insert", err)
	return v
}

/* -------------------------------- runs ------------------------------- */

type pgRunRepo struct{ s *pgStore }

// runCols selects from runs joined to workflows for the derived name/slug.
const runSelect = `select r.id, r.workspace_id, r.workflow_id, r.workflow_version_id,
  w.name, w.slug, r.number, r.status, r.trigger, r.input, r.triggered_by, r.region,
  r.steps_done, r.steps_total, r.current_step, r.cost_usd::float8,
  r.tokens_in, r.tokens_out, r.error, r.browser_session_id,
  r.queued_at, r.started_at, r.finished_at
  from runs r join workflows w on w.id = r.workflow_id`

func scanRun(row pgx.Row) (domain.Run, error) {
	var rn domain.Run
	var id, ws, wf string
	var verID *string
	var wfName, wfSlug, status, trigger, region, currentStep string
	var triggeredBy, input []byte
	var number, stepsDone, stepsTotal int
	var costUsd float64
	var tokensIn, tokensOut int64
	var errStr, bsID *string
	var queued any
	var started, finished any
	if err := row.Scan(&id, &ws, &wf, &verID, &wfName, &wfSlug, &number, &status, &trigger,
		&input, &triggeredBy, &region, &stepsDone, &stepsTotal, &currentStep, &costUsd,
		&tokensIn, &tokensOut, &errStr, &bsID, &queued, &started, &finished); err != nil {
		return rn, err
	}
	rn.Input = map[string]any{}
	jsonInto(input, &rn.Input)
	rn.ID = domain.RunId(id)
	rn.WorkspaceID = domain.WorkspaceId(ws)
	rn.WorkflowID = domain.WorkflowId(wf)
	if verID != nil {
		v := domain.WorkflowVersionId(*verID)
		rn.WorkflowVersionID = &v
	}
	rn.WorkflowName, rn.WorkflowSlug = wfName, wfSlug
	rn.Number = number
	rn.Status = domain.RunStatus(status)
	rn.Trigger = domain.TriggerType(trigger)
	jsonInto(triggeredBy, &rn.TriggeredBy)
	rn.Region = region
	rn.Steps = domain.StepProgress{Done: stepsDone, Total: stepsTotal}
	rn.CurrentStep = currentStep
	rn.CostUsd = costUsd
	rn.Usage = domain.Usage{TokensIn: int(tokensIn), TokensOut: int(tokensOut)}
	rn.Error = errStr
	if bsID != nil {
		b := domain.BrowserSessionId(*bsID)
		rn.BrowserSessionID = &b
	}
	rn.QueuedAt = anyTs(queued)
	rn.StartedAt = anyTsPtr(started)
	rn.FinishedAt = anyTsPtr(finished)
	return rn, nil
}

func (r *pgRunRepo) All() []domain.Run {
	rows, err := r.s.pool.Query(bg(), runSelect+` order by r.queued_at desc`)
	if err != nil {
		r.s.fail("run.All", err)
		return nil
	}
	defer rows.Close()
	return collectRuns(r.s, rows)
}

func (r *pgRunRepo) GetByID(id domain.RunId) (domain.Run, bool) {
	row := r.s.pool.QueryRow(bg(), runSelect+` where r.id=$1`, string(id))
	rn, err := scanRun(row)
	if err != nil {
		return domain.Run{}, false
	}
	return rn, true
}

func (r *pgRunRepo) ListByWorkflow(workflowID domain.WorkflowId) []domain.Run {
	rows, err := r.s.pool.Query(bg(), runSelect+` where r.workflow_id=$1 order by r.number desc`, string(workflowID))
	if err != nil {
		r.s.fail("run.ListByWorkflow", err)
		return nil
	}
	defer rows.Close()
	return collectRuns(r.s, rows)
}

func collectRuns(s *pgStore, rows pgx.Rows) []domain.Run {
	out := []domain.Run{}
	for rows.Next() {
		rn, err := scanRun(rows)
		if err != nil {
			s.fail("run scan", err)
			return out
		}
		out = append(out, rn)
	}
	return out
}

func (r *pgRunRepo) Insert(run domain.Run) domain.Run {
	_, err := r.s.pool.Exec(bg(),
		`insert into runs (id, workspace_id, workflow_id, workflow_version_id, number, status,
		   trigger, input, triggered_by, region, steps_done, steps_total, current_step, cost_usd,
		   tokens_in, tokens_out, error, browser_session_id, queued_at, started_at, finished_at)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		string(run.ID), string(run.WorkspaceID), string(run.WorkflowID), idPtrArg(run.WorkflowVersionID),
		run.Number, string(run.Status), string(run.Trigger), jsonArg(orEmptyMap(run.Input)), jsonArg(run.TriggeredBy), run.Region,
		run.Steps.Done, run.Steps.Total, run.CurrentStep, run.CostUsd,
		run.Usage.TokensIn, run.Usage.TokensOut, strArg(run.Error), idPtrArg(run.BrowserSessionID),
		tsArg(run.QueuedAt), tsPtrArg(run.StartedAt), tsPtrArg(run.FinishedAt))
	r.s.fail("run.Insert", err)
	return run
}

func (r *pgRunRepo) Update(id domain.RunId, patch RunPatch) (domain.Run, error) {
	b := newSetBuilder()
	if patch.Status != nil {
		b.add("status", string(*patch.Status))
	}
	if patch.Steps != nil {
		b.add("steps_done", patch.Steps.Done)
		b.add("steps_total", patch.Steps.Total)
	}
	if patch.CurrentStep != nil {
		b.add("current_step", *patch.CurrentStep)
	}
	if patch.CostUsd != nil {
		b.add("cost_usd", *patch.CostUsd)
	}
	if patch.Usage != nil {
		b.add("tokens_in", patch.Usage.TokensIn)
		b.add("tokens_out", patch.Usage.TokensOut)
	}
	if patch.Error != nil { // **string: set vs set-to-null
		b.add("error", strArg(*patch.Error))
	}
	if patch.StartedAt != nil {
		b.add("started_at", tsArg(*patch.StartedAt))
	}
	if patch.FinishedAt != nil {
		b.add("finished_at", tsArg(*patch.FinishedAt))
	}
	if err := b.exec(r.s, "runs", string(id)); err != nil {
		return domain.Run{}, err
	}
	rn, ok := r.GetByID(id)
	if !ok {
		return domain.Run{}, domain.NotFound("run")
	}
	return rn, nil
}

func (r *pgRunRepo) NextNumber(workflowID domain.WorkflowId) int {
	row := r.s.pool.QueryRow(bg(),
		`select coalesce(max(number),0)+1 from runs where workflow_id=$1`, string(workflowID))
	var n int
	if err := row.Scan(&n); err != nil {
		r.s.fail("run.NextNumber", err)
		return 1
	}
	return n
}

/* ----------------------------- run agents ---------------------------- */

type pgRunAgentRepo struct{ s *pgStore }

func scanRunAgent(row pgx.Row) (domain.RunAgent, error) {
	var a domain.RunAgent
	var id, run string
	var agentDef *string
	var name, role, model, status, summary string
	var dependsOn []string
	var col, rowN int
	var metrics []byte
	var started, finished any
	if err := row.Scan(&id, &run, &agentDef, &name, &role, &model, &status, &dependsOn,
		&col, &rowN, &summary, &metrics, &started, &finished); err != nil {
		return a, err
	}
	a.ID = domain.RunAgentId(id)
	a.RunID = domain.RunId(run)
	if agentDef != nil {
		ad := domain.AgentDefinitionId(*agentDef)
		a.AgentDefinitionID = &ad
	}
	a.Name, a.Role, a.Model = name, domain.AgentRole(role), model
	a.Status = domain.AgentStatus(status)
	a.DependsOn = toRunAgentIds(dependsOn)
	a.Col, a.Row, a.Summary = col, rowN, summary
	jsonInto(metrics, &a.Metrics)
	a.StartedAt = anyTsPtr(started)
	a.FinishedAt = anyTsPtr(finished)
	return a, nil
}

const runAgentCols = `id, run_id, agent_definition_id, name, role, model, status, depends_on, col, "row", summary, metrics, started_at, finished_at`

func (r *pgRunAgentRepo) ListByRun(runID domain.RunId) []domain.RunAgent {
	rows, err := r.s.pool.Query(bg(), `select `+runAgentCols+` from run_agents where run_id=$1`, string(runID))
	if err != nil {
		r.s.fail("runAgent.ListByRun", err)
		return nil
	}
	defer rows.Close()
	out := []domain.RunAgent{}
	for rows.Next() {
		a, err := scanRunAgent(rows)
		if err != nil {
			r.s.fail("runAgent.ListByRun scan", err)
			return out
		}
		out = append(out, a)
	}
	return out
}

func (r *pgRunAgentRepo) GetByID(id domain.RunAgentId) (domain.RunAgent, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+runAgentCols+` from run_agents where id=$1`, string(id))
	a, err := scanRunAgent(row)
	if err != nil {
		return domain.RunAgent{}, false
	}
	return a, true
}

func (r *pgRunAgentRepo) InsertMany(agents []domain.RunAgent) {
	if len(agents) == 0 {
		return
	}
	batch := &pgx.Batch{}
	for _, a := range agents {
		batch.Queue(
			`insert into run_agents (`+runAgentCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			string(a.ID), string(a.RunID), idPtrArg(a.AgentDefinitionID), a.Name, string(a.Role), a.Model,
			string(a.Status), fromRunAgentIds(a.DependsOn), a.Col, a.Row, a.Summary, jsonArg(a.Metrics),
			tsPtrArg(a.StartedAt), tsPtrArg(a.FinishedAt))
	}
	br := r.s.pool.SendBatch(bg(), batch)
	defer br.Close()
	for range agents {
		if _, err := br.Exec(); err != nil {
			r.s.fail("runAgent.InsertMany", err)
			return
		}
	}
}

func (r *pgRunAgentRepo) Update(id domain.RunAgentId, patch RunAgentPatch) (domain.RunAgent, error) {
	b := newSetBuilder()
	if patch.Status != nil {
		b.add("status", string(*patch.Status))
	}
	if patch.Summary != nil {
		b.add("summary", *patch.Summary)
	}
	if patch.Metrics != nil {
		b.add("metrics", jsonArg(*patch.Metrics))
	}
	if patch.StartedAt != nil {
		b.add("started_at", tsArg(*patch.StartedAt))
	}
	if patch.FinishedAt != nil {
		b.add("finished_at", tsArg(*patch.FinishedAt))
	}
	if err := b.exec(r.s, "run_agents", string(id)); err != nil {
		return domain.RunAgent{}, err
	}
	a, ok := r.GetByID(id)
	if !ok {
		return domain.RunAgent{}, domain.NotFound("run agent")
	}
	return a, nil
}

/* ------------------------------ messages ----------------------------- */

type pgMessageRepo struct{ s *pgStore }

func (r *pgMessageRepo) ListByRun(runID domain.RunId) []domain.AgentMessage {
	rows, err := r.s.pool.Query(bg(),
		`select id, run_id, from_agent_id, to_agent_id, label, at from agent_messages where run_id=$1 order by at`,
		string(runID))
	if err != nil {
		r.s.fail("message.ListByRun", err)
		return nil
	}
	defer rows.Close()
	out := []domain.AgentMessage{}
	for rows.Next() {
		var m domain.AgentMessage
		var id, run, from, to, label string
		var at any
		if err := rows.Scan(&id, &run, &from, &to, &label, &at); err != nil {
			r.s.fail("message scan", err)
			return out
		}
		m.ID = domain.AgentMessageId(id)
		m.RunID = domain.RunId(run)
		m.FromAgentID = domain.RunAgentId(from)
		m.ToAgentID = domain.RunAgentId(to)
		m.Label = label
		m.At = anyTs(at)
		out = append(out, m)
	}
	return out
}

func (r *pgMessageRepo) Insert(m domain.AgentMessage) domain.AgentMessage {
	_, err := r.s.pool.Exec(bg(),
		`insert into agent_messages (id, run_id, from_agent_id, to_agent_id, label, at)
		 values ($1,$2,$3,$4,$5,$6)`,
		string(m.ID), string(m.RunID), string(m.FromAgentID), string(m.ToAgentID), m.Label, tsArg(m.At))
	r.s.fail("message.Insert", err)
	return m
}

/* ------------------------------ artifacts ---------------------------- */

type pgArtifactRepo struct{ s *pgStore }

func (r *pgArtifactRepo) ListByRun(runID domain.RunId) []domain.Artifact {
	rows, err := r.s.pool.Query(bg(),
		`select id, run_id, name, kind, size_bytes, produced_by_agent_id, produced_by_name,
		   storage_key, preview, created_at from artifacts where run_id=$1 order by created_at`,
		string(runID))
	if err != nil {
		r.s.fail("artifact.ListByRun", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			r.s.fail("artifact scan", err)
			return out
		}
		out = append(out, a)
	}
	return out
}

func scanArtifact(row pgx.Row) (domain.Artifact, error) {
	var a domain.Artifact
	var id, run, name, kind, producedByName string
	var sizeBytes *int
	var producedBy, storageKey, preview *string
	var created any
	if err := row.Scan(&id, &run, &name, &kind, &sizeBytes, &producedBy, &producedByName,
		&storageKey, &preview, &created); err != nil {
		return a, err
	}
	a.ID = domain.ArtifactId(id)
	a.RunID = domain.RunId(run)
	a.Name, a.Kind = name, domain.ArtifactKind(kind)
	a.SizeBytes = sizeBytes
	if producedBy != nil {
		p := domain.RunAgentId(*producedBy)
		a.ProducedByAgentID = &p
	}
	a.ProducedByName = producedByName
	a.StorageKey = storageKey
	a.Preview = preview
	a.CreatedAt = anyTs(created)
	return a, nil
}

func (r *pgArtifactRepo) Insert(a domain.Artifact) domain.Artifact {
	_, err := r.s.pool.Exec(bg(),
		`insert into artifacts (id, run_id, name, kind, size_bytes, produced_by_agent_id,
		   produced_by_name, storage_key, preview, created_at)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		string(a.ID), string(a.RunID), a.Name, string(a.Kind), a.SizeBytes,
		idPtrArg(a.ProducedByAgentID), a.ProducedByName, strArg(a.StorageKey), strArg(a.Preview),
		tsArg(a.CreatedAt))
	r.s.fail("artifact.Insert", err)
	return a
}

/* -------------------------------- logs ------------------------------- */

type pgLogRepo struct{ s *pgStore }

func (r *pgLogRepo) ListByRun(runID domain.RunId) []domain.LogEntry {
	rows, err := r.s.pool.Query(bg(),
		`select id, run_id, seq, ts, offset_ms, level, channel, source, message, detail, reasoning
		 from run_logs where run_id=$1 order by seq`, string(runID))
	if err != nil {
		r.s.fail("log.ListByRun", err)
		return nil
	}
	defer rows.Close()
	out := []domain.LogEntry{}
	for rows.Next() {
		var l domain.LogEntry
		var id, run, level, channel, source, message string
		var seq, offsetMs int
		var detail *string
		var reasoning bool
		var ts any
		if err := rows.Scan(&id, &run, &seq, &ts, &offsetMs, &level, &channel, &source, &message, &detail, &reasoning); err != nil {
			r.s.fail("log scan", err)
			return out
		}
		l.ID = domain.LogId(id)
		l.RunID = domain.RunId(run)
		l.Seq, l.OffsetMs = seq, offsetMs
		l.Ts = anyTs(ts)
		l.Level = domain.LogLevel(level)
		l.Channel = domain.LogChannel(channel)
		l.Source, l.Message, l.Detail, l.Reasoning = source, message, detail, reasoning
		out = append(out, l)
	}
	return out
}

func (r *pgLogRepo) Insert(entry domain.LogEntry) domain.LogEntry {
	_, err := r.s.pool.Exec(bg(),
		`insert into run_logs (id, run_id, seq, ts, offset_ms, level, channel, source, message, detail, reasoning)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		string(entry.ID), string(entry.RunID), entry.Seq, tsArg(entry.Ts), entry.OffsetMs,
		string(entry.Level), string(entry.Channel), entry.Source, entry.Message, strArg(entry.Detail), entry.Reasoning)
	r.s.fail("log.Insert", err)
	return entry
}

func (r *pgLogRepo) MaxSeq(runID domain.RunId) int {
	row := r.s.pool.QueryRow(bg(), `select coalesce(max(seq),-1) from run_logs where run_id=$1`, string(runID))
	var n int
	if err := row.Scan(&n); err != nil {
		r.s.fail("log.MaxSeq", err)
		return -1
	}
	return n
}

/* ------------------------------ browser ------------------------------ */

type pgBrowserRepo struct{ s *pgStore }

func scanSession(row pgx.Row) (domain.BrowserSession, error) {
	var b domain.BrowserSession
	var id, run, agentName, status, currentURL, pageTitle string
	var vw, vh, pages, actions int
	var started any
	var finished any
	if err := row.Scan(&id, &run, &agentName, &status, &currentURL, &pageTitle,
		&vw, &vh, &pages, &actions, &started, &finished); err != nil {
		return b, err
	}
	b.ID = domain.BrowserSessionId(id)
	b.RunID = domain.RunId(run)
	b.AgentName = agentName
	b.Status = domain.RunStatus(status)
	b.CurrentURL, b.PageTitle = currentURL, pageTitle
	b.Viewport = domain.Viewport{Width: vw, Height: vh}
	b.PagesVisited, b.ActionsCount = pages, actions
	b.StartedAt = anyTs(started)
	b.FinishedAt = anyTsPtr(finished)
	return b, nil
}

const sessionCols = `id, run_id, agent_name, status, current_url, page_title, viewport_w, viewport_h, pages_visited, actions_count, started_at, finished_at`

func (r *pgBrowserRepo) GetByID(id domain.BrowserSessionId) (domain.BrowserSession, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+sessionCols+` from browser_sessions where id=$1`, string(id))
	b, err := scanSession(row)
	if err != nil {
		return domain.BrowserSession{}, false
	}
	return b, true
}

func (r *pgBrowserRepo) GetByRun(runID domain.RunId) (domain.BrowserSession, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+sessionCols+` from browser_sessions where run_id=$1 limit 1`, string(runID))
	b, err := scanSession(row)
	if err != nil {
		return domain.BrowserSession{}, false
	}
	return b, true
}

func (r *pgBrowserRepo) InsertSession(b domain.BrowserSession) domain.BrowserSession {
	_, err := r.s.pool.Exec(bg(),
		`insert into browser_sessions (`+sessionCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		string(b.ID), string(b.RunID), b.AgentName, string(b.Status), b.CurrentURL, b.PageTitle,
		b.Viewport.Width, b.Viewport.Height, b.PagesVisited, b.ActionsCount,
		tsArg(b.StartedAt), tsPtrArg(b.FinishedAt))
	r.s.fail("browser.InsertSession", err)
	return b
}

func (r *pgBrowserRepo) Update(id domain.BrowserSessionId, patch BrowserSessionPatch) (domain.BrowserSession, error) {
	b := newSetBuilder()
	if patch.Status != nil {
		b.add("status", string(*patch.Status))
	}
	if patch.CurrentURL != nil {
		b.add("current_url", *patch.CurrentURL)
	}
	if patch.PageTitle != nil {
		b.add("page_title", *patch.PageTitle)
	}
	if patch.PagesVisited != nil {
		b.add("pages_visited", *patch.PagesVisited)
	}
	if patch.ActionsCount != nil {
		b.add("actions_count", *patch.ActionsCount)
	}
	if patch.FinishedAt != nil {
		b.add("finished_at", tsArg(*patch.FinishedAt))
	}
	if err := b.exec(r.s, "browser_sessions", string(id)); err != nil {
		return domain.BrowserSession{}, err
	}
	sess, ok := r.GetByID(id)
	if !ok {
		return domain.BrowserSession{}, domain.NotFound("browser session")
	}
	return sess, nil
}

func (r *pgBrowserRepo) ListActions(id domain.BrowserSessionId) []domain.BrowserAction {
	rows, err := r.s.pool.Query(bg(),
		`select id, session_id, ts, type, target, value, status, duration_ms
		 from browser_actions where session_id=$1 order by ts`, string(id))
	if err != nil {
		r.s.fail("browser.ListActions", err)
		return nil
	}
	defer rows.Close()
	out := []domain.BrowserAction{}
	for rows.Next() {
		var a domain.BrowserAction
		var aid, sid, typ, target, status string
		var value *string
		var dur int
		var ts any
		if err := rows.Scan(&aid, &sid, &ts, &typ, &target, &value, &status, &dur); err != nil {
			r.s.fail("browser action scan", err)
			return out
		}
		a.ID = domain.BrowserActionId(aid)
		a.SessionID = domain.BrowserSessionId(sid)
		a.Ts = anyTs(ts)
		a.Type = domain.BrowserActionType(typ)
		a.Target, a.Value, a.Status, a.DurationMs = target, value, status, dur
		out = append(out, a)
	}
	return out
}

func (r *pgBrowserRepo) ListShots(id domain.BrowserSessionId) []domain.BrowserShot {
	rows, err := r.s.pool.Query(bg(),
		`select id, session_id, ts, url, title, label, storage_key
		 from browser_shots where session_id=$1 order by ts`, string(id))
	if err != nil {
		r.s.fail("browser.ListShots", err)
		return nil
	}
	defer rows.Close()
	out := []domain.BrowserShot{}
	for rows.Next() {
		var sh domain.BrowserShot
		var shid, sid, url, title, label string
		var storageKey *string
		var ts any
		if err := rows.Scan(&shid, &sid, &ts, &url, &title, &label, &storageKey); err != nil {
			r.s.fail("browser shot scan", err)
			return out
		}
		sh.ID = domain.BrowserShotId(shid)
		sh.SessionID = domain.BrowserSessionId(sid)
		sh.Ts = anyTs(ts)
		sh.URL, sh.Title, sh.Label, sh.StorageKey = url, title, label, storageKey
		out = append(out, sh)
	}
	return out
}

func (r *pgBrowserRepo) ListConsole(id domain.BrowserSessionId) []domain.BrowserConsoleLine {
	rows, err := r.s.pool.Query(bg(),
		`select ts, level, text from browser_console where session_id=$1 order by ts`, string(id))
	if err != nil {
		r.s.fail("browser.ListConsole", err)
		return nil
	}
	defer rows.Close()
	out := []domain.BrowserConsoleLine{}
	for rows.Next() {
		var c domain.BrowserConsoleLine
		var level, text string
		var ts any
		if err := rows.Scan(&ts, &level, &text); err != nil {
			r.s.fail("browser console scan", err)
			return out
		}
		c.Ts = anyTs(ts)
		c.Level = domain.LogLevel(level)
		c.Text = text
		out = append(out, c)
	}
	return out
}

func (r *pgBrowserRepo) InsertAction(a domain.BrowserAction) domain.BrowserAction {
	_, err := r.s.pool.Exec(bg(),
		`insert into browser_actions (id, session_id, ts, type, target, value, status, duration_ms)
		 values ($1,$2,$3,$4,$5,$6,$7,$8)`,
		string(a.ID), string(a.SessionID), tsArg(a.Ts), string(a.Type), a.Target, strArg(a.Value), a.Status, a.DurationMs)
	r.s.fail("browser.InsertAction", err)
	return a
}

func (r *pgBrowserRepo) InsertShot(sh domain.BrowserShot) domain.BrowserShot {
	_, err := r.s.pool.Exec(bg(),
		`insert into browser_shots (id, session_id, ts, url, title, label, storage_key)
		 values ($1,$2,$3,$4,$5,$6,$7)`,
		string(sh.ID), string(sh.SessionID), tsArg(sh.Ts), sh.URL, sh.Title, sh.Label, strArg(sh.StorageKey))
	r.s.fail("browser.InsertShot", err)
	return sh
}

func (r *pgBrowserRepo) InsertConsole(id domain.BrowserSessionId, line domain.BrowserConsoleLine) {
	_, err := r.s.pool.Exec(bg(),
		`insert into browser_console (session_id, ts, level, text) values ($1,$2,$3,$4)`,
		string(id), tsArg(line.Ts), string(line.Level), line.Text)
	r.s.fail("browser.InsertConsole", err)
}

/* --------------------------- notifications --------------------------- */

type pgNotificationRepo struct{ s *pgStore }

func scanNotification(row pgx.Row) (domain.Notification, error) {
	var n domain.Notification
	var id, ws, typ, severity, title, body string
	var recipient, workflowSlug, runID *string
	var runNumber *int
	var readAt any
	var created any
	if err := row.Scan(&id, &ws, &recipient, &typ, &severity, &title, &body,
		&workflowSlug, &runID, &runNumber, &readAt, &created); err != nil {
		return n, err
	}
	n.ID = domain.NotificationId(id)
	n.WorkspaceID = domain.WorkspaceId(ws)
	if recipient != nil {
		rc := domain.MemberId(*recipient)
		n.RecipientID = &rc
	}
	n.Type = domain.NotificationType(typ)
	n.Severity = domain.Severity(severity)
	n.Title, n.Body = title, body
	n.WorkflowSlug = workflowSlug
	if runID != nil {
		ri := domain.RunId(*runID)
		n.RunID = &ri
	}
	n.RunNumber = runNumber
	n.ReadAt = anyTsPtr(readAt)
	n.CreatedAt = anyTs(created)
	return n, nil
}

const notifCols = `id, workspace_id, recipient_id, type, severity, title, body, workflow_slug, run_id, run_number, read_at, created_at`

func (r *pgNotificationRepo) All() []domain.Notification {
	rows, err := r.s.pool.Query(bg(), `select `+notifCols+` from notifications order by created_at desc`)
	if err != nil {
		r.s.fail("notification.All", err)
		return nil
	}
	defer rows.Close()
	out := []domain.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			r.s.fail("notification scan", err)
			return out
		}
		out = append(out, n)
	}
	return out
}

func (r *pgNotificationRepo) GetByID(id domain.NotificationId) (domain.Notification, bool) {
	row := r.s.pool.QueryRow(bg(), `select `+notifCols+` from notifications where id=$1`, string(id))
	n, err := scanNotification(row)
	if err != nil {
		return domain.Notification{}, false
	}
	return n, true
}

func (r *pgNotificationRepo) Insert(n domain.Notification) domain.Notification {
	_, err := r.s.pool.Exec(bg(),
		`insert into notifications (`+notifCols+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		string(n.ID), string(n.WorkspaceID), idPtrArg(n.RecipientID), string(n.Type), string(n.Severity),
		n.Title, n.Body, strArg(n.WorkflowSlug), idPtrArg(n.RunID), n.RunNumber,
		tsPtrArg(n.ReadAt), tsArg(n.CreatedAt))
	r.s.fail("notification.Insert", err)
	return n
}

func (r *pgNotificationRepo) Update(id domain.NotificationId, patch NotificationPatch) (domain.Notification, error) {
	b := newSetBuilder()
	if patch.ReadAt != nil {
		b.add("read_at", tsArg(*patch.ReadAt))
	}
	if err := b.exec(r.s, "notifications", string(id)); err != nil {
		return domain.Notification{}, err
	}
	n, ok := r.GetByID(id)
	if !ok {
		return domain.Notification{}, domain.NotFound("notification")
	}
	return n, nil
}

func (r *pgNotificationRepo) MarkAllRead(workspaceID domain.WorkspaceId, at domain.Timestamp) int {
	tag, err := r.s.pool.Exec(bg(),
		`update notifications set read_at=$1 where workspace_id=$2 and read_at is null`,
		tsArg(at), string(workspaceID))
	if err != nil {
		r.s.fail("notification.MarkAllRead", err)
		return 0
	}
	return int(tag.RowsAffected())
}

/* ------------------------------ activity ----------------------------- */

type pgActivityRepo struct{ s *pgStore }

func (r *pgActivityRepo) All() []domain.ActivityEvent {
	rows, err := r.s.pool.Query(bg(),
		`select id, workspace_id, kind, actor, text, workflow_slug, run_id, at
		 from activity_events order by at desc`)
	if err != nil {
		r.s.fail("activity.All", err)
		return nil
	}
	defer rows.Close()
	out := []domain.ActivityEvent{}
	for rows.Next() {
		var e domain.ActivityEvent
		var id, ws, kind, actor, text string
		var workflowSlug, runID *string
		var at any
		if err := rows.Scan(&id, &ws, &kind, &actor, &text, &workflowSlug, &runID, &at); err != nil {
			r.s.fail("activity scan", err)
			return out
		}
		e.ID = domain.ActivityId(id)
		e.WorkspaceID = domain.WorkspaceId(ws)
		e.Kind = domain.ActivityKind(kind)
		e.Actor, e.Text = actor, text
		e.WorkflowSlug = workflowSlug
		if runID != nil {
			ri := domain.RunId(*runID)
			e.RunID = &ri
		}
		e.At = anyTs(at)
		out = append(out, e)
	}
	return out
}

func (r *pgActivityRepo) Insert(e domain.ActivityEvent) domain.ActivityEvent {
	_, err := r.s.pool.Exec(bg(),
		`insert into activity_events (id, workspace_id, kind, actor, text, workflow_slug, run_id, at)
		 values ($1,$2,$3,$4,$5,$6,$7,$8)`,
		string(e.ID), string(e.WorkspaceID), string(e.Kind), e.Actor, e.Text,
		strArg(e.WorkflowSlug), idPtrArg(e.RunID), tsArg(e.At))
	r.s.fail("activity.Insert", err)
	return e
}

/* ------------------------ small shared helpers ----------------------- */

// setBuilder builds a dynamic `UPDATE … SET col=$n, …` from non-nil patch fields.
type setBuilder struct {
	cols []string
	args []any
}

func newSetBuilder() *setBuilder { return &setBuilder{} }

func (b *setBuilder) add(col string, val any) {
	b.cols = append(b.cols, fmt.Sprintf("%s=$%d", col, len(b.args)+1))
	b.args = append(b.args, val)
}

// exec runs the update with id as the final positional arg. A no-op patch is fine.
func (b *setBuilder) exec(s *pgStore, table, id string) error {
	if len(b.cols) == 0 {
		return nil
	}
	q := fmt.Sprintf("update %s set %s where id=$%d", table, strings.Join(b.cols, ", "), len(b.args)+1)
	_, err := s.pool.Exec(bg(), q, append(b.args, id)...)
	if err != nil {
		s.fail("update "+table, err)
	}
	return err
}

// idPtrArg renders a nullable typed-id pointer (any *T where T's kind is string).
func idPtrArg[T ~string](p *T) any {
	if p == nil {
		return nil
	}
	return string(*p)
}

// orEmptyMap ensures a nil map marshals as {} not null (the column is NOT NULL).
func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// noteArg maps an empty note to SQL NULL (the column is nullable text).
func noteArg(note string) any {
	if note == "" {
		return nil
	}
	return note
}
