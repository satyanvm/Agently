// Package runner is the worker's execution loop: claim a run, hold the lease via
// a heartbeat goroutine, do the work, finish. In this chunk the "work" is
// simulated (a few sleeps with progress updates) — the native agent runtime
// (real LLM calls) lands in the next chunk and slots in where doWork() is.
package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agently/worker/internal/agent"
	"github.com/agently/worker/internal/browser"
	"github.com/agently/worker/internal/llm"
	"github.com/agently/worker/internal/notifier"
	"github.com/agently/worker/internal/queue"
)

// Config tunes the loop's timing. Defaults favor a fast, visible demo over
// production conservatism — short lease so a killed worker is reclaimed quickly.
type Config struct {
	WorkerID          string
	PollInterval      time.Duration // how often to look for work when idle
	HeartbeatInterval time.Duration // how often to renew the lease (MUST be < lease)
	AppBaseURL        string        // base url for run deep-links in notifications
	// The lease length itself lives in SQL (reap_stalled_runs' max_silence). Keep
	// HeartbeatInterval comfortably below it — see architecture.md on the margin.
}

// Logger is the tiny logging surface the runner needs (stdlib slog satisfies it).
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

type Runner struct {
	q        *queue.Queue
	cfg      Config
	log      Logger
	llm      llm.Provider
	browser  browser.Provider
	notifier *notifier.Notifier
}

func New(q *queue.Queue, cfg Config, log Logger, provider llm.Provider, browserProvider browser.Provider, n *notifier.Notifier) *Runner {
	return &Runner{q: q, cfg: cfg, log: log, llm: provider, browser: browserProvider, notifier: n}
}

// Run is the worker's main loop. It blocks until ctx is canceled (SIGINT/SIGTERM).
// One run is executed at a time in this skeleton — concurrency comes later.
func (r *Runner) Run(ctx context.Context) {
	r.log.Info("worker started", "workerId", r.cfg.WorkerID,
		"poll", r.cfg.PollInterval.String(), "heartbeat", r.cfg.HeartbeatInterval.String())

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("worker stopping", "workerId", r.cfg.WorkerID)
			return
		case <-ticker.C:
			r.claimAndExecute(ctx)
		}
	}
}

// claimAndExecute tries to claim one run and, if it gets one, executes it under a
// held lease. Errors are logged, not fatal — the loop keeps polling.
func (r *Runner) claimAndExecute(ctx context.Context) {
	claimed, err := r.q.Claim(ctx, r.cfg.WorkerID)
	if errors.Is(err, queue.ErrNoRun) {
		return // queue empty; normal
	}
	if err != nil {
		r.log.Error("claim failed", "error", err.Error())
		return
	}
	r.log.Info("claimed run", "runId", claimed.ID, "number", claimed.Number, "workerId", r.cfg.WorkerID)
	r.execute(ctx, claimed)
}

