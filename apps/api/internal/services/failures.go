package services

// failures.go handles failed runs after the fact (n8n's "error workflow"):
// every few seconds it claims runs that failed since it last looked and, per
// the workflow's settings (domain.WorkflowSettings),
//
//   - starts the error workflow with the failure as its input
//     ({{input.failedRun.workflow}}, .error, .failedNode, .url …), and
//   - posts an alert to a Slack incoming webhook and/or emails it.
//
// Claiming is a single UPDATE … RETURNING on runs.failure_handled_at, so a
// failure is handled once even across restarts. A run of the error workflow
// that itself fails never triggers the error workflow again (no loops).
//
// Per-node retry / timeout / continue-on-fail live in the reasoner
// (workflow.py); this file only sees runs whose failure survived those.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

// FailureWatcher reacts to failed runs.
type FailureWatcher struct {
	deps     Deps
	runs     *RunService
	http     *http.Client
	interval time.Duration
	// sendMail is net/smtp.SendMail; swapped in tests.
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

func (p *Platform) NewFailureWatcher() *FailureWatcher {
	return &FailureWatcher{
		deps:     Deps{Repos: p.Repos, Bus: p.Bus, Clock: p.Clock, Logger: p.Logger},
		runs:     p.Runs,
		http:     &http.Client{Timeout: 10 * time.Second},
		interval: 5 * time.Second,
		sendMail: smtp.SendMail,
	}
}

func (f *FailureWatcher) Start(ctx context.Context) {
	t := time.NewTicker(f.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.Tick()
		}
	}
}

// Tick handles every newly failed run once.
func (f *FailureWatcher) Tick() {
	for _, run := range f.deps.Repos.Runs.ClaimFailures(20) {
		f.handle(run)
	}
}

func (f *FailureWatcher) handle(run domain.Run) {
	wf, ok := f.deps.Repos.Workflows.GetByID(run.WorkflowID)
	if !ok {
		return
	}
	set := wf.Settings
	if set.ErrorWorkflow == "" && set.AlertEmail == "" && set.AlertSlackWebhook == "" {
		return
	}
	failure := f.describe(run, wf)

	if _, isErrorRun := run.Input["failedRun"]; set.ErrorWorkflow != "" && set.ErrorWorkflow != wf.Slug && !isErrorRun {
		if _, err := f.runs.Launch(set.ErrorWorkflow, validate.LaunchRunInput{
			Trigger: domain.TriggerEvent,
			Input:   map[string]any{"failedRun": failure},
		}); err != nil {
			f.deps.Logger.Error("error workflow launch failed", "workflow", wf.Slug, "errorWorkflow", set.ErrorWorkflow, "error", err.Error())
		}
	}

	text := fmt.Sprintf("Agently: run #%d of %q failed", run.Number, wf.Name)
	if n, _ := failure["failedNode"].(string); n != "" {
		text += " at node " + n
	}
	if e, _ := failure["error"].(string); e != "" {
		text += ": " + e
	}
	text += "\n" + failure["url"].(string)

	if set.AlertSlackWebhook != "" {
		if err := f.postSlack(set.AlertSlackWebhook, text); err != nil {
			f.deps.Logger.Error("slack failure alert failed", "workflow", wf.Slug, "error", err.Error())
		}
	}
	if set.AlertEmail != "" {
		if err := f.email(set.AlertEmail, fmt.Sprintf("Run #%d of %s failed", run.Number, wf.Name), text); err != nil {
			f.deps.Logger.Error("email failure alert failed", "workflow", wf.Slug, "error", err.Error())
		}
	}
}

// describe is the error workflow's input: enough to report or retry.
func (f *FailureWatcher) describe(run domain.Run, wf domain.Workflow) map[string]any {
	out := map[string]any{
		"runId":    string(run.ID),
		"number":   run.Number,
		"workflow": wf.Name,
		"slug":     wf.Slug,
		"url":      strings.TrimRight(envOrDefault("APP_BASE_URL", "http://localhost:3000"), "/") + "/runs/" + string(run.ID),
		"error":    "",
	}
	if run.Error != nil {
		out["error"] = *run.Error
	}
	for _, a := range f.deps.Repos.RunAgents.ListByRun(run.ID) {
		if a.Status == domain.AgentFailed {
			out["failedNode"] = orStr(a.NodeKey, a.Name)
			if a.Error != "" {
				out["error"] = a.Error
			}
			break
		}
	}
	return out
}

func (f *FailureWatcher) postSlack(webhook, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := f.http.Post(webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("slack webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// email uses the same SMTP_* settings as the reasoner's output.email node.
func (f *FailureWatcher) email(to, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return fmt.Errorf("SMTP_HOST is not set")
	}
	user, pass := os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASS")
	from := orStr(os.Getenv("SMTP_FROM"), user)
	var auth smtp.Auth
	if user != "" && pass != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	msg := "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
	return f.sendMail(host+":"+envOrDefault("SMTP_PORT", "587"), auth, from, []string{to}, []byte(msg))
}
