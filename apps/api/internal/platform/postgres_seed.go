package platform

import "github.com/jackc/pgx/v5/pgxpool"

// SeedPostgresIfEmpty creates only the tenancy rows required by the control
// plane. Workflows, runs, logs, notifications, agents, and browser data must be
// created by real user/runtime actions.
func SeedPostgresIfEmpty(repos *Repositories, pool *pgxpool.Pool, data MemoryData, log Logger) error {
	if existing := repos.Workspace.Get(); existing.ID != "" {
		return nil
	}
	log.Info("bootstrapping empty Postgres workspace")

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

	log.Info("workspace bootstrap complete")
	return nil
}
