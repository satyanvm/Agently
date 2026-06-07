package platform

import (
	"fmt"
	"time"

	"github.com/agently/api/internal/domain"
)

// The canonical seed dataset. A 1:1 port of
// packages/core/src/platform/seed.ts — same ids, timestamps, strings, and
// numbers — so the Go API serves the identical seeded world the TS backend did.
// Everything is anchored to a fixed run-start instant for deterministic
// timestamps across processes.

const (
	runStart = "2026-06-06T17:09:12Z"
	// NowISO is the seed's "current time" anchor (mirrors NOW_ISO in seed.ts).
	NowISO = "2026-06-06T17:42:00Z"
)

var runStartMs = mustParseMs(runStart)

func mustParseMs(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

// iso returns the ISO string offsetSec seconds after the run start.
func iso(offsetSec int) domain.Timestamp {
	t := time.UnixMilli(runStartMs + int64(offsetSec)*1000).UTC()
	return domain.Timestamp(t.Format(time.RFC3339))
}

const ws = domain.WorkspaceId("ws_northwind")
const run8842 = domain.RunId("run_8842")
const bs1 = domain.BrowserSessionId("bs_1")

// Seed bundles the data plus materialized stat read-models.
type Seed struct {
	Data           MemoryData
	WorkflowStats  map[string]domain.WorkflowStats
	WorkspaceStats domain.WorkspaceStats
}

// BuildSeed constructs the canonical dataset. The clock parameter mirrors the
// TS signature; timestamps here are literal/offset-based so it is unused.
func BuildSeed(_ Clock) Seed {
	workspace := domain.Workspace{
		ID: ws, Name: "Northwind Labs", Slug: "northwind", Plan: domain.PlanPro,
		DefaultRegion: "us-east-1", CreatedAt: "2026-01-02T10:00:00Z",
	}

	members := []domain.Member{
		{ID: "mem_maya", WorkspaceID: ws, Name: "Maya Chen", Email: "maya@northwind.dev", Initials: "MC", Role: domain.MemberOwner, CreatedAt: "2026-01-02T10:00:00Z"},
		{ID: "mem_dev", WorkspaceID: ws, Name: "Dev Patel", Email: "dev@northwind.dev", Initials: "DP", Role: domain.MemberAdmin, CreatedAt: "2026-01-04T10:00:00Z"},
		{ID: "mem_lena", WorkspaceID: ws, Name: "Lena Ortiz", Email: "lena@northwind.dev", Initials: "LO", Role: domain.MemberMember, CreatedAt: "2026-02-09T10:00:00Z"},
		{ID: "mem_sam", WorkspaceID: ws, Name: "Sam Idowu", Email: "sam@northwind.dev", Initials: "SI", Role: domain.MemberMember, CreatedAt: "2026-04-22T10:00:00Z"},
	}

	agentDef := func(id, name string, role domain.AgentRole, model, description string, tools []string) domain.AgentDefinition {
		return domain.AgentDefinition{
			ID: domain.AgentDefinitionId(id), WorkspaceID: ws, Name: name, Role: role,
			Model: model, Description: description, Tools: tools, Config: map[string]any{},
			CreatedAt: "2026-03-01T10:00:00Z", UpdatedAt: "2026-06-01T10:00:00Z",
		}
	}
	agents := []domain.AgentDefinition{
		agentDef("agt_conductor", "Conductor", domain.RoleOrchestrator, "claude-opus-4-8", "Plans multi-agent sweeps and gates final output.", []string{"plan", "delegate", "review"}),
		agentDef("agt_scout", "Scout", domain.RoleResearcher, "claude-sonnet-4-6", "Open-web research with structured extraction.", []string{"web.search", "fetch", "rss.poll"}),
		agentDef("agt_navigator", "Navigator", domain.RoleBrowser, "claude-sonnet-4-6", "Drives a headless browser to capture live UIs.", []string{"browser.navigate", "browser.click", "screenshot"}),
		agentDef("agt_synth", "Synthesizer", domain.RoleAnalyst, "claude-opus-4-8", "Reconciles signals into structured matrices.", []string{"sql", "reconcile", "chart"}),
		agentDef("agt_composer", "Composer", domain.RoleWriter, "claude-opus-4-8", "Long-form writing from structured inputs.", []string{"write", "cite", "format"}),
		agentDef("agt_auditor", "Auditor", domain.RoleValidator, "claude-sonnet-4-6", "Fact-checks claims against captured sources.", []string{"verify", "diff", "flag"}),
		agentDef("agt_triage", "Triage", domain.RoleAnalyst, "claude-haiku-4-5", "Classifies and routes inbound support email.", []string{"classify", "route", "draft"}),
		agentDef("agt_patcher", "Patcher", domain.RoleCoder, "claude-opus-4-8", "Writes and runs small fix-up scripts.", []string{"python", "shell", "test"}),
	}

	wf := func(id, slug, name, description string, trigger domain.TriggerType, schedule *string, ownerID string, tags []string, agentCount int, createdAt string) domain.Workflow {
		var currentVersionID *domain.WorkflowVersionId
		if id == "wf_compint" {
			currentVersionID = domain.Ptr(domain.WorkflowVersionId("wfv_compint_12"))
		}
		owner := domain.MemberId(ownerID)
		return domain.Workflow{
			ID: domain.WorkflowId(id), WorkspaceID: ws, Slug: slug, Name: name, Description: description,
			Trigger: trigger, Schedule: schedule, Tags: tags, OwnerID: &owner, AgentCount: agentCount,
			CurrentVersionID: currentVersionID, CreatedAt: domain.Timestamp(createdAt),
			UpdatedAt: domain.Timestamp(createdAt), ArchivedAt: nil,
		}
	}
	sched := func(s string) *string { return &s }
	workflows := []domain.Workflow{
		wf("wf_compint", "competitive-intelligence-sweep", "Competitive Intelligence Sweep", "A 7-agent crew that researches competitors, captures live product UIs, and ships an executive brief twice a week.", domain.TriggerSchedule, sched("Mon, Thu · 09:00 UTC"), "mem_maya", []string{"research", "browser", "report"}, 7, "2026-03-02T10:00:00Z"),
		wf("wf_inbox", "inbox-triage-autopilot", "Inbox Triage Autopilot", "Continuously classifies, drafts, and routes incoming support email; escalates anything ambiguous to a human.", domain.TriggerEvent, nil, "mem_dev", []string{"support", "email"}, 3, "2026-01-18T10:00:00Z"),
		wf("wf_dataqa", "nightly-data-qa", "Nightly Data QA", "Runs schema, freshness and anomaly checks across the warehouse, then opens issues for anything that drifts.", domain.TriggerSchedule, sched("Daily · 02:00 UTC"), "mem_lena", []string{"data", "qa", "monitoring"}, 4, "2026-02-09T10:00:00Z"),
		wf("wf_recruit", "candidate-sourcing", "Candidate Sourcing Pipeline", "Sources, screens and ranks candidates from public profiles, then prepares outreach drafts for review.", domain.TriggerManual, nil, "mem_sam", []string{"recruiting", "browser"}, 5, "2026-04-22T10:00:00Z"),
		wf("wf_seo", "seo-content-engine", "SEO Content Engine", "Researches keywords, drafts long-form articles, and stages them in the CMS with internal links.", domain.TriggerSchedule, sched("Weekdays · 06:00 UTC"), "mem_maya", []string{"content", "seo"}, 4, "2026-03-30T10:00:00Z"),
		wf("wf_finance", "invoice-reconciliation", "Invoice Reconciliation", "Matches invoices to POs and ledger entries, flags mismatches, and prepares a daily reconciliation report.", domain.TriggerSchedule, sched("Daily · 23:30 UTC"), "mem_dev", []string{"finance", "ops"}, 3, "2026-05-11T10:00:00Z"),
	}

	ra := func(id, name string, role domain.AgentRole, model string, status domain.AgentStatus, dependsOn []string, col, row int, summary string, tokens int, costUsd float64, runtimeMs *int, toolCalls int, progress float64) domain.RunAgent {
		deps := make([]domain.RunAgentId, len(dependsOn))
		for i, d := range dependsOn {
			deps[i] = domain.RunAgentId(d)
		}
		var startedAt *domain.Timestamp
		if status != domain.AgentIdle {
			startedAt = domain.Ptr(iso(10))
		}
		var finishedAt *domain.Timestamp
		if status == domain.AgentSucceeded {
			finishedAt = domain.Ptr(iso(640))
		}
		return domain.RunAgent{
			ID: domain.RunAgentId(id), RunID: run8842, AgentDefinitionID: nil, Name: name, Role: role,
			Model: model, Status: status, DependsOn: deps, Col: col, Row: row, Summary: summary,
			Metrics:   domain.AgentMetrics{Tokens: tokens, CostUsd: costUsd, RuntimeMs: runtimeMs, ToolCalls: toolCalls, Progress: progress},
			StartedAt: startedAt, FinishedAt: finishedAt,
		}
	}
	runAgents := []domain.RunAgent{
		ra("ra_conductor", "Conductor", domain.RoleOrchestrator, "claude-opus-4-8", domain.AgentRunning, nil, 0, 1, "Plans the sweep, delegates to scouts, and gates the final report.", 48210, 0.96, domain.Ptr(1_980_000), 22, 0.7),
		ra("ra_scouta", "Scout · Pricing", domain.RoleResearcher, "claude-sonnet-4-6", domain.AgentSucceeded, []string{"ra_conductor"}, 1, 0, "Collected pricing & packaging for 11 competitors.", 92430, 0.71, domain.Ptr(612_000), 41, 1),
		ra("ra_scoutb", "Scout · Launches", domain.RoleResearcher, "claude-sonnet-4-6", domain.AgentRunning, []string{"ra_conductor"}, 1, 1, "Tracking changelogs, blog posts and release notes.", 64120, 0.49, domain.Ptr(540_000), 33, 0.62),
		ra("ra_navigator", "Navigator", domain.RoleBrowser, "claude-sonnet-4-6", domain.AgentRunning, []string{"ra_conductor"}, 1, 2, "Drives a headless browser to capture live product UIs.", 38800, 0.33, domain.Ptr(498_000), 58, 0.55),
		ra("ra_synth", "Synthesizer", domain.RoleAnalyst, "claude-opus-4-8", domain.AgentWaiting, []string{"ra_scouta", "ra_scoutb", "ra_navigator"}, 2, 1, "Reconciles signals into a structured competitive matrix.", 12010, 0.28, nil, 4, 0.1),
		ra("ra_composer", "Composer", domain.RoleWriter, "claude-opus-4-8", domain.AgentIdle, []string{"ra_synth"}, 3, 1, "Writes the executive brief and one-pager.", 0, 0, nil, 0, 0),
		ra("ra_auditor", "Auditor", domain.RoleValidator, "claude-sonnet-4-6", domain.AgentIdle, []string{"ra_composer"}, 4, 1, "Fact-checks every claim against captured sources.", 0, 0, nil, 0, 0),
	}

	msg := func(id, from, to, label string, off int) domain.AgentMessage {
		return domain.AgentMessage{ID: domain.AgentMessageId(id), RunID: run8842, FromAgentID: domain.RunAgentId(from), ToAgentID: domain.RunAgentId(to), Label: label, At: iso(off)}
	}
	messages := []domain.AgentMessage{
		msg("msg_1", "ra_conductor", "ra_scouta", "scope: pricing", 40),
		msg("msg_2", "ra_conductor", "ra_scoutb", "scope: launches", 44),
		msg("msg_3", "ra_conductor", "ra_navigator", "capture UIs", 52),
		msg("msg_4", "ra_scouta", "ra_synth", "11 pricing rows", 640),
		msg("msg_5", "ra_scoutb", "ra_synth", "partial: 18 items", 900),
		msg("msg_6", "ra_navigator", "ra_synth", "24 screenshots", 960),
		msg("msg_7", "ra_synth", "ra_composer", "matrix v1", 1180),
		msg("msg_8", "ra_composer", "ra_auditor", "draft brief", 1320),
	}

	art := func(id, name string, kind domain.ArtifactKind, size int, by string, off int, preview *string) domain.Artifact {
		return domain.Artifact{ID: domain.ArtifactId(id), RunID: run8842, Name: name, Kind: kind, SizeBytes: domain.Ptr(size), ProducedByAgentID: nil, ProducedByName: by, StorageKey: nil, Preview: preview, CreatedAt: iso(off)}
	}
	prev := func(s string) *string { return &s }
	artifacts := []domain.Artifact{
		art("art_1", "competitive-matrix.json", domain.ArtifactJSON, 48213, "Synthesizer", 1190, prev(`{ "competitors": 11, "dimensions": 9 }`)),
		art("art_2", "pricing-table.csv", domain.ArtifactDataset, 8841, "Scout · Pricing", 648, nil),
		art("art_3", "executive-brief.md", domain.ArtifactReport, 14920, "Composer", 1330, prev("# Competitive Landscape — Q2 2026")),
		art("art_4", "navigator-capture-24.png", domain.ArtifactImage, 1843200, "Navigator", 965, nil),
		art("art_5", "sources.json", domain.ArtifactJSON, 22104, "Scout · Launches", 905, nil),
		art("art_6", "scrape-runner.ts", domain.ArtifactCode, 3120, "Navigator", 120, nil),
	}

	// Workflow version (flagship graph): derived from runAgents, stripping the
	// "ra_" prefix to form node keys.
	nodes := make([]domain.GraphNode, len(runAgents))
	for i, a := range runAgents {
		deps := make([]string, len(a.DependsOn))
		for j, d := range a.DependsOn {
			deps[j] = trimPrefix(string(d), "ra_")
		}
		nodes[i] = domain.GraphNode{
			Key: trimPrefix(string(a.ID), "ra_"), AgentDefinitionID: nil, Name: a.Name,
			Role: a.Role, Model: a.Model, Col: a.Col, Row: a.Row, DependsOn: deps,
		}
	}
	versions := []domain.WorkflowVersion{{
		ID: "wfv_compint_12", WorkflowID: "wf_compint", Version: 12, Note: "Add Auditor fact-check gate",
		CreatedBy: domain.Ptr(domain.MemberId("mem_maya")), CreatedAt: "2026-06-06T08:55:00Z", Nodes: nodes,
	}}

	run := func(id string, number int, workflowID, workflowName, workflowSlug string, status domain.RunStatus, trigger domain.TriggerType, by domain.Principal, startedAt *string, finishedAt *string, costUsd float64, tokensIn, tokensOut, done, total int, currentStep string, browserSessionID *string) domain.Run {
		var versionID *domain.WorkflowVersionId
		if workflowID == "wf_compint" {
			versionID = domain.Ptr(domain.WorkflowVersionId("wfv_compint_12"))
		}
		var errPtr *string
		if status == domain.RunFailed {
			errPtr = &currentStep
		}
		var bsID *domain.BrowserSessionId
		if browserSessionID != nil {
			bsID = domain.Ptr(domain.BrowserSessionId(*browserSessionID))
		}
		queuedAt := iso(-6)
		if startedAt != nil {
			queuedAt = domain.Timestamp(*startedAt)
		}
		var startedPtr *domain.Timestamp
		if startedAt != nil {
			startedPtr = domain.Ptr(domain.Timestamp(*startedAt))
		}
		var finishedPtr *domain.Timestamp
		if finishedAt != nil {
			finishedPtr = domain.Ptr(domain.Timestamp(*finishedAt))
		}
		return domain.Run{
			ID: domain.RunId(id), WorkspaceID: ws, WorkflowID: domain.WorkflowId(workflowID), WorkflowVersionID: versionID,
			WorkflowName: workflowName, WorkflowSlug: workflowSlug, Number: number, Status: status, Trigger: trigger,
			TriggeredBy: by, Region: "us-east-1", Steps: domain.StepProgress{Done: done, Total: total}, CurrentStep: currentStep,
			CostUsd: costUsd, Usage: domain.Usage{TokensIn: tokensIn, TokensOut: tokensOut}, Error: errPtr,
			BrowserSessionID: bsID, QueuedAt: queuedAt, StartedAt: startedPtr, FinishedAt: finishedPtr,
		}
	}
	ts := func(s string) *string { return &s }
	scheduler := domain.Principal{Name: "Scheduler", Initials: "SC"}
	webhook := domain.Principal{Name: "Webhook", Initials: "WH"}
	runs := []domain.Run{
		run("run_8842", 142, "wf_compint", "Competitive Intelligence Sweep", "competitive-intelligence-sweep", domain.RunRunning, domain.TriggerSchedule, scheduler, ts(string(iso(0))), nil, 4.21, 268400, 41280, 19, 27, "Synthesizing competitive matrix", ts("bs_1")),
		run("run_8841", 1284, "wf_inbox", "Inbox Triage Autopilot", "inbox-triage-autopilot", domain.RunRunning, domain.TriggerEvent, webhook, ts("2026-06-06T17:38:00Z"), nil, 0.04, 8200, 1400, 2, 3, "Drafting reply", nil),
		run("run_8839", 141, "wf_compint", "Competitive Intelligence Sweep", "competitive-intelligence-sweep", domain.RunSucceeded, domain.TriggerSchedule, scheduler, ts("2026-06-03T09:00:00Z"), ts("2026-06-03T09:39:00Z"), 5.74, 251000, 39800, 27, 27, "Completed", nil),
		run("run_8836", 117, "wf_dataqa", "Nightly Data QA", "nightly-data-qa", domain.RunSucceeded, domain.TriggerSchedule, scheduler, ts("2026-06-06T02:00:00Z"), ts("2026-06-06T02:18:00Z"), 1.32, 94000, 12100, 14, 14, "Completed", nil),
		run("run_8833", 31, "wf_recruit", "Candidate Sourcing Pipeline", "candidate-sourcing", domain.RunFailed, domain.TriggerManual, domain.Principal{Name: "Sam Idowu", Initials: "SI"}, ts("2026-06-06T14:12:00Z"), ts("2026-06-06T14:31:00Z"), 2.18, 142000, 18400, 9, 14, "Browser session timed out", nil),
		run("run_8830", 63, "wf_seo", "SEO Content Engine", "seo-content-engine", domain.RunCanceled, domain.TriggerSchedule, domain.Principal{Name: "Maya Chen", Initials: "MC"}, ts("2026-06-05T06:00:00Z"), ts("2026-06-05T06:04:00Z"), 0.21, 14000, 2200, 2, 12, "Canceled", nil),
		run("run_8828", 1283, "wf_inbox", "Inbox Triage Autopilot", "inbox-triage-autopilot", domain.RunSucceeded, domain.TriggerEvent, webhook, ts("2026-06-06T17:31:00Z"), ts("2026-06-06T17:31:42Z"), 0.07, 9100, 1600, 3, 3, "Completed", nil),
		run("run_8825", 26, "wf_finance", "Invoice Reconciliation", "invoice-reconciliation", domain.RunSucceeded, domain.TriggerSchedule, scheduler, ts("2026-06-05T23:30:00Z"), ts("2026-06-05T23:35:00Z"), 0.58, 41000, 5200, 8, 8, "Completed", nil),
	}

	// Logs (flagship).
	type rawLog struct {
		offset  int
		level   domain.LogLevel
		channel domain.LogChannel
		source  string
		message string
		detail  string
	}
	rawLogs := []rawLog{
		{0, domain.LevelInfo, domain.ChannelSystem, "scheduler", "Run #142 queued from schedule 'Mon, Thu · 09:00 UTC'", ""},
		{2, domain.LevelInfo, domain.ChannelSystem, "runtime", "Provisioning sandbox · us-east-1 · 4 vCPU / 8 GiB", ""},
		{6, domain.LevelSuccess, domain.ChannelSystem, "runtime", "Sandbox ready in 3.9s — workspace mounted", ""},
		{9, domain.LevelInfo, domain.ChannelAgent, "Conductor", "Booting orchestrator with 6 downstream agents", ""},
		{12, domain.LevelInfo, domain.ChannelModel, "Conductor", "Planning sweep across 11 target competitors", "Reasoning: prioritize pricing + recent launches; parallelize scouts."},
		{40, domain.LevelInfo, domain.ChannelAgent, "Conductor", "→ Scout · Pricing  ·  scope=pricing, targets=11", ""},
		{52, domain.LevelInfo, domain.ChannelAgent, "Conductor", "→ Navigator ·  capture live product UIs", ""},
		{61, domain.LevelInfo, domain.ChannelTool, "Scout · Pricing", "web.search('competitor pricing 2026')  · 11 results", ""},
		{96, domain.LevelInfo, domain.ChannelBrowser, "Navigator", "session.open()  · viewport 1440×900 · us-east-1", ""},
		{120, domain.LevelWarn, domain.ChannelTool, "Scout · Launches", "rival.io returned 429 — backing off 8s, attempt 1/3", ""},
		{151, domain.LevelInfo, domain.ChannelBrowser, "Navigator", "screenshot captured · acme-pricing.png (1.8 MB)", ""},
		{360, domain.LevelError, domain.ChannelBrowser, "Navigator", "net::ERR_TIMED_OUT loading https://legacy-corp.com (30s)", "Host blocks datacenter IPs. Falling back to cached snapshot."},
		{430, domain.LevelSuccess, domain.ChannelAgent, "Scout · Pricing", "Completed — 11/11 pricing rows extracted", ""},
		{612, domain.LevelWarn, domain.ChannelModel, "Synthesizer", "Conflicting price for 'Nimbus Pro': $49 (page) vs $59 (cache)", ""},
		{1180, domain.LevelSuccess, domain.ChannelAgent, "Synthesizer", "Matrix v1 ready · 99 cells, 3 flagged", ""},
		{1410, domain.LevelWarn, domain.ChannelAgent, "Auditor", "Claim 14 lacks a primary source — requesting recapture", ""},
	}
	logs := make([]domain.LogEntry, len(rawLogs))
	for i, r := range rawLogs {
		var detail *string
		if r.detail != "" {
			d := r.detail
			detail = &d
		}
		logs[i] = domain.LogEntry{
			ID: domain.LogId(fmt.Sprintf("log_%04d", i)), RunID: run8842, Seq: i, Ts: iso(r.offset),
			OffsetMs: r.offset * 1000, Level: r.level, Channel: r.channel, Source: r.source,
			Message: r.message, Detail: detail, Reasoning: r.channel == domain.ChannelModel,
		}
	}

	// Browser session.
	browserSessions := []domain.BrowserSession{{
		ID: bs1, RunID: run8842, AgentName: "Navigator", Status: domain.RunRunning,
		CurrentURL: "https://nimbus.ai/pricing", PageTitle: "Pricing — Nimbus AI",
		Viewport: domain.Viewport{Width: 1440, Height: 900}, PagesVisited: 14, ActionsCount: 58,
		StartedAt: iso(96), FinishedAt: nil,
	}}

	shot := func(id string, off int, url, title, label string) domain.BrowserShot {
		return domain.BrowserShot{ID: domain.BrowserShotId(id), SessionID: bs1, Ts: iso(off), URL: url, Title: title, Label: label, StorageKey: nil}
	}
	browserShots := []domain.BrowserShot{
		shot("shot_1", 110, "https://acme.dev/product", "Acme · Product", "Acme product hero"),
		shot("shot_2", 151, "https://acme.dev/pricing", "Acme · Pricing", "Acme pricing grid"),
		shot("shot_3", 322, "https://nimbus.ai", "Nimbus · Home", "Nimbus landing"),
		shot("shot_4", 560, "https://nimbus.ai/pricing", "Nimbus · Pricing", "Nimbus pricing grid"),
		shot("shot_5", 965, "https://rival.io/product", "Rival · Product", "Rival product tour"),
	}

	action := func(id string, off int, typ domain.BrowserActionType, target string, value *string, status string, dur int) domain.BrowserAction {
		return domain.BrowserAction{ID: domain.BrowserActionId(id), SessionID: bs1, Ts: iso(off), Type: typ, Target: target, Value: value, Status: status, DurationMs: dur}
	}
	val := func(s string) *string { return &s }
	browserActions := []domain.BrowserAction{
		action("ba_1", 96, domain.BrowserNavigate, "https://acme.dev/product", nil, "ok", 940),
		action("ba_2", 142, domain.BrowserClick, "button[data-cta='see-pricing']", nil, "ok", 120),
		action("ba_3", 151, domain.BrowserScreenshot, "viewport", nil, "ok", 310),
		action("ba_4", 360, domain.BrowserNavigate, "https://legacy-corp.com", nil, "error", 30000),
		action("ba_5", 560, domain.BrowserExtract, "table.pricing-grid", val("9 columns"), "ok", 240),
		action("ba_6", 965, domain.BrowserScreenshot, "viewport", val("capture set ×24"), "ok", 380),
	}

	consoleRows := []struct {
		off   int
		level domain.LogLevel
		text  string
	}{
		{105, domain.LevelInfo, "[page] DOMContentLoaded in 0.9s"},
		{360, domain.LevelError, "GET https://legacy-corp.com net::ERR_TIMED_OUT"},
		{561, domain.LevelInfo, "[extract] matched 1 table, 11 rows"},
	}
	browserConsole := make([]ConsoleRow, len(consoleRows))
	for i, c := range consoleRows {
		browserConsole[i] = ConsoleRow{SessionID: bs1, Line: domain.BrowserConsoleLine{Ts: iso(c.off), Level: c.level, Text: c.text}}
	}

	notif := func(id string, typ domain.NotificationType, severity domain.Severity, title, body, slug, runID string, num int, createdAt string, readAt *string) domain.Notification {
		var readPtr *domain.Timestamp
		if readAt != nil {
			readPtr = domain.Ptr(domain.Timestamp(*readAt))
		}
		return domain.Notification{
			ID: domain.NotificationId(id), WorkspaceID: ws, RecipientID: domain.Ptr(domain.MemberId("mem_maya")),
			Type: typ, Severity: severity, Title: title, Body: body, WorkflowSlug: domain.Ptr(slug),
			RunID: domain.Ptr(domain.RunId(runID)), RunNumber: domain.Ptr(num), ReadAt: readPtr, CreatedAt: domain.Timestamp(createdAt),
		}
	}
	notifications := []domain.Notification{
		notif("ntf_1", domain.NotifCostAlert, domain.SeverityWarning, "Cost threshold approaching", "Competitive Intelligence Sweep run #142 has used $4.21 of its $6.00 budget.", "competitive-intelligence-sweep", "run_8842", 142, "2026-06-06T17:40:00Z", nil),
		notif("ntf_2", domain.NotifBrowserError, domain.SeverityWarning, "Browser navigation failed", "Navigator hit net::ERR_TIMED_OUT on legacy-corp.com and fell back to a cached snapshot.", "competitive-intelligence-sweep", "run_8842", 142, "2026-06-06T17:15:12Z", nil),
		notif("ntf_3", domain.NotifWorkflowFailed, domain.SeverityError, "Workflow failed", "Candidate Sourcing Pipeline run #31 failed: browser session timed out at step 9/14.", "candidate-sourcing", "run_8833", 31, "2026-06-06T14:31:00Z", nil),
		notif("ntf_4", domain.NotifWorkflowCompleted, domain.SeveritySuccess, "Workflow completed", "Nightly Data QA run #117 finished successfully in 18m with 0 anomalies.", "nightly-data-qa", "run_8836", 117, "2026-06-06T02:18:00Z", ts("2026-06-06T08:00:00Z")),
		notif("ntf_5", domain.NotifAgentBlocked, domain.SeverityInfo, "Agent blocked", "Auditor requested a recapture — claim 14 in the executive brief lacks a primary source.", "competitive-intelligence-sweep", "run_8842", 142, "2026-06-06T17:33:00Z", ts("2026-06-06T17:34:00Z")),
		notif("ntf_6", domain.NotifWorkflowCompleted, domain.SeveritySuccess, "Workflow completed", "Competitive Intelligence Sweep run #141 shipped the Q2 brief to 4 recipients.", "competitive-intelligence-sweep", "run_8839", 141, "2026-06-03T09:39:00Z", ts("2026-06-03T10:00:00Z")),
	}

	act := func(id string, kind domain.ActivityKind, actor, text, slug string, runID *string, at string) domain.ActivityEvent {
		var runPtr *domain.RunId
		if runID != nil {
			runPtr = domain.Ptr(domain.RunId(*runID))
		}
		return domain.ActivityEvent{ID: domain.ActivityId(id), WorkspaceID: ws, Kind: kind, Actor: actor, Text: text, WorkflowSlug: domain.Ptr(slug), RunID: runPtr, At: domain.Timestamp(at)}
	}
	activity := []domain.ActivityEvent{
		act("act_1", domain.ActivityRun, "Scheduler", "started run #142 of Competitive Intelligence Sweep", "competitive-intelligence-sweep", ts("run_8842"), "2026-06-06T17:09:12Z"),
		act("act_2", domain.ActivityComplete, "Inbox Triage Autopilot", "completed run #1283 in 42s", "inbox-triage-autopilot", ts("run_8828"), "2026-06-06T17:31:42Z"),
		act("act_3", domain.ActivityFail, "Candidate Sourcing Pipeline", "failed run #31 — browser timeout", "candidate-sourcing", ts("run_8833"), "2026-06-06T14:31:00Z"),
		act("act_4", domain.ActivityDeploy, "Maya Chen", "deployed v12 of Competitive Intelligence Sweep", "competitive-intelligence-sweep", nil, "2026-06-06T08:55:00Z"),
		act("act_5", domain.ActivityScale, "Autoscaler", "scaled Inbox Triage to 3 concurrent sandboxes", "inbox-triage-autopilot", nil, "2026-06-06T12:02:00Z"),
		act("act_6", domain.ActivityComplete, "Nightly Data QA", "completed run #117 — 0 anomalies", "nightly-data-qa", ts("run_8836"), "2026-06-06T02:18:00Z"),
		act("act_7", domain.ActivityComment, "Dev Patel", "paused SEO Content Engine pending CMS migration", "seo-content-engine", nil, "2026-06-05T18:40:00Z"),
	}

	stat := func(successRate float64, avgRuntimeMs int, avgCostUsd float64, totalRuns int, recent []int, trend []float64) domain.WorkflowStats {
		return domain.WorkflowStats{SuccessRate: successRate, AvgRuntimeMs: avgRuntimeMs, AvgCostUsd: avgCostUsd, TotalRuns: totalRuns, Recent: recent, Trend: trend}
	}
	workflowStats := map[string]domain.WorkflowStats{
		"wf_compint": stat(0.96, 2_340_000, 5.9, 48, []int{1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1}, []float64{3, 4, 2, 5, 6, 4, 7, 5, 8, 6, 7, 9, 8, 10}),
		"wf_inbox":   stat(0.991, 42_000, 0.08, 12840, []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1}, []float64{120, 140, 110, 160, 180, 150, 210, 190, 230, 250, 240, 280, 300, 320}),
		"wf_dataqa":  stat(0.92, 1_080_000, 1.4, 117, []int{1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1}, []float64{5, 6, 5, 7, 6, 8, 7, 6, 9, 8, 7, 8, 9, 8}),
		"wf_recruit": stat(0.78, 1_620_000, 3.1, 31, []int{1, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 0, 1, 0}, []float64{2, 3, 2, 4, 3, 5, 4, 3, 6, 5, 4, 5, 6, 4}),
		"wf_seo":     stat(0.88, 960_000, 2.2, 64, []int{1, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1}, []float64{4, 5, 4, 6, 5, 7, 6, 8, 7, 6, 8, 7, 9, 8}),
		"wf_finance": stat(0.95, 300_000, 0.6, 26, []int{1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1}, []float64{3, 4, 3, 5, 4, 6, 5, 4, 6, 5, 6, 7, 6, 7}),
	}

	workspaceStats := domain.WorkspaceStats{
		ActiveRuns: 2, RunsToday: 47, SuccessRate: 0.962, SpendTodayUsd: 38.42, SpendBudgetUsd: 120,
		ComputeHours: 64.8, TokensToday: 4_820_000,
		RunVolume:   []int{2, 1, 1, 0, 1, 2, 3, 5, 8, 11, 9, 12, 14, 10, 13, 9, 11, 7, 6, 4, 3, 5, 2, 3},
		SpendSeries: []float64{0.4, 0.2, 0.3, 0.1, 0.2, 0.6, 0.9, 1.4, 2.1, 3.0, 2.4, 3.2, 3.8, 2.7, 3.4, 2.2, 2.9, 1.8, 1.5, 1.0, 0.7, 1.2, 0.5, 0.6},
	}

	data := MemoryData{
		Workspace: workspace, Members: members, Agents: agents, Workflows: workflows, Versions: versions,
		Runs: runs, RunAgents: runAgents, Messages: messages, Artifacts: artifacts, Logs: logs,
		BrowserSessions: browserSessions, BrowserActions: browserActions, BrowserShots: browserShots,
		BrowserConsole: browserConsole, Notifications: notifications, Activity: activity,
	}
	return Seed{Data: data, WorkflowStats: workflowStats, WorkspaceStats: workspaceStats}
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}
