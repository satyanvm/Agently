package queue

import (
	"context"
	"fmt"
)

// notify.go is the worker's write path for in-app notifications. When a run
// reaches a terminal state the worker inserts a notifications row — the same
// table the API serves to the UI's notification center — so "your run finished"
// shows up in the bell. The external webhook/email delivery is separate (the
// notifier package); this is the durable in-app record.

// RunMeta is the bit of run context a notification needs.
type RunMeta struct {
	WorkspaceID  string
	WorkflowSlug string
	WorkflowName string
	Number       int
	Email        string // recipient from run.input.email (optional)
}

// LoadRunMeta fetches the notification-relevant fields for a run, including the
// recipient email from the run's input (so "email me the digest" is honored).
func (q *Queue) LoadRunMeta(ctx context.Context, runID string) (RunMeta, error) {
	row := q.pool.QueryRow(ctx,
		`select r.workspace_id, w.slug, w.name, r.number, coalesce(r.input->>'email','')
		   from runs r join workflows w on w.id = r.workflow_id where r.id=$1`, runID)
	var m RunMeta
	if err := row.Scan(&m.WorkspaceID, &m.WorkflowSlug, &m.WorkflowName, &m.Number, &m.Email); err != nil {
		return RunMeta{}, fmt.Errorf("load run meta: %w", err)
	}
	return m, nil
}

// GoogleIntegration is a workspace's connected Gmail account: a refresh token the
// worker uses to send mail AS that account (option 2), plus the account address.
type GoogleIntegration struct {
	RefreshToken string
	AccountEmail string
}

// LoadGoogleIntegration returns the workspace's connected Google account, if any.
// ok=false when the workspace has not connected Gmail (the caller then falls back
// to a transactional provider / SMTP). Never returns an error for "not connected".
func (q *Queue) LoadGoogleIntegration(ctx context.Context, workspaceID string) (GoogleIntegration, bool) {
	row := q.pool.QueryRow(ctx,
		`select coalesce(refresh_token,''), coalesce(account_email,'')
		   from integrations
		  where workspace_id=$1 and provider='google' and refresh_token <> ''
		  limit 1`, workspaceID)
	var g GoogleIntegration
	if err := row.Scan(&g.RefreshToken, &g.AccountEmail); err != nil {
		return GoogleIntegration{}, false
	}
	return g, g.RefreshToken != ""
}

// LoadDigest returns the content of the run's primary result artifact (the digest
// the Editor produced), for inclusion in the notification body. Empty if none.
func (q *Queue) LoadDigest(ctx context.Context, runID string) string {
	row := q.pool.QueryRow(ctx,
		`select coalesce(preview,'') from artifacts where run_id=$1 order by created_at desc limit 1`, runID)
	var preview string
	_ = row.Scan(&preview)
	return preview
}

// CreateNotification inserts an in-app notification for a run outcome. severity is
// success|warning|error|info; ntype is e.g. workflow.completed / workflow.failed.
func (q *Queue) CreateNotification(ctx context.Context, m RunMeta, runID, ntype, severity, title, body string) error {
	id := genID("ntf")
	// recipient_id is left null (workspace-wide); the API's read path tolerates it.
	_, err := q.pool.Exec(ctx,
		`insert into notifications (id, workspace_id, recipient_id, type, severity, title, body, workflow_slug, run_id, run_number, created_at)
		 values ($1,$2,NULL,$3,$4,$5,$6,$7,$8,$9, now())`,
		id, m.WorkspaceID, ntype, severity, title, body, m.WorkflowSlug, runID, m.Number)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}
