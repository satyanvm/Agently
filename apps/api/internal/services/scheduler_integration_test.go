package services

import (
	"testing"
	"time"

	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

// TestScheduler_FiresOnceAndIsIdempotent drives the whole Phase-1 path on the
// in-memory platform: a prompt with "every day at 9 am" compiles to a schedule-
// triggered workflow, the scheduler launches it when 09:00 has passed, and a second
// tick (simulating a restart or the next 30s) does NOT double-fire.
func TestScheduler_FiresOnceAndIsIdempotent(t *testing.T) {
	// Freeze the write clock so launched runs get a deterministic QueuedAt that the
	// idempotency check (QueuedAt >= fireAt) can reason about.
	clock := platform.FixedClock("2026-06-20T09:30:00Z")
	plat := NewPlatform(Options{Clock: clock})

	// The target prompt — no hardcoded email, no hardcoded sources.
	wf, err := plat.Workflows.Create(validate.CreateWorkflowInput{
		Prompt: "every day at 9 am i want startup news emailed to satyanvm7@gmail.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wf.Workflow.Trigger != "schedule" {
		t.Fatalf("trigger = %q, want schedule", wf.Workflow.Trigger)
	}
	if wf.Workflow.Schedule == nil || *wf.Workflow.Schedule != "daily 09:00" {
		t.Fatalf("schedule = %v, want daily 09:00", wf.Workflow.Schedule)
	}
	if got := wf.Workflow.DefaultInput["email"]; got != "satyanvm7@gmail.com" {
		t.Fatalf("email = %v, want satyanvm7@gmail.com (parsed from prompt)", got)
	}

	sched := plat.NewScheduler("UTC")
	now := time.Date(2026, 6, 20, 9, 30, 0, 0, time.UTC) // past today's 09:00 slot

	sched.Tick(now)
	if n := len(plat.Repos.Runs.ListByWorkflow(wf.Workflow.ID)); n != 1 {
		t.Fatalf("after first tick: %d runs, want 1", n)
	}

	// Second tick same day → idempotent (this is the restart-safety guarantee).
	sched.Tick(now)
	if n := len(plat.Repos.Runs.ListByWorkflow(wf.Workflow.ID)); n != 1 {
		t.Fatalf("after second tick: %d runs, want 1 (no double-fire)", n)
	}

	// Before 09:00 it must NOT fire (a fresh workflow, evaluated at 08:00).
	wf2, _ := plat.Workflows.Create(validate.CreateWorkflowInput{
		Prompt: "daily at 9 am email me startup news at a@b.com",
	})
	sched.Tick(time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC))
	if n := len(plat.Repos.Runs.ListByWorkflow(wf2.Workflow.ID)); n != 0 {
		t.Fatalf("pre-09:00 tick fired %d runs, want 0", n)
	}
}
