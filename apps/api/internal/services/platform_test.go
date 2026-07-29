package services

import (
	"testing"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

func newTestPlatform() *Platform {
	return NewPlatform(Options{})
}

func TestSeedWorkflow(t *testing.T) {
	p := newTestPlatform()
	// The bootstrap seeds the AI Digest workflow and NO runs (system starts empty).
	wf, ok := p.Repos.Workflows.GetBySlug("ai-digest")
	if !ok {
		t.Fatal("expected ai-digest workflow to be seeded")
	}
	if wf.AgentCount != 5 {
		t.Fatalf("expected 5 agents in AI Digest, got %d", wf.AgentCount)
	}
	if runs := p.Repos.Runs.All(); len(runs) != 0 {
		t.Fatalf("expected no seeded runs, got %d", len(runs))
	}
}

func TestLaunchThenCancel(t *testing.T) {
	p := newTestPlatform()
	detail, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	// Launch enqueues; the Temporal reasoner claims it later.
	if detail.Status != domain.RunQueued {
		t.Fatalf("expected queued, got %s", detail.Status)
	}
	if detail.Number != 1 {
		t.Fatalf("expected first run number 1, got %d", detail.Number)
	}
	// The reasoner materializes run agents as it executes — Launch must not.
	if len(detail.Agents) != 0 {
		t.Fatalf("expected no pre-materialized agents, got %d", len(detail.Agents))
	}
	canceled, err := p.Runs.Cancel(detail.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if canceled.Status != domain.RunCanceled {
		t.Fatalf("expected canceled, got %s", canceled.Status)
	}
	// Cancelling a terminal run conflicts.
	if _, err := p.Runs.Cancel(detail.ID); err == nil {
		t.Fatal("expected conflict cancelling an already-canceled run")
	}
}

func TestLaunchUnknownWorkflow(t *testing.T) {
	p := newTestPlatform()
	if _, err := p.Runs.Launch("does-not-exist", validate.LaunchRunInput{Trigger: domain.TriggerManual}); err == nil {
		t.Fatal("expected not-found for unknown workflow")
	}
}

func TestEmittedEventsBuffered(t *testing.T) {
	p := newTestPlatform()
	_, _ = p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual})
	events := p.Bus.ReplayAfter("")
	var queued, started, logged int
	for _, e := range events {
		switch e.EventType() {
		case "run.queued":
			queued++
		case "run.started":
			started++
		case "run.log.appended":
			logged++
		}
	}
	// Launch only enqueues: run.started is the reasoner's to emit via Start.
	if queued < 1 || started != 0 || logged < 2 {
		t.Fatalf("expected launch events buffered: queued=%d started=%d logged=%d", queued, started, logged)
	}
}
