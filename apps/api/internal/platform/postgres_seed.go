package platform

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedPostgresIfEmpty writes the canonical demo dataset into Postgres the first
// time the API boots against an empty database. It keeps the "#142 Competitive
// Intelligence Sweep" demo alive against the real DB and gives the worker real
// rows to claim. Idempotent: if a workspace already exists, it does nothing.
//
// Most entities are inserted through their repository Insert methods (reusing the
// SQL already written); workspaces and members have no Insert on their interface,
// so they go in via raw SQL here.
func SeedPostgresIfEmpty(repos *Repositories, pool *pgxpool.Pool, data MemoryData, log Logger) error {
	if existing := repos.Workspace.Get(); existing.ID != "" {
		return nil // already seeded
	}
	log.Info("seeding empty Postgres with canonical dataset")

	w := data.Workspace
	if _, err := pool.Exec(bg(),
		`insert into workspaces (id, name, slug, plan, default_region, created_at)
		 values ($1,$2,$3,$4,$5,$6)`,
		string(w.ID), w.Name, w.Slug, string(w.Plan), w.DefaultRegion, tsArg(w.CreatedAt)); err != nil {
		return err
	}

	for _, m := range data.Members {
		if _, err := pool.Exec(bg(),
			`insert into members (id, workspace_id, name, email, initials, role, created_at)
			 values ($1,$2,$3,$4,$5,$6,$7)`,
			string(m.ID), string(m.WorkspaceID), m.Name, m.Email, m.Initials, string(m.Role), tsArg(m.CreatedAt)); err != nil {
			return err
		}
	}

	// Everything else reuses the repository Insert methods. Order respects FKs:
	// agents → workflows → versions → runs → run_agents → (messages, artifacts,
	// logs) → browser sessions → browser children → notifications → activity.
	for _, a := range data.Agents {
		repos.Agents.Insert(a)
	}
	// FK cycle: workflows.current_version_id → workflow_versions.id, but
	// workflow_versions.workflow_id → workflows.id. Insert workflows with a null
	// current_version_id first, then versions, then re-link.
	type versionLink struct {
		workflowID string
		versionID  string
	}
	var verLinks []versionLink
	for _, wf := range data.Workflows {
		if wf.CurrentVersionID != nil {
			verLinks = append(verLinks, versionLink{string(wf.ID), string(*wf.CurrentVersionID)})
			wf.CurrentVersionID = nil
		}
		repos.Workflows.Insert(wf)
	}
	for _, v := range data.Versions {
		repos.Versions.Insert(v)
	}
	for _, ln := range verLinks {
		if _, err := pool.Exec(bg(),
			`update workflows set current_version_id=$1 where id=$2`, ln.versionID, ln.workflowID); err != nil {
			return err
		}
	}
	// FK cycle: runs.browser_session_id → browser_sessions.id, but
	// browser_sessions.run_id → runs.id. Insert runs with a null session id first,
	// remember which runs had one, then re-link after sessions are inserted.
	type sessionLink struct {
		runID     string
		sessionID string
	}
	var links []sessionLink
	for _, run := range data.Runs {
		if run.BrowserSessionID != nil {
			links = append(links, sessionLink{string(run.ID), string(*run.BrowserSessionID)})
			run.BrowserSessionID = nil
		}
		repos.Runs.Insert(run)
	}
	repos.RunAgents.InsertMany(data.RunAgents)
	for _, msg := range data.Messages {
		repos.Messages.Insert(msg)
	}
	for _, art := range data.Artifacts {
		repos.Artifacts.Insert(art)
	}
	for _, l := range data.Logs {
		repos.Logs.Insert(l)
	}
	for _, sess := range data.BrowserSessions {
		repos.Browser.InsertSession(sess)
	}
	// Now that sessions exist, re-link the runs that referenced one.
	for _, ln := range links {
		if _, err := pool.Exec(bg(),
			`update runs set browser_session_id=$1 where id=$2`, ln.sessionID, ln.runID); err != nil {
			return err
		}
	}
	for _, act := range data.BrowserActions {
		repos.Browser.InsertAction(act)
	}
	for _, shot := range data.BrowserShots {
		repos.Browser.InsertShot(shot)
	}
	for _, row := range data.BrowserConsole {
		repos.Browser.InsertConsole(row.SessionID, row.Line)
	}
	for _, n := range data.Notifications {
		repos.Notifications.Insert(n)
	}
	for _, e := range data.Activity {
		repos.Activity.Insert(e)
	}

	log.Info("seed complete",
		"workflows", len(data.Workflows), "runs", len(data.Runs), "logs", len(data.Logs))
	return nil
}
