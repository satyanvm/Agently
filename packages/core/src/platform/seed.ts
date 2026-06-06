import type {
  Workspace,
  Member,
  AgentDefinition,
  Workflow,
  WorkflowVersion,
  Run,
  RunAgent,
  AgentMessage,
  Artifact,
  LogEntry,
  BrowserSession,
  BrowserAction,
  BrowserShot,
  BrowserConsoleLine,
  Notification,
  ActivityEvent,
  WorkflowStats,
  WorkspaceStats,
  LogLevel,
  LogChannel,
  AgentRole,
  AgentStatus,
  BrowserActionType,
  WorkspaceId,
  MemberId,
  AgentDefinitionId,
  WorkflowId,
  WorkflowVersionId,
  RunId,
  RunAgentId,
  AgentMessageId,
  ArtifactId,
  LogId,
  BrowserSessionId,
  BrowserActionId,
  BrowserShotId,
  NotificationId,
  ActivityId,
} from "@agently/contracts";
import type { Clock } from "./clock.js";
import type { MemoryData } from "./repositories/memory.js";

/**
 * The canonical seed dataset. It mirrors what the frontend renders today
 * (apps/web/lib/mock-data.ts) but expressed in the canonical entity model, so
 * the API serves the same world the UI already knows. This is a *separate*
 * server-side dataset — the frontend's own mock data is left untouched.
 *
 * Everything is anchored to a fixed run-start instant so timestamps are
 * deterministic across processes.
 */

const RUN_START = "2026-06-06T17:09:12Z";
const RUN_START_MS = Date.parse(RUN_START);
const NOW_ISO = "2026-06-06T17:42:00Z";
const iso = (offsetSec: number) => new Date(RUN_START_MS + offsetSec * 1000).toISOString();

/** Cast a literal string id into its branded type (seed values are trusted). */
const brand = <T>(v: string): T => v as unknown as T;

const WS = brand<WorkspaceId>("ws_northwind");
const RUN_8842 = brand<RunId>("run_8842");
const BS_1 = brand<BrowserSessionId>("bs_1");

export interface Seed {
  data: MemoryData;
  /** Materialized per-workflow rollups (cached read model). */
  workflowStats: Record<string, WorkflowStats>;
  workspaceStats: WorkspaceStats;
}

