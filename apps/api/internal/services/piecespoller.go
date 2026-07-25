package services

// piecespoller.go — the polling half of piece triggers, mirroring
// scheduler.go's design: a control-plane ticker (default every 5m,
// PIECES_POLL_INTERVAL) that, for every workflow whose entry node is a
// `pieces.*` POLLING trigger, invokes the trigger's run() on the pieces
// worker. New events (the trigger's own store dedupes via its polling
// cursor, migration 0012) each launch a run with the event as
// run.input.__trigger_event.
//
// Same single-instance assumption as the scheduler. PIECES_POLLER=0 disables.

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/agently/api/internal/domain"
)

const maxRunsPerPollTick = 3

type PiecesPoller struct {
	deps       Deps
	runs       *RunService
	workflows  *WorkflowService
	client     *PieceTriggerClient
	interval   time.Duration
	strategies map[string]string // node type id → "webhook" | "polling" | "app_webhook"
}

func (p *Platform) NewPiecesPoller() *PiecesPoller {
	interval := 5 * time.Minute
	if v := os.Getenv("PIECES_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Second {
			interval = d
		}
	}
	return &PiecesPoller{
		deps:       Deps{Repos: p.Repos, Bus: p.Bus, Clock: p.Clock, Logger: p.Logger},
		runs:       p.Runs,
		workflows:  p.Workflows,
		client:     NewPieceTriggerClient(),
		interval:   interval,
		strategies: loadTriggerStrategies(),
	}
}

// loadTriggerStrategies reads the pieces index once and maps trigger node type
// ids to their strategy. Missing index → empty map → the poller no-ops.
func loadTriggerStrategies() map[string]string {
	out := map[string]string{}
	path := piecesIndexPath()
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var idx pieceIndexFile
	if err := json.Unmarshal(raw, &idx); err != nil {
		return out
	}
	for _, n := range idx.Nodes {
		if n.Kind == "trigger" && n.Strategy != "" {
			out[n.ID] = n.Strategy
		}
	}
	return out
}

func (pp *PiecesPoller) Start(ctx context.Context) {
	pp.deps.Logger.Info("pieces poller started",
		"interval", pp.interval.String(), "pollingTriggers", len(pp.strategies))
	t := time.NewTicker(pp.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			pp.deps.Logger.Info("pieces poller stopped")
			return
		case <-t.C:
			pp.Tick()
		}
	}
}

// Tick polls every polling-trigger workflow once. Errors are logged and
// skipped — one broken trigger must not stall the loop.
func (pp *PiecesPoller) Tick() {
	for _, wf := range pp.deps.Repos.Workflows.All() {
		if wf.ArchivedAt != nil {
			continue
		}
		nodes, err := pp.workflows.GraphNodes(wf.Slug)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if len(n.DependsOn) > 0 || pp.strategies[n.Type] != "polling" {
				continue
			}
			req, ok := BuildTriggerRequest(string(wf.ID), n)
			if !ok {
				continue
			}
			res, err := pp.client.RunTrigger(req)
			if err != nil {
				pp.deps.Logger.Warn("pieces poller: worker unreachable", "workflow", wf.Slug, "error", err.Error())
				return // worker down — no point polling the rest this tick
			}
			if !res.OK {
				pp.deps.Logger.Warn("pieces poller: trigger poll failed",
					"workflow", wf.Slug, "node", n.Key, "errorType", res.ErrorType, "error", res.Error)
				continue
			}
			pp.launchEvents(wf.Slug, n.Key, res.Events)
		}
	}
}

func (pp *PiecesPoller) launchEvents(slug, nodeKey string, events []any) {
	for i, ev := range events {
		if i >= maxRunsPerPollTick {
			pp.deps.Logger.Warn("pieces poller: event burst capped",
				"workflow", slug, "node", nodeKey, "dropped", len(events)-maxRunsPerPollTick)
			return
		}
		if _, err := pp.runs.Launch(slug, LaunchInputForTriggerEvent(ev, domain.TriggerSchedule)); err != nil {
			pp.deps.Logger.Error("pieces poller: launch failed", "workflow", slug, "error", err.Error())
			return
		}
		pp.deps.Logger.Info("pieces poller: launched run from trigger event", "workflow", slug, "node", nodeKey)
	}
}
