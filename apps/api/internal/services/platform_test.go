package services

import (
	"testing"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

func newTestPlatform() *Platform {
	return NewPlatform(Options{})
}

func TestSeedRunDetail(t *testing.T) {
	p := newTestPlatform()
	detail, err := p.Runs.Get("run_8842")
	if err != nil {
		t.Fatalf("get run_8842: %v", err)
	}
	if detail.Number != 142 || detail.Status != domain.RunRunning {
		t.Fatalf("unexpected run: number=%d status=%s", detail.Number, detail.Status)
	}
	if len(detail.Agents) != 7 || len(detail.Messages) != 8 || len(detail.Artifacts) != 6 {
		t.Fatalf("unexpected children: agents=%d messages=%d artifacts=%d", len(detail.Agents), len(detail.Messages), len(detail.Artifacts))
	}
}

func TestLaunchThenCancel(t *testing.T) {
	p := newTestPlatform()
	detail, err := p.Runs.Launch("competitive-intelligence-sweep", validate.LaunchRunInput{Trigger: domain.TriggerManual})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	// Launch enqueues; a worker claims it later. (Pre-worker this was RunRunning.)
	if detail.Status != domain.RunQueued {
		t.Fatalf("expected queued, got %s", detail.Status)
	}
	if detail.Number != 143 {
		t.Fatalf("expected run number 143, got %d", detail.Number)
	}
	if len(detail.Agents) != 7 {
		t.Fatalf("expected 7 materialized agents, got %d", len(detail.Agents))
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
	_, _ = p.Runs.Launch("inbox-triage-autopilot", validate.LaunchRunInput{Trigger: domain.TriggerManual})
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
	if queued < 1 || started < 1 || logged < 2 {
		t.Fatalf("expected launch events buffered: queued=%d started=%d logged=%d", queued, started, logged)
	}
}