export function buildSeed(_clock: Clock): Seed {
  const workspace: Workspace = {
    id: WS,
    name: "Northwind Labs",
    slug: "northwind",
    plan: "pro",
    defaultRegion: "us-east-1",
    createdAt: "2026-01-02T10:00:00Z",
  };

  const members: Member[] = [
    { id: brand<MemberId>("mem_maya"), workspaceId: WS, name: "Maya Chen", email: "maya@northwind.dev", initials: "MC", role: "owner", createdAt: "2026-01-02T10:00:00Z" },
    { id: brand<MemberId>("mem_dev"), workspaceId: WS, name: "Dev Patel", email: "dev@northwind.dev", initials: "DP", role: "admin", createdAt: "2026-01-04T10:00:00Z" },
    { id: brand<MemberId>("mem_lena"), workspaceId: WS, name: "Lena Ortiz", email: "lena@northwind.dev", initials: "LO", role: "member", createdAt: "2026-02-09T10:00:00Z" },
    { id: brand<MemberId>("mem_sam"), workspaceId: WS, name: "Sam Idowu", email: "sam@northwind.dev", initials: "SI", role: "member", createdAt: "2026-04-22T10:00:00Z" },
  ];

  /* ---- agent definitions (library) ---- */
  const agentDef = (
    id: string,
    name: string,
    role: AgentRole,
    model: string,
    description: string,
    tools: string[],
  ): AgentDefinition => ({
    id: brand<AgentDefinitionId>(id),
    workspaceId: WS,
    name,
    role,
    model,
    description,
    tools,
    config: {},
    createdAt: "2026-03-01T10:00:00Z",
    updatedAt: "2026-06-01T10:00:00Z",
  });

  const agents: AgentDefinition[] = [
    agentDef("agt_conductor", "Conductor", "orchestrator", "claude-opus-4-8", "Plans multi-agent sweeps and gates final output.", ["plan", "delegate", "review"]),
    agentDef("agt_scout", "Scout", "researcher", "claude-sonnet-4-6", "Open-web research with structured extraction.", ["web.search", "fetch", "rss.poll"]),
    agentDef("agt_navigator", "Navigator", "browser", "claude-sonnet-4-6", "Drives a headless browser to capture live UIs.", ["browser.navigate", "browser.click", "screenshot"]),
    agentDef("agt_synth", "Synthesizer", "analyst", "claude-opus-4-8", "Reconciles signals into structured matrices.", ["sql", "reconcile", "chart"]),
    agentDef("agt_composer", "Composer", "writer", "claude-opus-4-8", "Long-form writing from structured inputs.", ["write", "cite", "format"]),
    agentDef("agt_auditor", "Auditor", "validator", "claude-sonnet-4-6", "Fact-checks claims against captured sources.", ["verify", "diff", "flag"]),
    agentDef("agt_triage", "Triage", "analyst", "claude-haiku-4-5", "Classifies and routes inbound support email.", ["classify", "route", "draft"]),
    agentDef("agt_patcher", "Patcher", "coder", "claude-opus-4-8", "Writes and runs small fix-up scripts.", ["python", "shell", "test"]),
  ];

  /* ---- workflows ---- */
  const wf = (
    id: string,
    slug: string,
    name: string,
    description: string,
    trigger: Workflow["trigger"],
    schedule: string | null,
    ownerId: string,
    tags: string[],
    agentCount: number,
    createdAt: string,
  ): Workflow => ({
    id: brand<WorkflowId>(id),
    workspaceId: WS,
    slug,
    name,
    description,
    trigger,
    schedule,
    tags,
    ownerId: brand<MemberId>(ownerId),
    agentCount,
    currentVersionId: id === "wf_compint" ? brand<WorkflowVersionId>("wfv_compint_12") : null,
    createdAt,
    updatedAt: createdAt,
    archivedAt: null,
  });

  const workflows: Workflow[] = [
    wf("wf_compint", "competitive-intelligence-sweep", "Competitive Intelligence Sweep", "A 7-agent crew that researches competitors, captures live product UIs, and ships an executive brief twice a week.", "schedule", "Mon, Thu · 09:00 UTC", "mem_maya", ["research", "browser", "report"], 7, "2026-03-02T10:00:00Z"),
    wf("wf_inbox", "inbox-triage-autopilot", "Inbox Triage Autopilot", "Continuously classifies, drafts, and routes incoming support email; escalates anything ambiguous to a human.", "event", null, "mem_dev", ["support", "email"], 3, "2026-01-18T10:00:00Z"),
    wf("wf_dataqa", "nightly-data-qa", "Nightly Data QA", "Runs schema, freshness and anomaly checks across the warehouse, then opens issues for anything that drifts.", "schedule", "Daily · 02:00 UTC", "mem_lena", ["data", "qa", "monitoring"], 4, "2026-02-09T10:00:00Z"),
    wf("wf_recruit", "candidate-sourcing", "Candidate Sourcing Pipeline", "Sources, screens and ranks candidates from public profiles, then prepares outreach drafts for review.", "manual", null, "mem_sam", ["recruiting", "browser"], 5, "2026-04-22T10:00:00Z"),
    wf("wf_seo", "seo-content-engine", "SEO Content Engine", "Researches keywords, drafts long-form articles, and stages them in the CMS with internal links.", "schedule", "Weekdays · 06:00 UTC", "mem_maya", ["content", "seo"], 4, "2026-03-30T10:00:00Z"),
    wf("wf_finance", "invoice-reconciliation", "Invoice Reconciliation", "Matches invoices to POs and ledger entries, flags mismatches, and prepares a daily reconciliation report.", "schedule", "Daily · 23:30 UTC", "mem_dev", ["finance", "ops"], 3, "2026-05-11T10:00:00Z"),
  ];

  /* ---- flagship run agents (graph) ---- */
  const ra = (
    id: string,
    name: string,
    role: AgentRole,
    model: string,
    status: AgentStatus,
    dependsOn: string[],
    col: number,
    row: number,
    summary: string,
    tokens: number,
    costUsd: number,
    runtimeMs: number | null,
    toolCalls: number,
    progress: number,
  ): RunAgent => ({
    id: brand<RunAgentId>(id),
    runId: RUN_8842,
    agentDefinitionId: null,
    name,
    role,
    model,
    status,
    dependsOn: dependsOn.map((d) => brand<RunAgentId>(d)),
    col,
    row,
    summary,
    metrics: { tokens, costUsd, runtimeMs, toolCalls, progress },
    startedAt: status === "idle" ? null : iso(10),
    finishedAt: status === "succeeded" ? iso(640) : null,
  });

  const runAgents: RunAgent[] = [
    ra("ra_conductor", "Conductor", "orchestrator", "claude-opus-4-8", "running", [], 0, 1, "Plans the sweep, delegates to scouts, and gates the final report.", 48210, 0.96, 1_980_000, 22, 0.7),
    ra("ra_scouta", "Scout · Pricing", "researcher", "claude-sonnet-4-6", "succeeded", ["ra_conductor"], 1, 0, "Collected pricing & packaging for 11 competitors.", 92430, 0.71, 612_000, 41, 1),
    ra("ra_scoutb", "Scout · Launches", "researcher", "claude-sonnet-4-6", "running", ["ra_conductor"], 1, 1, "Tracking changelogs, blog posts and release notes.", 64120, 0.49, 540_000, 33, 0.62),
    ra("ra_navigator", "Navigator", "browser", "claude-sonnet-4-6", "running", ["ra_conductor"], 1, 2, "Drives a headless browser to capture live product UIs.", 38800, 0.33, 498_000, 58, 0.55),
    ra("ra_synth", "Synthesizer", "analyst", "claude-opus-4-8", "waiting", ["ra_scouta", "ra_scoutb", "ra_navigator"], 2, 1, "Reconciles signals into a structured competitive matrix.", 12010, 0.28, null, 4, 0.1),
    ra("ra_composer", "Composer", "writer", "claude-opus-4-8", "idle", ["ra_synth"], 3, 1, "Writes the executive brief and one-pager.", 0, 0, null, 0, 0),
    ra("ra_auditor", "Auditor", "validator", "claude-sonnet-4-6", "idle", ["ra_composer"], 4, 1, "Fact-checks every claim against captured sources.", 0, 0, null, 0, 0),
  ];

  const msg = (id: string, from: string, to: string, label: string, off: number): AgentMessage => ({
    id: brand<AgentMessageId>(id),
    runId: RUN_8842,
    fromAgentId: brand<RunAgentId>(from),
    toAgentId: brand<RunAgentId>(to),
    label,
    at: iso(off),
  });
  const messages: AgentMessage[] = [
    msg("msg_1", "ra_conductor", "ra_scouta", "scope: pricing", 40),
    msg("msg_2", "ra_conductor", "ra_scoutb", "scope: launches", 44),
    msg("msg_3", "ra_conductor", "ra_navigator", "capture UIs", 52),
    msg("msg_4", "ra_scouta", "ra_synth", "11 pricing rows", 640),
    msg("msg_5", "ra_scoutb", "ra_synth", "partial: 18 items", 900),
    msg("msg_6", "ra_navigator", "ra_synth", "24 screenshots", 960),
    msg("msg_7", "ra_synth", "ra_composer", "matrix v1", 1180),
    msg("msg_8", "ra_composer", "ra_auditor", "draft brief", 1320),
  ];

  const art = (
    id: string,
    name: string,
    kind: Artifact["kind"],
    size: number,
    by: string,
    off: number,
    preview: string | null,
  ): Artifact => ({
    id: brand<ArtifactId>(id),
    runId: RUN_8842,
    name,
    kind,
    sizeBytes: size,
    producedByAgentId: null,
    producedByName: by,
    storageKey: null,
    preview,
    createdAt: iso(off),
  });
  const artifacts: Artifact[] = [
    art("art_1", "competitive-matrix.json", "json", 48213, "Synthesizer", 1190, '{ "competitors": 11, "dimensions": 9 }'),
    art("art_2", "pricing-table.csv", "dataset", 8841, "Scout · Pricing", 648, null),
    art("art_3", "executive-brief.md", "report", 14920, "Composer", 1330, "# Competitive Landscape — Q2 2026"),
    art("art_4", "navigator-capture-24.png", "image", 1843200, "Navigator", 965, null),
    art("art_5", "sources.json", "json", 22104, "Scout · Launches", 905, null),
    art("art_6", "scrape-runner.ts", "code", 3120, "Navigator", 120, null),
  ];

  /* ---- workflow version (flagship graph) ---- */
  const versions: WorkflowVersion[] = [
    {
      id: brand<WorkflowVersionId>("wfv_compint_12"),
      workflowId: brand<WorkflowId>("wf_compint"),
      version: 12,
      note: "Add Auditor fact-check gate",
      createdBy: brand<MemberId>("mem_maya"),
      createdAt: "2026-06-06T08:55:00Z",
      nodes: runAgents.map((a) => ({
        key: (a.id as string).replace("ra_", ""),
        agentDefinitionId: null,
        name: a.name,
        role: a.role,
        model: a.model,
        col: a.col,
        row: a.row,
        dependsOn: a.dependsOn.map((d) => (d as string).replace("ra_", "")),
      })),
    },
  ];

  /* ---- runs ---- */
  const run = (
    id: string,
    number: number,
    workflowId: string,
    workflowName: string,
    workflowSlug: string,
    status: Run["status"],
    trigger: Run["trigger"],
    by: { name: string; initials: string },
    startedAt: string | null,
    finishedAt: string | null,
    costUsd: number,
    tokensIn: number,
    tokensOut: number,
    done: number,
    total: number,
    currentStep: string,
    browserSessionId: string | null,
  ): Run => ({
    id: brand<RunId>(id),
    workspaceId: WS,
    workflowId: brand<WorkflowId>(workflowId),
    workflowVersionId: workflowId === "wf_compint" ? brand<WorkflowVersionId>("wfv_compint_12") : null,
    workflowName,
    workflowSlug,
    number,
    status,
    trigger,
    triggeredBy: by,
    region: "us-east-1",
    steps: { done, total },
    currentStep,
    costUsd,
    usage: { tokensIn, tokensOut },
    error: status === "failed" ? currentStep : null,
    browserSessionId: browserSessionId ? brand<BrowserSessionId>(browserSessionId) : null,
    queuedAt: startedAt ?? iso(-6),
    startedAt,
    finishedAt,
  });

  const runs: Run[] = [
    run("run_8842", 142, "wf_compint", "Competitive Intelligence Sweep", "competitive-intelligence-sweep", "running", "schedule", { name: "Scheduler", initials: "SC" }, iso(0), null, 4.21, 268400, 41280, 19, 27, "Synthesizing competitive matrix", "bs_1"),
    run("run_8841", 1284, "wf_inbox", "Inbox Triage Autopilot", "inbox-triage-autopilot", "running", "event", { name: "Webhook", initials: "WH" }, "2026-06-06T17:38:00Z", null, 0.04, 8200, 1400, 2, 3, "Drafting reply", null),
    run("run_8839", 141, "wf_compint", "Competitive Intelligence Sweep", "competitive-intelligence-sweep", "succeeded", "schedule", { name: "Scheduler", initials: "SC" }, "2026-06-03T09:00:00Z", "2026-06-03T09:39:00Z", 5.74, 251000, 39800, 27, 27, "Completed", null),
    run("run_8836", 117, "wf_dataqa", "Nightly Data QA", "nightly-data-qa", "succeeded", "schedule", { name: "Scheduler", initials: "SC" }, "2026-06-06T02:00:00Z", "2026-06-06T02:18:00Z", 1.32, 94000, 12100, 14, 14, "Completed", null),
    run("run_8833", 31, "wf_recruit", "Candidate Sourcing Pipeline", "candidate-sourcing", "failed", "manual", { name: "Sam Idowu", initials: "SI" }, "2026-06-06T14:12:00Z", "2026-06-06T14:31:00Z", 2.18, 142000, 18400, 9, 14, "Browser session timed out", null),
    run("run_8830", 63, "wf_seo", "SEO Content Engine", "seo-content-engine", "canceled", "schedule", { name: "Maya Chen", initials: "MC" }, "2026-06-05T06:00:00Z", "2026-06-05T06:04:00Z", 0.21, 14000, 2200, 2, 12, "Canceled", null),
    run("run_8828", 1283, "wf_inbox", "Inbox Triage Autopilot", "inbox-triage-autopilot", "succeeded", "event", { name: "Webhook", initials: "WH" }, "2026-06-06T17:31:00Z", "2026-06-06T17:31:42Z", 0.07, 9100, 1600, 3, 3, "Completed", null),
    run("run_8825", 26, "wf_finance", "Invoice Reconciliation", "invoice-reconciliation", "succeeded", "schedule", { name: "Scheduler", initials: "SC" }, "2026-06-05T23:30:00Z", "2026-06-05T23:35:00Z", 0.58, 41000, 5200, 8, 8, "Completed", null),
  ];

  /* ---- logs (flagship) ---- */
  type RawLog = [number, LogLevel, LogChannel, string, string, string?];
  const rawLogs: RawLog[] = [
    [0, "info", "system", "scheduler", "Run #142 queued from schedule 'Mon, Thu · 09:00 UTC'"],
    [2, "info", "system", "runtime", "Provisioning sandbox · us-east-1 · 4 vCPU / 8 GiB"],
    [6, "success", "system", "runtime", "Sandbox ready in 3.9s — workspace mounted"],
    [9, "info", "agent", "Conductor", "Booting orchestrator with 6 downstream agents"],
    [12, "info", "model", "Conductor", "Planning sweep across 11 target competitors", "Reasoning: prioritize pricing + recent launches; parallelize scouts."],
    [40, "info", "agent", "Conductor", "→ Scout · Pricing  ·  scope=pricing, targets=11"],
    [52, "info", "agent", "Conductor", "→ Navigator ·  capture live product UIs"],
    [61, "info", "tool", "Scout · Pricing", "web.search('competitor pricing 2026')  · 11 results"],
    [96, "info", "browser", "Navigator", "session.open()  · viewport 1440×900 · us-east-1"],
    [120, "warn", "tool", "Scout · Launches", "rival.io returned 429 — backing off 8s, attempt 1/3"],
    [151, "info", "browser", "Navigator", "screenshot captured · acme-pricing.png (1.8 MB)"],
    [360, "error", "browser", "Navigator", "net::ERR_TIMED_OUT loading https://legacy-corp.com (30s)", "Host blocks datacenter IPs. Falling back to cached snapshot."],
    [430, "success", "agent", "Scout · Pricing", "Completed — 11/11 pricing rows extracted"],
    [612, "warn", "model", "Synthesizer", "Conflicting price for 'Nimbus Pro': $49 (page) vs $59 (cache)"],
    [1180, "success", "agent", "Synthesizer", "Matrix v1 ready · 99 cells, 3 flagged"],
    [1410, "warn", "agent", "Auditor", "Claim 14 lacks a primary source — requesting recapture"],
  ];
  const logs: LogEntry[] = rawLogs.map((r, i) => {
    const [offset, level, channel, source, message, detail] = r;
    return {
      id: brand<LogId>(`log_${i.toString().padStart(4, "0")}`),
      runId: RUN_8842,
      seq: i,
      ts: iso(offset),
      offsetMs: offset * 1000,
      level,
      channel,
      source,
      message,
      detail: detail ?? null,
      reasoning: channel === "model",
    };
  });

  /* ---- browser session ---- */
  const browserSessions: BrowserSession[] = [
    {
      id: BS_1,
      runId: RUN_8842,
      agentName: "Navigator",
      status: "running",
      currentUrl: "https://nimbus.ai/pricing",
      pageTitle: "Pricing — Nimbus AI",
      viewport: { width: 1440, height: 900 },
      pagesVisited: 14,
      actionsCount: 58,
      startedAt: iso(96),
      finishedAt: null,
    },
  ];

  const shot = (id: string, off: number, url: string, title: string, label: string): BrowserShot => ({
    id: brand<BrowserShotId>(id),
    sessionId: BS_1,
    ts: iso(off),
    url,
    title,
    label,
    storageKey: null,
  });
  const browserShots: BrowserShot[] = [
    shot("shot_1", 110, "https://acme.dev/product", "Acme · Product", "Acme product hero"),
    shot("shot_2", 151, "https://acme.dev/pricing", "Acme · Pricing", "Acme pricing grid"),
    shot("shot_3", 322, "https://nimbus.ai", "Nimbus · Home", "Nimbus landing"),
    shot("shot_4", 560, "https://nimbus.ai/pricing", "Nimbus · Pricing", "Nimbus pricing grid"),
    shot("shot_5", 965, "https://rival.io/product", "Rival · Product", "Rival product tour"),
  ];

  const action = (
    id: string,
    off: number,
    type: BrowserActionType,
    target: string,
    value: string | null,
    status: "ok" | "error",
    dur: number,
  ): BrowserAction => ({
    id: brand<BrowserActionId>(id),
    sessionId: BS_1,
    ts: iso(off),
    type,
    target,
    value,
    status,
    durationMs: dur,
  });
  const browserActions: BrowserAction[] = [
    action("ba_1", 96, "navigate", "https://acme.dev/product", null, "ok", 940),
    action("ba_2", 142, "click", "button[data-cta='see-pricing']", null, "ok", 120),
    action("ba_3", 151, "screenshot", "viewport", null, "ok", 310),
    action("ba_4", 360, "navigate", "https://legacy-corp.com", null, "error", 30000),
    action("ba_5", 560, "extract", "table.pricing-grid", "9 columns", "ok", 240),
    action("ba_6", 965, "screenshot", "viewport", "capture set ×24", "ok", 380),
  ];

  const consoleRows: [number, LogLevel, string][] = [
    [105, "info", "[page] DOMContentLoaded in 0.9s"],
    [360, "error", "GET https://legacy-corp.com net::ERR_TIMED_OUT"],
    [561, "info", "[extract] matched 1 table, 11 rows"],
  ];
  const browserConsole: { sessionId: BrowserSessionId; line: BrowserConsoleLine }[] = consoleRows.map(
    ([off, level, text]) => ({ sessionId: BS_1, line: { ts: iso(off), level, text } }),
  );

  /* ---- notifications ---- */
  const notif = (
    id: string,
    type: Notification["type"],
    severity: Notification["severity"],
    title: string,
    body: string,
    slug: string,
    runId: string,
    num: number,
    createdAt: string,
    readAt: string | null,
  ): Notification => ({
    id: brand<NotificationId>(id),
    workspaceId: WS,
    recipientId: brand<MemberId>("mem_maya"),
    type,
    severity,
    title,
    body,
    workflowSlug: slug,
    runId: brand<RunId>(runId),
    runNumber: num,
    createdAt,
    readAt,
  });
  const notifications: Notification[] = [
    notif("ntf_1", "cost.alert", "warning", "Cost threshold approaching", "Competitive Intelligence Sweep run #142 has used $4.21 of its $6.00 budget.", "competitive-intelligence-sweep", "run_8842", 142, "2026-06-06T17:40:00Z", null),
    notif("ntf_2", "browser.error", "warning", "Browser navigation failed", "Navigator hit net::ERR_TIMED_OUT on legacy-corp.com and fell back to a cached snapshot.", "competitive-intelligence-sweep", "run_8842", 142, "2026-06-06T17:15:12Z", null),
    notif("ntf_3", "workflow.failed", "error", "Workflow failed", "Candidate Sourcing Pipeline run #31 failed: browser session timed out at step 9/14.", "candidate-sourcing", "run_8833", 31, "2026-06-06T14:31:00Z", null),
    notif("ntf_4", "workflow.completed", "success", "Workflow completed", "Nightly Data QA run #117 finished successfully in 18m with 0 anomalies.", "nightly-data-qa", "run_8836", 117, "2026-06-06T02:18:00Z", "2026-06-06T08:00:00Z"),
    notif("ntf_5", "agent.blocked", "info", "Agent blocked", "Auditor requested a recapture — claim 14 in the executive brief lacks a primary source.", "competitive-intelligence-sweep", "run_8842", 142, "2026-06-06T17:33:00Z", "2026-06-06T17:34:00Z"),
    notif("ntf_6", "workflow.completed", "success", "Workflow completed", "Competitive Intelligence Sweep run #141 shipped the Q2 brief to 4 recipients.", "competitive-intelligence-sweep", "run_8839", 141, "2026-06-03T09:39:00Z", "2026-06-03T10:00:00Z"),
  ];

  /* ---- activity ---- */
  const act = (
    id: string,
    kind: ActivityEvent["kind"],
    actor: string,
    text: string,
    slug: string,
    runId: string | null,
    at: string,
  ): ActivityEvent => ({
    id: brand<ActivityId>(id),
    workspaceId: WS,
    kind,
    actor,
    text,
    workflowSlug: slug,
    runId: runId ? brand<RunId>(runId) : null,
    at,
  });
  const activity: ActivityEvent[] = [
    act("act_1", "run", "Scheduler", "started run #142 of Competitive Intelligence Sweep", "competitive-intelligence-sweep", "run_8842", "2026-06-06T17:09:12Z"),
    act("act_2", "complete", "Inbox Triage Autopilot", "completed run #1283 in 42s", "inbox-triage-autopilot", "run_8828", "2026-06-06T17:31:42Z"),
    act("act_3", "fail", "Candidate Sourcing Pipeline", "failed run #31 — browser timeout", "candidate-sourcing", "run_8833", "2026-06-06T14:31:00Z"),
    act("act_4", "deploy", "Maya Chen", "deployed v12 of Competitive Intelligence Sweep", "competitive-intelligence-sweep", null, "2026-06-06T08:55:00Z"),
    act("act_5", "scale", "Autoscaler", "scaled Inbox Triage to 3 concurrent sandboxes", "inbox-triage-autopilot", null, "2026-06-06T12:02:00Z"),
    act("act_6", "complete", "Nightly Data QA", "completed run #117 — 0 anomalies", "nightly-data-qa", "run_8836", "2026-06-06T02:18:00Z"),
    act("act_7", "comment", "Dev Patel", "paused SEO Content Engine pending CMS migration", "seo-content-engine", null, "2026-06-05T18:40:00Z"),
  ];

  /* ---- materialized stats ---- */
  const stat = (
    successRate: number,
    avgRuntimeMs: number,
    avgCostUsd: number,
    totalRuns: number,
    recent: (0 | 1)[],
    trend: number[],
  ): WorkflowStats => ({ successRate, avgRuntimeMs, avgCostUsd, totalRuns, recent, trend });

  const workflowStats: Record<string, WorkflowStats> = {
    wf_compint: stat(0.96, 2_340_000, 5.9, 48, [1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1], [3, 4, 2, 5, 6, 4, 7, 5, 8, 6, 7, 9, 8, 10]),
    wf_inbox: stat(0.991, 42_000, 0.08, 12840, [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1], [120, 140, 110, 160, 180, 150, 210, 190, 230, 250, 240, 280, 300, 320]),
    wf_dataqa: stat(0.92, 1_080_000, 1.4, 117, [1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1], [5, 6, 5, 7, 6, 8, 7, 6, 9, 8, 7, 8, 9, 8]),
    wf_recruit: stat(0.78, 1_620_000, 3.1, 31, [1, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 0, 1, 0], [2, 3, 2, 4, 3, 5, 4, 3, 6, 5, 4, 5, 6, 4]),
    wf_seo: stat(0.88, 960_000, 2.2, 64, [1, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1], [4, 5, 4, 6, 5, 7, 6, 8, 7, 6, 8, 7, 9, 8]),
    wf_finance: stat(0.95, 300_000, 0.6, 26, [1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1], [3, 4, 3, 5, 4, 6, 5, 4, 6, 5, 6, 7, 6, 7]),
  };

  const workspaceStats: WorkspaceStats = {
    activeRuns: 2,
    runsToday: 47,
    successRate: 0.962,
    spendTodayUsd: 38.42,
    spendBudgetUsd: 120,
    computeHours: 64.8,
    tokensToday: 4_820_000,
    runVolume: [2, 1, 1, 0, 1, 2, 3, 5, 8, 11, 9, 12, 14, 10, 13, 9, 11, 7, 6, 4, 3, 5, 2, 3],
    spendSeries: [0.4, 0.2, 0.3, 0.1, 0.2, 0.6, 0.9, 1.4, 2.1, 3.0, 2.4, 3.2, 3.8, 2.7, 3.4, 2.2, 2.9, 1.8, 1.5, 1.0, 0.7, 1.2, 0.5, 0.6],
  };

  const data: MemoryData = {
    workspace,
    members,
    agents,
    workflows,
    versions,
    runs,
    runAgents,
    messages,
    artifacts,
    logs,
    browserSessions,
    browserActions,
    browserShots,
    browserConsole,
    notifications,
    activity,
  };

  return { data, workflowStats, workspaceStats };
}

export { NOW_ISO };
