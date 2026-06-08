package runner

import (
	"context"
	"time"

	"github.com/agently/worker/internal/queue"
)

// Reaper periodically requeues runs whose lease has lapsed. In production this
// would run as its own small process (or a leader-elected singleton) so it's
// independent of any worker. For the MVP demo, any worker can run one — the
// requeue is idempotent and SKIP-LOCKED-safe, so overlap is harmless.
//
// This is the other half of crash recovery: a worker that dies stops
// heartbeating; the reaper notices the silence and puts the run back on the
// queue for a healthy worker to claim and resume.
type Reaper struct {
	q        *queue.Queue
	interval time.Duration
	log      Logger
}

func NewReaper(q *queue.Queue, interval time.Duration, log Logger) *Reaper {
	return &Reaper{q: q, interval: interval, log: log}
}

func (rp *Reaper) Run(ctx context.Context) {
	rp.log.Info("reaper started", "interval", rp.interval.String())
	t := time.NewTicker(rp.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			rp.log.Info("reaper stopping")
			return
		case <-t.C:
			n, err := rp.q.Reap(ctx)
			if err != nil {
				if ctx.Err() == nil {
					rp.log.Error("reap failed", "error", err.Error())
				}
				continue
			}
			if n > 0 {
				rp.log.Info("requeued stalled runs", "count", n)
			}
		}
	}
}
