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
}

// LoadRunMeta fetches the notification-relevant fields for a run.
func (q *Queue) LoadRunMeta(ctx context.Context, runID string) (RunMeta, error) {
	row := q.pool.QueryRow(ctx,
		`select r.workspace_id, w.slug, w.name, r.number
		   from runs r join workflows w on w.id = r.workflow_id where r.id=$1`, runID)
	var m RunMeta
	if err := row.Scan(&m.WorkspaceID, &m.WorkflowSlug, &m.WorkflowName, &m.Number); err != nil {
		return RunMeta{}, fmt.Errorf("load run meta: %w", err)
	}
	return m, nil
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
