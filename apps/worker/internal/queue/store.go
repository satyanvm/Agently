package queue

import (
	"context"
	"crypto/rand"
	"fmt"
)

// store.go holds the worker's run-scoped WRITE operations: appending logs,
// recording artifacts, and rolling up token/cost usage. These are how the agent
// runtime makes its work VISIBLE and DURABLE — every reasoning step and result is
// persisted to Postgres as it happens, so a user who returns later (or watches
// live) sees exactly what the agent did. This is the observability half of the
// product, written from the data plane.

// AppendLog writes one append-only log line for a run. seq must be monotonic per
// run (the schema enforces unique (run_id, seq)); callers get it from NextLogSeq
// or by tracking it locally across a run. channel/level/reasoning mirror the
// domain enums the API and UI already understand.
func (q *Queue) AppendLog(ctx context.Context, runID string, seq, offsetMs int, level, channel, source, message string, reasoning bool, detail *string) error {
	id := genID("log")
	_, err := q.pool.Exec(ctx,
		`insert into run_logs (id, run_id, seq, offset_ms, level, channel, source, message, detail, reasoning)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		id, runID, seq, offsetMs, level, channel, source, message, detail, reasoning)
	if err != nil {
		return fmt.Errorf("append log: %w", err)
	}
	return nil
}

// NextLogSeq returns the next seq for a run (max+1, starting at 0). Used on resume
// so a re-claimed run continues its log numbering instead of colliding.
func (q *Queue) NextLogSeq(ctx context.Context, runID string) (int, error) {
	row := q.pool.QueryRow(ctx, `select coalesce(max(seq),-1)+1 from run_logs where run_id=$1`, runID)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("next log seq: %w", err)
	}
	return n, nil
}

// AddArtifact records a produced result (report/file/json/etc.). preview holds a
// short inline snippet the UI can show without fetching blob storage.
func (q *Queue) AddArtifact(ctx context.Context, runID, name, kind, producedByName, preview string) error {
	id := genID("art")
	_, err := q.pool.Exec(ctx,
		`insert into artifacts (id, run_id, name, kind, produced_by_name, preview, created_at)
		 values ($1,$2,$3,$4,$5,$6, now())`,
		id, runID, name, kind, producedByName, preview)
	if err != nil {
		return fmt.Errorf("add artifact: %w", err)
	}
	return nil
}

// AddUsage atomically accumulates token + cost usage onto a run, scoped to the
// owner so a lost-lease worker can't keep mutating it.
func (q *Queue) AddUsage(ctx context.Context, runID, workerID string, tokensIn, tokensOut int, costUSD float64) error {
	_, err := q.pool.Exec(ctx,
		`update runs
		    set tokens_in = tokens_in + $3,
		        tokens_out = tokens_out + $4,
		        cost_usd = cost_usd + $5
		  where id=$1 and claimed_by=$2`,
		runID, workerID, tokensIn, tokensOut, costUSD)
	if err != nil {
		return fmt.Errorf("add usage: %w", err)
	}
	return nil
}

// genID mints a prefixed id matching the app's convention (prefix_<base36×20>).
// The worker doesn't import the API's domain package, so it has its own minter.
func genID(prefix string) string {
	const base36 = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	out := make([]byte, 20)
	for i, c := range b {
		out[i] = base36[int(c)%36]
	}
	return prefix + "_" + string(out)
}