// execute holds the lease for the duration of the work. The structure is the
// important part to understand:
//
//   - A child context (runCtx) is canceled the moment the lease is lost, so the
//     work stops promptly instead of finishing a run we no longer own.
//   - A heartbeat goroutine renews the lease on an interval. If a renewal returns
//     ErrLeaseLost (a reaper requeued us because WE were too slow / paused), it
//     cancels runCtx — that's the worker honestly giving up the run.
//   - doWork respects runCtx, so cancellation actually interrupts it.
func (r *Runner) execute(ctx context.Context, c queue.Claimed) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Heartbeat goroutine: the "I'm still alive" signal that lets a short lease
	// coexist with arbitrarily long work.
	go r.heartbeatLoop(runCtx, cancel, c.ID)

	rt := agent.New(r.q, r.llm, r.browser, r.cfg.WorkerID, r.log)

	// Choose the executor by the run's shape:
	//   - a materialized agent graph (run_agents rows) → the multi-agent DAG engine
	//   - none → the single-agent linear plan (older/native runs)
	// Both are durable + resumable; the DAG engine reads agent status from the DB.
	graph, err := r.q.LoadAgents(runCtx, c.ID)
	if err != nil {
		r.log.Error("could not load agent graph", "runId", c.ID, "error", err.Error())
	}

	var ok bool
	var stepsReached int
	if len(graph) > 0 {
		ok = rt.RunDAG(runCtx, c.ID, graph)
		stepsReached = c.StepsTotal // progress is tracked per-agent; mark the bar full on success
	} else {
		plan := agent.BuildPlan(c.WorkflowName, c.Number)
		startStep := c.StepsDone // 0 on a fresh run; >0 when resuming after a crash
		if startStep > len(plan) {
			startStep = len(plan)
		}
		stepsReached, ok = rt.Run(runCtx, c.ID, plan, startStep)
	}

	// Decide terminal status. If the lease was lost mid-run, runCtx is canceled;
	// we must NOT mark it finished (another worker owns it now).
	if runCtx.Err() != nil {
		r.log.Info("abandoning run (lease lost or shutdown)", "runId", c.ID, "workerId", r.cfg.WorkerID)
		return
	}

	status := "succeeded"
	step := "Completed"
	if !ok {
		status, step = "failed", "Failed"
	}
	if err := r.q.Finish(ctx, c.ID, r.cfg.WorkerID, status, stepsReached, step); err != nil {
		if errors.Is(err, queue.ErrLeaseLost) {
			r.log.Info("could not finish: lease lost", "runId", c.ID)
			return
		}
		r.log.Error("finish failed", "runId", c.ID, "error", err.Error())
		return
	}
	r.log.Info("run finished", "runId", c.ID, "status", status, "workerId", r.cfg.WorkerID)

	// Notify: the "ping me when it's done" promise. Best-effort and AFTER the run
	// is durably terminal — a notification failure must never fail a finished run.
	r.notify(ctx, c, status)
}

// notify writes an in-app notification row and pushes to external channels
// (webhook/email) for a terminal run. Failures are logged, never fatal.
func (r *Runner) notify(ctx context.Context, c queue.Claimed, status string) {
	meta, err := r.q.LoadRunMeta(ctx, c.ID)
	if err != nil {
		r.log.Error("notify: load run meta", "runId", c.ID, "error", err.Error())
		return
	}
	ntype, severity, title := "workflow.completed", "success", "Workflow completed"
	if status != "succeeded" {
		ntype, severity, title = "workflow.failed", "error", "Workflow failed"
	}
	body := fmt.Sprintf("%s run #%d %s.", meta.WorkflowName, meta.Number, status)
	if err := r.q.CreateNotification(ctx, meta, c.ID, ntype, severity, title, body); err != nil {
		r.log.Error("notify: create in-app notification", "runId", c.ID, "error", err.Error())
	}
	if r.notifier != nil {
		r.notifier.Notify(ctx, notifier.Event{
			RunID: c.ID, WorkflowName: meta.WorkflowName, WorkflowSlug: meta.WorkflowSlug,
			Number: meta.Number, Status: status,
			URL: fmt.Sprintf("%s/runs/%s", r.cfg.AppBaseURL, c.ID),
		})
	}
}

// heartbeatLoop renews the lease until the context ends. On ErrLeaseLost it
// cancels the run (we've been declared dead by a reaper); on a transient error it
// logs and keeps trying — a single missed beat is survivable because the lease
// covers several intervals (that margin is the whole point of heartbeat < lease).
func (r *Runner) heartbeatLoop(ctx context.Context, cancel context.CancelFunc, runID string) {
	t := time.NewTicker(r.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			err := r.q.Heartbeat(ctx, runID, r.cfg.WorkerID)
			if errors.Is(err, queue.ErrLeaseLost) {
				r.log.Error("lease lost — stopping work on run", "runId", runID, "workerId", r.cfg.WorkerID)
				cancel()
				return
			}
			if err != nil && ctx.Err() == nil {
				r.log.Error("heartbeat error (will retry)", "runId", runID, "error", err.Error())
			}
		}
	}
}

