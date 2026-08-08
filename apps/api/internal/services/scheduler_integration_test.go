package services

import (
	"testing"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// TestScheduler_FiresOnceAndIsIdempotent drives the whole Phase-1 path on the
// in-memory platform: a scheduled workflow launches when 09:00 has passed, and a
// second tick (simulating a restart or the next 30s) does NOT double-fire. Prompt
// compilation is tested separately because it now requires a real OpenAI call.
func TestScheduler_FiresOnceAndIsIdempotent(t *testing.T) {
	// Freeze the write clock so launched runs get a deterministic QueuedAt that the
	// idempotency check (QueuedAt >= fireAt) can reason about.
	clock := platform.FixedClock("2026-06-20T09:30:00Z")
	plat := NewPlatform(Options{Clock: clock})

	wf, ok := plat.Repos.Workflows.GetBySlug("ai-digest")
	if !ok {
		t.Fatal("seed workflow missing")
	}
	schedule := "daily 09:00"
	trigger := domain.TriggerSchedule
	wf.Schedule = &schedule
	wf.Trigger = trigger
	wf.DefaultInput = map[string]any{"email": "satyanvm7@gmail.com"}
	wf.ID = domain.NewWorkflowId()
	wf.Slug = "scheduled-ai-digest"
	plat.Repos.Workflows.Insert(wf)

	sched := plat.NewScheduler("UTC")
	now := time.Date(2026, 6, 20, 9, 30, 0, 0, time.UTC) // past today's 09:00 slot

	sched.Tick(now)
	if n := len(plat.Repos.Runs.ListByWorkflow(wf.ID)); n != 1 {
		t.Fatalf("after first tick: %d runs, want 1", n)
	}

	// Second tick same day → idempotent (this is the restart-safety guarantee).
	sched.Tick(now)
	if n := len(plat.Repos.Runs.ListByWorkflow(wf.ID)); n != 1 {
		t.Fatalf("after second tick: %d runs, want 1 (no double-fire)", n)
	}

	// Before 09:00 it must NOT fire (a fresh workflow, evaluated at 08:00).
	wf2 := wf
	wf2.ID = domain.NewWorkflowId()
	wf2.Slug = "ai-digest-later"
	wf2.Name = "AI Digest Later"
	wf2.DefaultInput = map[string]any{"email": "a@b.com"}
	plat.Repos.Workflows.Insert(wf2)
	sched.Tick(time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC))
	if n := len(plat.Repos.Runs.ListByWorkflow(wf2.ID)); n != 0 {
		t.Fatalf("pre-09:00 tick fired %d runs, want 0", n)
	}
}
