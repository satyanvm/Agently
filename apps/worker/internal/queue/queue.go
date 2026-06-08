// Package queue is the worker's view of the Postgres-as-queue. It wraps the
// claim/heartbeat/finish/reap operations the durable execution loop is built on.
//
// The worker shares NO Go code with the API — only the database. That's the
// control-plane / data-plane split made concrete: the API manages runs, the
// worker executes them, and Postgres is the only thing between them.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRun is returned by Claim when the queue is empty.
var ErrNoRun = errors.New("no queued run available")

// ErrLeaseLost is returned by Heartbeat/Finish when the run is no longer owned by
// this worker — meaning a reaper requeued it and someone else (or no one) holds
// it now. The worker MUST stop working on the run when it sees this.
var ErrLeaseLost = errors.New("lease lost: run no longer owned by this worker")

// Claimed is the slice of a run the worker cares about. We deliberately don't
// import the API's domain model — the worker only needs a few columns to drive
// execution, and coupling the two modules would defeat the data-plane split.
type Claimed struct {
	ID           string
	WorkflowID   string
	WorkflowName string
	Number       int
	StepsTotal   int
	StepsDone    int            // > 0 on a resumed run — the agent skips already-done steps
	Input        map[string]any // the run's task parameters (sources, topic, email…)
}

// Queue wraps a connection pool with the worker's queue operations.
type Queue struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Queue { return &Queue{pool: pool} }

// Claim atomically grabs the oldest queued run and marks it running, stamping
// this worker's id as the owner and laying down the first heartbeat. Returns
// ErrNoRun if the queue is empty. Built on FOR UPDATE SKIP LOCKED, so many
// workers can call this concurrently without ever claiming the same run.
func (q *Queue) Claim(ctx context.Context, workerID string) (Claimed, error) {
	// claim_next_run returns the runs row; join to workflows for the display name.
	// We claim first (the SKIP LOCKED update), then read the name in the same query
	// via a CTE so it stays atomic.
	row := q.pool.QueryRow(ctx,
		`with c as (select * from claim_next_run($1))
		 select c.id, c.workflow_id, w.name, c.number, c.steps_total, c.steps_done, c.input
		   from c join workflows w on w.id = c.workflow_id`, workerID)
	var c Claimed
	var input []byte
	if err := row.Scan(&c.ID, &c.WorkflowID, &c.WorkflowName, &c.Number, &c.StepsTotal, &c.StepsDone, &input); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Claimed{}, ErrNoRun
		}
		return Claimed{}, fmt.Errorf("claim: %w", err)
	}
	c.Input = map[string]any{}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &c.Input)
	}
	return c, nil
}

// Heartbeat renews the lease for a run THIS worker owns. It is scoped to
// (id, claimed_by): if a reaper already requeued the run and handed it off, the
// WHERE clause matches no rows and we return ErrLeaseLost. This is the guard that
// prevents split-brain — a slow worker can't keep touching a run it lost.
//
// Note this is idempotent in the lease sense: it SETs heartbeat_at = now(); it
// does not accumulate. Calling it twice is the same as calling it once.
func (q *Queue) Heartbeat(ctx context.Context, runID, workerID string) error {
	tag, err := q.pool.Exec(ctx,
		`update runs set heartbeat_at = now()
		  where id = $1 and claimed_by = $2 and status = 'running'`, runID, workerID)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Finish marks an owned run terminal (succeeded/failed/canceled), again scoped to
// ownership so a worker can't finish a run it has lost. progressDone/currentStep
// let the skeleton show movement; real execution will set richer fields.
func (q *Queue) Finish(ctx context.Context, runID, workerID, status string, progressDone int, currentStep string) error {
	tag, err := q.pool.Exec(ctx,
		`update runs
		    set status = $3, finished_at = now(), steps_done = $4, current_step = $5
		  where id = $1 and claimed_by = $2 and status = 'running'`,
		runID, workerID, status, progressDone, currentStep)
	if err != nil {
		return fmt.Errorf("finish: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Progress updates the step counter / current step for an owned run, so the UI
// shows movement during a long run. Best-effort: a lost lease is reported so the
// caller can stop.
func (q *Queue) Progress(ctx context.Context, runID, workerID string, done int, step string) error {
	tag, err := q.pool.Exec(ctx,
		`update runs set steps_done = $3, current_step = $4
		  where id = $1 and claimed_by = $2 and status = 'running'`,
		runID, workerID, done, step)
	if err != nil {
		return fmt.Errorf("progress: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Reap requeues runs whose lease has lapsed and returns how many were requeued.
// Run periodically by a supervisor loop; max_silence is set by the SQL default.
func (q *Queue) Reap(ctx context.Context) (int, error) {
	row := q.pool.QueryRow(ctx, `select reap_stalled_runs()`)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("reap: %w", err)
	}
	return n, nil
}
