package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

func TestFailureWatcherStartsErrorWorkflowAndAlerts(t *testing.T) {
	var slackText string
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		slackText = b["text"]
	}))
	defer slack.Close()
	t.Setenv("SMTP_HOST", "smtp.test")

	p := newTestPlatform()
	main, _ := p.Repos.Workflows.GetBySlug("ai-digest")
	handler := main
	handler.ID, handler.Slug, handler.Name = domain.NewWorkflowId(), "on-failure", "On failure"
	p.Repos.Workflows.Insert(handler)

	if _, err := p.Workflows.UpdateSettings("ai-digest", domain.WorkflowSettings{ErrorWorkflow: "ai-digest"}); err == nil {
		t.Fatal("self as error workflow must be rejected")
	}
	if _, err := p.Workflows.UpdateSettings("ai-digest", domain.WorkflowSettings{
		ErrorWorkflow: "on-failure", AlertEmail: "ops@example.com", AlertSlackWebhook: "https://hooks.example/x",
	}); err != nil {
		t.Fatal(err)
	}
	// The https check is for users; the test server is plain http.
	wf, _ := p.Repos.Workflows.GetBySlug("ai-digest")
	wf.Settings.AlertSlackWebhook = slack.URL
	p.Repos.Workflows.Update(wf.ID, platformPatchSettings(wf.Settings))

	run, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	msg := "boom"
	if _, err := p.Runs.Finish(run.ID, domain.RunFailed, &msg); err != nil {
		t.Fatal(err)
	}

	fw := p.NewFailureWatcher()
	var mailTo []string
	fw.sendMail = func(_ string, _ smtp.Auth, _ string, to []string, _ []byte) error {
		mailTo = to
		return nil
	}
	fw.Tick()
	fw.Tick() // a failure is handled once

	runs := p.Repos.Runs.ListByWorkflow(handler.ID)
	if len(runs) != 1 {
		t.Fatalf("error workflow runs = %d, want 1", len(runs))
	}
	failed, _ := runs[0].Input["failedRun"].(map[string]any)
	if failed["slug"] != "ai-digest" || failed["error"] != "boom" {
		t.Fatalf("failedRun input = %#v", runs[0].Input)
	}
	if !strings.Contains(slackText, "boom") || len(mailTo) != 1 || mailTo[0] != "ops@example.com" {
		t.Fatalf("alerts: slack=%q mail=%v", slackText, mailTo)
	}
}

func TestStepExecSeedsAncestorsFromLastRun(t *testing.T) {
	p := newTestPlatform()
	wf, _ := p.Repos.Workflows.GetBySlug("ai-digest")
	nodes, _ := p.Workflows.GraphNodes("ai-digest")
	if len(nodes) < 2 {
		t.Skip("seed graph too small")
	}
	var target domain.GraphNode
	for _, n := range nodes {
		if len(n.DependsOn) > 0 {
			target = n
		}
	}
	dep := target.DependsOn[0]

	prev, _ := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual})
	p.Repos.RunAgents.InsertMany([]domain.RunAgent{{
		ID: domain.NewRunAgentId(), RunID: prev.ID, NodeKey: dep, Name: dep,
		Status: domain.AgentSucceeded, Output: map[string]any{"text": "cached"},
	}})

	data, _ := p.Runs.NodeData("ai-digest")
	if data[dep].Output["text"] != "cached" {
		t.Fatalf("node data = %#v", data)
	}

	run, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{Trigger: domain.TriggerManual, TargetNode: target.Key, UsePins: true})
	if err != nil {
		t.Fatal(err)
	}
	exec, _ := run.Input[ExecKey].(map[string]any)
	seed, _ := exec["seed"].(map[string]any)
	if exec["targetNode"] != target.Key || exec["usePins"] != true || seed[dep] == nil {
		t.Fatalf("exec = %#v", exec)
	}
	if _, err := p.Runs.Launch("ai-digest", validate.LaunchRunInput{TargetNode: "nope"}); err == nil {
		t.Fatal("unknown target node must fail")
	}
	// A caller can't smuggle __exec in through the input.
	plain, _ := p.Runs.Launch(wf.Slug, validate.LaunchRunInput{Input: map[string]any{ExecKey: "x"}})
	if _, has := plain.Input[ExecKey]; has {
		t.Fatal("__exec from input must be dropped")
	}
}

func platformPatchSettings(s domain.WorkflowSettings) platform.WorkflowPatch {
	return platform.WorkflowPatch{Settings: &s}
}
