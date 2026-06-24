package services

// scheduler.go is the control-plane timer that turns a captured schedule ("daily
// 09:00", "hourly") into actual runs. architecture.md §18 flagged this as the one
// missing piece: the prompt's "every day at 9 am" was parsed and stored, but nothing
// fired it. This closes that gap.
//
// Design:
//   - A goroutine ticks every `interval` (default 30s).
//   - Each tick lists workflows with trigger='schedule' + a parseable schedule.
//   - It computes the most recent fire time that has already passed (today 09:00, or
//     the top of the current hour) in a configurable timezone.
//   - It launches IDEMPOTENTLY: a workflow is skipped if a trigger='schedule' run was
//     already queued at/after that fire time. This is derived from the durable `runs`
//     table — so a restart mid-day does NOT double-fire, with no new migration.
//   - Launch is the SAME path "Run now" uses (RunService.Launch), so default_input
//     (email/sources) is merged and the DAG materialized exactly as for a manual run.
//
// Single-instance assumption: idempotency is correct for one API process. Multiple API
// instances would need a DB lock / leader election (noted, not built — same caveat the
// worker's reaper carries).

import (
	"context"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

// Scheduler launches schedule-triggered workflows when they come due.
type Scheduler struct {
	deps     Deps
	runs     *RunService
	loc      *time.Location
	interval time.Duration
}

// NewScheduler assembles a scheduler from an already-built Platform. tz is an IANA
// timezone name; empty/unknown falls back to the server's local zone.
func (p *Platform) NewScheduler(tz string) *Scheduler {
	deps := Deps{Repos: p.Repos, Bus: p.Bus, Clock: p.Clock, Logger: p.Logger}
	return NewScheduler(deps, p.Runs, tz)
}

// NewScheduler builds a scheduler. tz is an IANA name (e.g. "America/Los_Angeles");
// empty or unknown falls back to the server's local zone. Times in schedule strings
// ("daily 09:00") are interpreted in this location.
func NewScheduler(deps Deps, runs *RunService, tz string) *Scheduler {
	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		} else {
			deps.Logger.Warn("scheduler: unknown SCHEDULER_TZ, using server local", "tz", tz, "error", err.Error())
		}
	}
	return &Scheduler{deps: deps, runs: runs, loc: loc, interval: 30 * time.Second}
}

// Start runs the tick loop until ctx is canceled. Non-blocking caller pattern: `go
// sch.Start(ctx)`.
func (s *Scheduler) Start(ctx context.Context) {
	s.deps.Logger.Info("scheduler started", "interval", s.interval.String(), "tz", s.loc.String())
	t := time.NewTicker(s.interval)
	defer t.Stop()
	s.Tick(time.Now().In(s.loc)) // fire once at boot so a due workflow doesn't wait a full interval
	for {
		select {
		case <-ctx.Done():
			s.deps.Logger.Info("scheduler stopped")
			return
		case <-t.C:
			s.Tick(time.Now().In(s.loc))
		}
	}
}

// Tick evaluates every scheduled workflow once against `now` (already in s.loc).
// Exported so it can be unit-tested deterministically with a fixed time.
func (s *Scheduler) Tick(now time.Time) {
	for _, wf := range s.deps.Repos.Workflows.All() {
		if wf.ArchivedAt != nil || wf.Trigger != domain.TriggerSchedule || wf.Schedule == nil {
			continue
		}
		fireAt, ok := dueAt(*wf.Schedule, now, s.loc)
		if !ok {
			continue // unparseable schedule, or not due yet this period
		}
		if s.alreadyFired(wf.ID, fireAt) {
			continue
		}
		s.launch(wf, fireAt)
	}
}

func (s *Scheduler) launch(wf domain.Workflow, fireAt time.Time) {
	_, err := s.runs.Launch(wf.Slug, validate.LaunchRunInput{
		Trigger: domain.TriggerSchedule,
		Input:   map[string]any{}, // default_input (email/sources) is merged by Launch
	})
	if err != nil {
		s.deps.Logger.Error("scheduler: launch failed", "workflow", wf.Slug, "error", err.Error())
		return
	}
	s.deps.Logger.Info("scheduler: launched scheduled run", "workflow", wf.Slug, "fireAt", fireAt.Format(time.RFC3339))
}

// alreadyFired is the idempotency guard: true if a schedule-triggered run for this
// workflow was already queued at/after the fire time. Derived from the durable runs
// table, so it survives an API restart.
func (s *Scheduler) alreadyFired(workflowID domain.WorkflowId, fireAt time.Time) bool {
	for _, r := range s.deps.Repos.Runs.ListByWorkflow(workflowID) {
		if r.Trigger != domain.TriggerSchedule {
			continue
		}
		if t, err := time.Parse(time.RFC3339, string(r.QueuedAt)); err == nil && !t.Before(fireAt) {
			return true
		}
	}
	return false
}

// dueAt parses a schedule string and returns the most recent fire instant that has
// already passed at `now`, plus ok=true if the workflow is due in the current period.
//
//	"daily HH:MM" → today's HH:MM in loc, if now is at/after it; else not due yet.
//	"hourly"      → the top of the current hour (always "due"; idempotency dedupes).
//
// ok=false for an unparseable schedule or a daily time still in the future today.
func dueAt(schedule string, now time.Time, loc *time.Location) (time.Time, bool) {
	sched := strings.ToLower(strings.TrimSpace(schedule))
	if sched == "hourly" {
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc), true
	}
	if rest, found := strings.CutPrefix(sched, "daily"); found {
		hh, mm, ok := parseHHMM(strings.TrimSpace(rest))
		if !ok {
			return time.Time{}, false
		}
		fire := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
		if now.Before(fire) {
			return time.Time{}, false // today's slot hasn't arrived yet
		}
		return fire, true
	}
	return time.Time{}, false
}

// parseHHMM reads "HH:MM" (24-hour). Returns ok=false on any malformed input.
func parseHHMM(s string) (hh, mm int, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	hh = atoiSafe(parts[0])
	mm = atoiSafe(parts[1])
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}
