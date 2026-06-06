import type {
  Workflow,
  WorkflowRun,
  AgentNode,
  AgentMessage,
  LogEntry,
  BrowserSession,
  AppNotification,
  ActivityItem,
  RunArtifact,
  LogLevel,
  LogChannel,
} from "./types";

/* ------------------------------------------------------------------ *
   Time helpers — everything is anchored to a fixed run start so the
   dataset renders deterministically on server and client.
 * ------------------------------------------------------------------ */
const RUN_START = Date.parse("2026-06-06T17:09:12Z");
const iso = (offsetSec: number) => new Date(RUN_START + offsetSec * 1000).toISOString();

/* ================================================================== *
   FLAGSHIP WORKFLOW: Competitive Intelligence Sweep
 * ================================================================== */

const flagshipAgents: AgentNode[] = [
  {
    id: "a-conductor",
    name: "Conductor",
    role: "orchestrator",
    model: "claude-opus-4-8",
    status: "running",
    dependsOn: [],
    col: 0,
    row: 1,
    summary: "Plans the sweep, delegates to scouts, and gates the final report.",
    metrics: { tokens: 48210, costUsd: 0.96, runtimeMs: 1_980_000, toolCalls: 22, progress: 0.7 },
  },
  {
    id: "a-scout-a",
    name: "Scout · Pricing",
    role: "researcher",
    model: "claude-sonnet-4-6",
    status: "succeeded",
    dependsOn: ["a-conductor"],
    col: 1,
    row: 0,
    summary: "Collected pricing & packaging for 11 competitors.",
    metrics: { tokens: 92430, costUsd: 0.71, runtimeMs: 612_000, toolCalls: 41, progress: 1 },
  },
  {
    id: "a-scout-b",
    name: "Scout · Launches",
    role: "researcher",
    model: "claude-sonnet-4-6",
    status: "running",
    dependsOn: ["a-conductor"],
    col: 1,
    row: 1,
    summary: "Tracking changelogs, blog posts and release notes.",
    metrics: { tokens: 64120, costUsd: 0.49, runtimeMs: 540_000, toolCalls: 33, progress: 0.62 },
  },
  {
    id: "a-navigator",
    name: "Navigator",
    role: "browser",
    model: "claude-sonnet-4-6",
    status: "running",
    dependsOn: ["a-conductor"],
    col: 1,
    row: 2,
    summary: "Drives a headless browser to capture live product UIs.",
    metrics: { tokens: 38800, costUsd: 0.33, runtimeMs: 498_000, toolCalls: 58, progress: 0.55 },
  },
  {
    id: "a-synth",
    name: "Synthesizer",
    role: "analyst",
    model: "claude-opus-4-8",
    status: "waiting",
    dependsOn: ["a-scout-a", "a-scout-b", "a-navigator"],
    col: 2,
    row: 1,
    summary: "Reconciles signals into a structured competitive matrix.",
    metrics: { tokens: 12010, costUsd: 0.28, runtimeMs: null, toolCalls: 4, progress: 0.1 },
  },
  {
    id: "a-composer",
    name: "Composer",
    role: "writer",
    model: "claude-opus-4-8",
    status: "idle",
    dependsOn: ["a-synth"],
    col: 3,
    row: 1,
    summary: "Writes the executive brief and one-pager.",
    metrics: { tokens: 0, costUsd: 0, runtimeMs: null, toolCalls: 0, progress: 0 },
  },
  {
    id: "a-auditor",
    name: "Auditor",
    role: "validator",
    model: "claude-sonnet-4-6",
    status: "idle",
    dependsOn: ["a-composer"],
    col: 4,
    row: 1,
    summary: "Fact-checks every claim against captured sources.",
    metrics: { tokens: 0, costUsd: 0, runtimeMs: null, toolCalls: 0, progress: 0 },
  },
];

const flagshipMessages: AgentMessage[] = [
  { id: "m1", from: "a-conductor", to: "a-scout-a", label: "scope: pricing", at: iso(40) },
  { id: "m2", from: "a-conductor", to: "a-scout-b", label: "scope: launches", at: iso(44) },
  { id: "m3", from: "a-conductor", to: "a-navigator", label: "capture UIs", at: iso(52) },
  { id: "m4", from: "a-scout-a", to: "a-synth", label: "11 pricing rows", at: iso(640) },
  { id: "m5", from: "a-scout-b", to: "a-synth", label: "partial: 18 items", at: iso(900) },
  { id: "m6", from: "a-navigator", to: "a-synth", label: "24 screenshots", at: iso(960) },
  { id: "m7", from: "a-synth", to: "a-composer", label: "matrix v1", at: iso(1180) },
  { id: "m8", from: "a-composer", to: "a-auditor", label: "draft brief", at: iso(1320) },
];

const flagshipArtifacts: RunArtifact[] = [
  { id: "art-1", name: "competitive-matrix.json", kind: "json", sizeBytes: 48213, producedBy: "Synthesizer", at: iso(1190), preview: '{ "competitors": 11, "dimensions": 9 }' },
  { id: "art-2", name: "pricing-table.csv", kind: "dataset", sizeBytes: 8841, producedBy: "Scout · Pricing", at: iso(648) },
  { id: "art-3", name: "executive-brief.md", kind: "report", sizeBytes: 14920, producedBy: "Composer", at: iso(1330), preview: "# Competitive Landscape — Q2 2026" },
  { id: "art-4", name: "navigator-capture-24.png", kind: "image", sizeBytes: 1843200, producedBy: "Navigator", at: iso(965) },
  { id: "art-5", name: "sources.json", kind: "json", sizeBytes: 22104, producedBy: "Scout · Launches", at: iso(905) },
  { id: "art-6", name: "scrape-runner.ts", kind: "code", sizeBytes: 3120, producedBy: "Navigator", at: iso(120) },
];

export const flagshipRun: WorkflowRun = {
  id: "run-8842",
  number: 142,
  workflowId: "wf-compint",
  workflowName: "Competitive Intelligence Sweep",
  workflowSlug: "competitive-intelligence-sweep",
  status: "running",
  trigger: "schedule",
  triggeredBy: { name: "Scheduler", initials: "SC" },
  region: "us-east-1",
  queuedAt: iso(-6),
  startedAt: iso(0),
  finishedAt: null,
  costUsd: 4.21,
  tokensIn: 268400,
  tokensOut: 41280,
  steps: { done: 19, total: 27 },
  currentStep: "Synthesizing competitive matrix",
  agents: flagshipAgents,
  messages: flagshipMessages,
  artifacts: flagshipArtifacts,
  browserSessionId: "bs-1",
};

/* ================================================================== *
   WORKFLOWS (list)
 * ================================================================== */

export const workflows: Workflow[] = [
  {
    id: "wf-compint",
    slug: "competitive-intelligence-sweep",
    name: "Competitive Intelligence Sweep",
    description:
      "A 7-agent crew that researches competitors, captures live product UIs, and ships an executive brief twice a week.",
    status: "running",
    agentCount: 7,
    trigger: "schedule",
    schedule: "Mon, Thu · 09:00 UTC",
    owner: { name: "Maya Chen", initials: "MC" },
    tags: ["research", "browser", "report"],
    createdAt: "2026-03-02T10:00:00Z",
    lastRunAt: iso(0),
    stats: {
      successRate: 0.96,
      avgRuntimeMs: 2_340_000,
      avgCostUsd: 5.9,
      totalRuns: 48,
      recent: [1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1],
      trend: [3, 4, 2, 5, 6, 4, 7, 5, 8, 6, 7, 9, 8, 10],
    },
  },
  {
    id: "wf-inbox",
    slug: "inbox-triage-autopilot",
    name: "Inbox Triage Autopilot",
    description:
      "Continuously classifies, drafts, and routes incoming support email; escalates anything ambiguous to a human.",
    status: "running",
    agentCount: 3,
    trigger: "event",
    owner: { name: "Dev Patel", initials: "DP" },
    tags: ["support", "email"],
    createdAt: "2026-01-18T10:00:00Z",
    lastRunAt: "2026-06-06T17:38:00Z",
    stats: {
      successRate: 0.991,
      avgRuntimeMs: 42_000,
      avgCostUsd: 0.08,
      totalRuns: 12840,
      recent: [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1],
      trend: [120, 140, 110, 160, 180, 150, 210, 190, 230, 250, 240, 280, 300, 320],
    },
  },
  {
    id: "wf-dataqa",
    slug: "nightly-data-qa",
    name: "Nightly Data QA",
    description:
      "Runs schema, freshness and anomaly checks across the warehouse, then opens issues for anything that drifts.",
    status: "succeeded",
    agentCount: 4,
    trigger: "schedule",
    schedule: "Daily · 02:00 UTC",
    owner: { name: "Lena Ortiz", initials: "LO" },
    tags: ["data", "qa", "monitoring"],
    createdAt: "2026-02-09T10:00:00Z",
    lastRunAt: "2026-06-06T02:00:00Z",
    stats: {
      successRate: 0.92,
      avgRuntimeMs: 1_080_000,
      avgCostUsd: 1.4,
      totalRuns: 117,
      recent: [1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1],
      trend: [5, 6, 5, 7, 6, 8, 7, 6, 9, 8, 7, 8, 9, 8],
    },
  },
  {
    id: "wf-recruit",
    slug: "candidate-sourcing",
    name: "Candidate Sourcing Pipeline",
    description:
      "Sources, screens and ranks candidates from public profiles, then prepares outreach drafts for review.",
    status: "failed",
    agentCount: 5,
    trigger: "manual",
    owner: { name: "Sam Idowu", initials: "SI" },
    tags: ["recruiting", "browser"],
    createdAt: "2026-04-22T10:00:00Z",
    lastRunAt: "2026-06-06T14:12:00Z",
    stats: {
      successRate: 0.78,
      avgRuntimeMs: 1_620_000,
      avgCostUsd: 3.1,
      totalRuns: 31,
      recent: [1, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 0, 1, 0],
      trend: [2, 3, 2, 4, 3, 5, 4, 3, 6, 5, 4, 5, 6, 4],
    },
  },
  {
    id: "wf-seo",
    slug: "seo-content-engine",
    name: "SEO Content Engine",
    description:
      "Researches keywords, drafts long-form articles, and stages them in the CMS with internal links.",
    status: "paused",
    agentCount: 4,
    trigger: "schedule",
    schedule: "Weekdays · 06:00 UTC",
    owner: { name: "Maya Chen", initials: "MC" },
    tags: ["content", "seo"],
    createdAt: "2026-03-30T10:00:00Z",
    lastRunAt: "2026-06-05T06:00:00Z",
    stats: {
      successRate: 0.88,
      avgRuntimeMs: 960_000,
      avgCostUsd: 2.2,
      totalRuns: 64,
      recent: [1, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1],
      trend: [4, 5, 4, 6, 5, 7, 6, 8, 7, 6, 8, 7, 9, 8],
    },
  },
  {
    id: "wf-finance",
    slug: "invoice-reconciliation",
    name: "Invoice Reconciliation",
    description:
      "Matches invoices to POs and ledger entries, flags mismatches, and prepares a daily reconciliation report.",
    status: "queued",
    agentCount: 3,
    trigger: "schedule",
    schedule: "Daily · 23:30 UTC",
    owner: { name: "Dev Patel", initials: "DP" },
    tags: ["finance", "ops"],
    createdAt: "2026-05-11T10:00:00Z",
    lastRunAt: "2026-06-05T23:30:00Z",
    stats: {
      successRate: 0.95,
      avgRuntimeMs: 300_000,
      avgCostUsd: 0.6,
      totalRuns: 26,
      recent: [1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1],
      trend: [3, 4, 3, 5, 4, 6, 5, 4, 6, 5, 6, 7, 6, 7],
    },
  },
];

/* ================================================================== *
   RUNS (list) — a feed across workflows
 * ================================================================== */

function lite(run: Partial<WorkflowRun>): WorkflowRun {
  return {
    id: "",
    number: 0,
    workflowId: "",
    workflowName: "",
    workflowSlug: "",
    status: "succeeded",
    trigger: "manual",
    triggeredBy: { name: "Maya Chen", initials: "MC" },
    region: "us-east-1",
    queuedAt: "2026-06-06T16:00:00Z",
    startedAt: "2026-06-06T16:00:00Z",
    finishedAt: "2026-06-06T16:20:00Z",
    costUsd: 1,
    tokensIn: 1000,
    tokensOut: 200,
    steps: { done: 1, total: 1 },
    currentStep: "",
    agents: [],
    messages: [],
    artifacts: [],
    ...run,
  };
}

export const runs: WorkflowRun[] = [
  flagshipRun,
  lite({
    id: "run-8841",
    number: 1284,
    workflowId: "wf-inbox",
    workflowName: "Inbox Triage Autopilot",
    workflowSlug: "inbox-triage-autopilot",
    status: "running",
    trigger: "event",
    triggeredBy: { name: "Webhook", initials: "WH" },
    startedAt: "2026-06-06T17:38:00Z",
    finishedAt: null,
    costUsd: 0.04,
    tokensIn: 8200,
    tokensOut: 1400,
    steps: { done: 2, total: 3 },
    currentStep: "Drafting reply",
  }),
  lite({
    id: "run-8839",
    number: 141,
    workflowId: "wf-compint",
    workflowName: "Competitive Intelligence Sweep",
    workflowSlug: "competitive-intelligence-sweep",
    status: "succeeded",
    trigger: "schedule",
    triggeredBy: { name: "Scheduler", initials: "SC" },
    startedAt: "2026-06-03T09:00:00Z",
    finishedAt: "2026-06-03T09:39:00Z",
    costUsd: 5.74,
    tokensIn: 251000,
    tokensOut: 39800,
    steps: { done: 27, total: 27 },
  }),
  lite({
    id: "run-8836",
    number: 117,
    workflowId: "wf-dataqa",
    workflowName: "Nightly Data QA",
    workflowSlug: "nightly-data-qa",
    status: "succeeded",
    trigger: "schedule",
    triggeredBy: { name: "Scheduler", initials: "SC" },
    startedAt: "2026-06-06T02:00:00Z",
    finishedAt: "2026-06-06T02:18:00Z",
    costUsd: 1.32,
    tokensIn: 94000,
    tokensOut: 12100,
    steps: { done: 14, total: 14 },
  }),
  lite({
    id: "run-8833",
    number: 31,
    workflowId: "wf-recruit",
    workflowName: "Candidate Sourcing Pipeline",
    workflowSlug: "candidate-sourcing",
    status: "failed",
    trigger: "manual",
    triggeredBy: { name: "Sam Idowu", initials: "SI" },
    startedAt: "2026-06-06T14:12:00Z",
    finishedAt: "2026-06-06T14:31:00Z",
    costUsd: 2.18,
    tokensIn: 142000,
    tokensOut: 18400,
    steps: { done: 9, total: 14 },
    currentStep: "Browser session timed out",
  }),
  lite({
    id: "run-8830",
    number: 63,
    workflowId: "wf-seo",
    workflowName: "SEO Content Engine",
    workflowSlug: "seo-content-engine",
    status: "canceled",
    trigger: "schedule",
    triggeredBy: { name: "Maya Chen", initials: "MC" },
    startedAt: "2026-06-05T06:00:00Z",
    finishedAt: "2026-06-05T06:04:00Z",
    costUsd: 0.21,
    tokensIn: 14000,
    tokensOut: 2200,
    steps: { done: 2, total: 12 },
  }),
  lite({
    id: "run-8828",
    number: 1283,
    workflowId: "wf-inbox",
    workflowName: "Inbox Triage Autopilot",
    workflowSlug: "inbox-triage-autopilot",
    status: "succeeded",
    trigger: "event",
    triggeredBy: { name: "Webhook", initials: "WH" },
    startedAt: "2026-06-06T17:31:00Z",
    finishedAt: "2026-06-06T17:31:42Z",
    costUsd: 0.07,
    tokensIn: 9100,
    tokensOut: 1600,
    steps: { done: 3, total: 3 },
  }),
  lite({
    id: "run-8825",
    number: 26,
    workflowId: "wf-finance",
    workflowName: "Invoice Reconciliation",
    workflowSlug: "invoice-reconciliation",
    status: "succeeded",
    trigger: "schedule",
    triggeredBy: { name: "Scheduler", initials: "SC" },
    startedAt: "2026-06-05T23:30:00Z",
    finishedAt: "2026-06-05T23:35:00Z",
    costUsd: 0.58,
    tokensIn: 41000,
    tokensOut: 5200,
    steps: { done: 8, total: 8 },
  }),
];

/* ================================================================== *
   LOGS — generated for the flagship run (rich enough for the viewer)
 * ================================================================== */

type RawLog = [offset: number, level: LogLevel, channel: LogChannel, source: string, message: string, detail?: string];

const rawLogs: RawLog[] = [
  [0, "info", "system", "scheduler", "Run #142 queued from schedule 'Mon, Thu · 09:00 UTC'"],
  [2, "info", "system", "runtime", "Provisioning sandbox · us-east-1 · 4 vCPU / 8 GiB"],
  [6, "success", "system", "runtime", "Sandbox ready in 3.9s — workspace mounted"],
  [9, "info", "agent", "Conductor", "Booting orchestrator with 6 downstream agents"],
  [12, "info", "model", "Conductor", "Planning sweep across 11 target competitors", "Reasoning: prioritize pricing + recent launches; parallelize scouts; reserve Navigator for live UI capture where docs are thin."],
  [40, "info", "agent", "Conductor", "→ Scout · Pricing  ·  scope=pricing, targets=11"],
  [44, "info", "agent", "Conductor", "→ Scout · Launches ·  scope=changelogs, window=90d"],
  [52, "info", "agent", "Conductor", "→ Navigator ·  capture live product UIs"],
  [61, "info", "tool", "Scout · Pricing", "web.search('competitor pricing 2026')  · 11 results"],
  [73, "debug", "tool", "Scout · Pricing", "fetch https://acme.dev/pricing  · 200 · 84ms"],
  [88, "debug", "tool", "Scout · Launches", "fetch https://rival.io/changelog  · 200 · 121ms"],
  [96, "info", "browser", "Navigator", "session.open()  · viewport 1440×900 · us-east-1"],
  [104, "info", "browser", "Navigator", "navigate → https://acme.dev/product"],
  [120, "warn", "tool", "Scout · Launches", "rival.io returned 429 — backing off 8s, attempt 1/3"],
  [142, "debug", "browser", "Navigator", "click  button[data-cta='see-pricing']"],
  [151, "info", "browser", "Navigator", "screenshot captured · acme-pricing.png (1.8 MB)"],
  [168, "info", "model", "Scout · Pricing", "Extracting structured pricing rows", "tier, seats, monthly, annual, overage — 11 vendors → 11 rows reconciled"],
  [205, "debug", "tool", "Scout · Pricing", "fetch https://nimbus.ai/pricing · 200 · 67ms"],
  [240, "info", "tool", "Scout · Launches", "rss.poll(8 feeds) · 18 new items in window"],
  [288, "warn", "browser", "Navigator", "cookie consent overlay intercepted click — dismissing"],
  [322, "debug", "browser", "Navigator", "navigate → https://nimbus.ai  · domcontentloaded 0.9s"],
  [360, "error", "browser", "Navigator", "net::ERR_TIMED_OUT loading https://legacy-corp.com (30s)", "Host appears to block datacenter IPs. Falling back to cached snapshot from 2026-06-02."],
  [388, "info", "browser", "Navigator", "fallback → cached snapshot legacy-corp.com (4d old)"],
  [430, "success", "agent", "Scout · Pricing", "Completed — 11/11 pricing rows extracted"],
  [432, "info", "agent", "Scout · Pricing", "→ Synthesizer  · artifact pricing-table.csv"],
  [468, "info", "model", "Scout · Launches", "Clustering 18 launch items into 6 themes"],
  [520, "debug", "tool", "Scout · Launches", "fetch https://rival.io/blog/2026-q2 · 200 · 142ms"],
  [560, "info", "browser", "Navigator", "extract  table.pricing-grid → 9 columns"],
  [612, "warn", "model", "Synthesizer", "Conflicting price for 'Nimbus Pro': $49 (page) vs $59 (cache)", "Preferring live page value; flagging for Auditor review."],
  [648, "success", "tool", "Scout · Pricing", "artifact written · pricing-table.csv (8.6 KB)"],
  [720, "info", "agent", "Conductor", "checkpoint · 2/3 scouts complete, Navigator 55%"],
  [815, "debug", "browser", "Navigator", "scroll → viewport bottom · lazy images loaded"],
  [905, "success", "tool", "Scout · Launches", "artifact written · sources.json (22 KB)"],
  [930, "info", "agent", "Scout · Launches", "→ Synthesizer · 18 launch items, 6 themes"],
  [965, "info", "browser", "Navigator", "24 screenshots captured · bundling capture set"],
  [1010, "info", "agent", "Synthesizer", "Building competitive matrix · 11 vendors × 9 dims"],
  [1090, "info", "model", "Synthesizer", "Resolving 3 cell conflicts via source recency", "All conflicts attributed to stale cache; live values win."],
  [1180, "success", "agent", "Synthesizer", "Matrix v1 ready · 99 cells, 3 flagged"],
  [1182, "info", "agent", "Synthesizer", "→ Composer · matrix v1"],
  [1240, "info", "agent", "Composer", "Drafting executive brief · target 900 words"],
  [1320, "info", "agent", "Composer", "→ Auditor · draft brief for fact-check"],
  [1380, "info", "agent", "Auditor", "Verifying 22 claims against captured sources"],
  [1410, "warn", "agent", "Auditor", "Claim 14 lacks a primary source — requesting recapture"],
  [1440, "info", "agent", "Conductor", "Synthesizer waiting on Navigator (55%) — holding gate"],
];

export const flagshipLogs: LogEntry[] = rawLogs.map((r, i) => {
  const [offset, level, channel, source, message, detail] = r;
  return {
    id: `log-${i}`,
    runId: flagshipRun.id,
    seq: i,
    ts: iso(offset),
    offsetMs: offset * 1000,
    level,
    channel,
    source,
    message,
    detail,
    reasoning: channel === "model",
  };
});

/* ================================================================== *
   BROWSER SESSION
 * ================================================================== */

const shotTones = [
  "from-indigo-500 via-violet-500 to-sky-500",
  "from-emerald-500 via-teal-500 to-cyan-500",
  "from-rose-500 via-pink-500 to-orange-500",
  "from-amber-500 via-yellow-400 to-lime-500",
  "from-sky-500 via-blue-500 to-indigo-600",
  "from-fuchsia-500 via-purple-500 to-violet-600",
];

export const browserSession: BrowserSession = {
  id: "bs-1",
  runId: flagshipRun.id,
  agentName: "Navigator",
  status: "running",
  currentUrl: "https://nimbus.ai/pricing",
  pageTitle: "Pricing — Nimbus AI",
  viewport: { w: 1440, h: 900 },
  startedAt: iso(96),
  pagesVisited: 14,
  actionsCount: 58,
  shots: [
    { id: "s1", ts: iso(110), url: "https://acme.dev/product", title: "Acme · Product", tone: shotTones[0]!, label: "Acme product hero" },
    { id: "s2", ts: iso(151), url: "https://acme.dev/pricing", title: "Acme · Pricing", tone: shotTones[1]!, label: "Acme pricing grid" },
    { id: "s3", ts: iso(322), url: "https://nimbus.ai", title: "Nimbus · Home", tone: shotTones[2]!, label: "Nimbus landing" },
    { id: "s4", ts: iso(388), url: "https://legacy-corp.com", title: "Cached snapshot", tone: shotTones[3]!, label: "Legacy Corp (cached)" },
    { id: "s5", ts: iso(560), url: "https://nimbus.ai/pricing", title: "Nimbus · Pricing", tone: shotTones[4]!, label: "Nimbus pricing grid" },
    { id: "s6", ts: iso(965), url: "https://rival.io/product", title: "Rival · Product", tone: shotTones[5]!, label: "Rival product tour" },
  ],
  actions: [
    { id: "ba1", ts: iso(96), type: "navigate", target: "https://acme.dev/product", status: "ok", durationMs: 940 },
    { id: "ba2", ts: iso(142), type: "click", target: "button[data-cta='see-pricing']", status: "ok", durationMs: 120 },
    { id: "ba3", ts: iso(151), type: "screenshot", target: "viewport", status: "ok", durationMs: 310 },
    { id: "ba4", ts: iso(288), type: "click", target: ".cookie-accept", value: "dismiss overlay", status: "ok", durationMs: 90 },
    { id: "ba5", ts: iso(322), type: "navigate", target: "https://nimbus.ai", status: "ok", durationMs: 900 },
    { id: "ba6", ts: iso(360), type: "navigate", target: "https://legacy-corp.com", status: "error", durationMs: 30000 },
    { id: "ba7", ts: iso(560), type: "extract", target: "table.pricing-grid", value: "9 columns", status: "ok", durationMs: 240 },
    { id: "ba8", ts: iso(815), type: "scroll", target: "viewport-bottom", status: "ok", durationMs: 60 },
    { id: "ba9", ts: iso(965), type: "screenshot", target: "viewport", value: "capture set ×24", status: "ok", durationMs: 380 },
  ],
  console: [
    { ts: iso(105), level: "info", text: "[page] DOMContentLoaded in 0.9s" },
    { ts: iso(289), level: "warn", text: "[page] consent overlay intercepted pointer events" },
    { ts: iso(360), level: "error", text: "GET https://legacy-corp.com net::ERR_TIMED_OUT" },
    { ts: iso(561), level: "info", text: "[extract] matched 1 table, 11 rows" },
    { ts: iso(815), level: "debug", text: "[page] 18 lazy images entered viewport" },
  ],
};

/* ================================================================== *
   NOTIFICATIONS
 * ================================================================== */

export const notifications: AppNotification[] = [
  {
    id: "n1",
    type: "cost.alert",
    title: "Cost threshold approaching",
    body: "Competitive Intelligence Sweep run #142 has used $4.21 of its $6.00 budget.",
    at: "2026-06-06T17:40:00Z",
    read: false,
    workflowSlug: "competitive-intelligence-sweep",
    runNumber: 142,
  },
  {
    id: "n2",
    type: "browser.error",
    title: "Browser navigation failed",
    body: "Navigator hit net::ERR_TIMED_OUT on legacy-corp.com and fell back to a cached snapshot.",
    at: "2026-06-06T17:15:12Z",
    read: false,
    workflowSlug: "competitive-intelligence-sweep",
    runNumber: 142,
  },
  {
    id: "n3",
    type: "workflow.failed",
    title: "Workflow failed",
    body: "Candidate Sourcing Pipeline run #31 failed: browser session timed out at step 9/14.",
    at: "2026-06-06T14:31:00Z",
    read: false,
    workflowSlug: "candidate-sourcing",
    runNumber: 31,
  },
  {
    id: "n4",
    type: "workflow.completed",
    title: "Workflow completed",
    body: "Nightly Data QA run #117 finished successfully in 18m with 0 anomalies.",
    at: "2026-06-06T02:18:00Z",
    read: true,
    workflowSlug: "nightly-data-qa",
    runNumber: 117,
  },
  {
    id: "n5",
    type: "agent.blocked",
    title: "Agent blocked",
    body: "Auditor requested a recapture — claim 14 in the executive brief lacks a primary source.",
    at: "2026-06-06T17:33:00Z",
    read: true,
    workflowSlug: "competitive-intelligence-sweep",
    runNumber: 142,
  },
  {
    id: "n6",
    type: "workflow.completed",
    title: "Workflow completed",
    body: "Competitive Intelligence Sweep run #141 shipped the Q2 brief to 4 recipients.",
    at: "2026-06-03T09:39:00Z",
    read: true,
    workflowSlug: "competitive-intelligence-sweep",
    runNumber: 141,
  },
];

/* ================================================================== *
   ACTIVITY FEED
 * ================================================================== */

export const activity: ActivityItem[] = [
  { id: "ac1", kind: "run", actor: "Scheduler", text: "started run #142 of Competitive Intelligence Sweep", at: "2026-06-06T17:09:12Z", workflowSlug: "competitive-intelligence-sweep" },
  { id: "ac2", kind: "complete", actor: "Inbox Triage Autopilot", text: "completed run #1283 in 42s", at: "2026-06-06T17:31:42Z", workflowSlug: "inbox-triage-autopilot" },
  { id: "ac3", kind: "fail", actor: "Candidate Sourcing Pipeline", text: "failed run #31 — browser timeout", at: "2026-06-06T14:31:00Z", workflowSlug: "candidate-sourcing" },
  { id: "ac4", kind: "deploy", actor: "Maya Chen", text: "deployed v12 of Competitive Intelligence Sweep", at: "2026-06-06T08:55:00Z", workflowSlug: "competitive-intelligence-sweep" },
  { id: "ac5", kind: "scale", actor: "Autoscaler", text: "scaled Inbox Triage to 3 concurrent sandboxes", at: "2026-06-06T12:02:00Z", workflowSlug: "inbox-triage-autopilot" },
  { id: "ac6", kind: "complete", actor: "Nightly Data QA", text: "completed run #117 — 0 anomalies", at: "2026-06-06T02:18:00Z", workflowSlug: "nightly-data-qa" },
  { id: "ac7", kind: "comment", actor: "Dev Patel", text: "paused SEO Content Engine pending CMS migration", at: "2026-06-05T18:40:00Z", workflowSlug: "seo-content-engine" },
];

/* ================================================================== *
   DASHBOARD AGGREGATES
 * ================================================================== */

export const dashboardStats = {
  activeRuns: runs.filter((r) => r.status === "running").length,
  runsToday: 47,
  successRate: 0.962,
  spendToday: 38.42,
  spendBudget: 120,
  computeHours: 64.8,
  /** 24h hourly run volume for the hero chart. */
  runVolume: [
    2, 1, 1, 0, 1, 2, 3, 5, 8, 11, 9, 12, 14, 10, 13, 9, 11, 7, 6, 4, 3, 5, 2, 3,
  ],
  /** 24h hourly spend ($) for the cost chart. */
  spendSeries: [
    0.4, 0.2, 0.3, 0.1, 0.2, 0.6, 0.9, 1.4, 2.1, 3.0, 2.4, 3.2, 3.8, 2.7, 3.4,
    2.2, 2.9, 1.8, 1.5, 1.0, 0.7, 1.2, 0.5, 0.6,
  ],
  tokensToday: 4_820_000,
};

/* ================================================================== *
   AGENT LIBRARY — reusable agent definitions across workflows
 * ================================================================== */

import type { AgentRole, AgentStatus } from "./types";

export interface LibraryAgent {
  id: string;
  name: string;
  role: AgentRole;
  model: string;
  status: AgentStatus;
  workflows: string[];
  description: string;
  runs7d: number;
  avgCostUsd: number;
  tools: string[];
}

export const agentLibrary: LibraryAgent[] = [
  { id: "lib-conductor", name: "Conductor", role: "orchestrator", model: "claude-opus-4-8", status: "running", workflows: ["Competitive Intelligence Sweep"], description: "Plans multi-agent sweeps and gates final output.", runs7d: 14, avgCostUsd: 0.94, tools: ["plan", "delegate", "review"] },
  { id: "lib-scout", name: "Scout", role: "researcher", model: "claude-sonnet-4-6", status: "running", workflows: ["Competitive Intelligence Sweep", "SEO Content Engine"], description: "Open-web research with structured extraction.", runs7d: 62, avgCostUsd: 0.58, tools: ["web.search", "fetch", "rss.poll"] },
  { id: "lib-navigator", name: "Navigator", role: "browser", model: "claude-sonnet-4-6", status: "running", workflows: ["Competitive Intelligence Sweep", "Candidate Sourcing Pipeline"], description: "Drives a headless browser to capture live UIs.", runs7d: 41, avgCostUsd: 0.33, tools: ["browser.navigate", "browser.click", "screenshot"] },
  { id: "lib-synth", name: "Synthesizer", role: "analyst", model: "claude-opus-4-8", status: "waiting", workflows: ["Competitive Intelligence Sweep", "Nightly Data QA"], description: "Reconciles signals into structured matrices.", runs7d: 23, avgCostUsd: 0.41, tools: ["sql", "reconcile", "chart"] },
  { id: "lib-composer", name: "Composer", role: "writer", model: "claude-opus-4-8", status: "idle", workflows: ["Competitive Intelligence Sweep", "SEO Content Engine"], description: "Long-form writing from structured inputs.", runs7d: 18, avgCostUsd: 0.62, tools: ["write", "cite", "format"] },
  { id: "lib-auditor", name: "Auditor", role: "validator", model: "claude-sonnet-4-6", status: "idle", workflows: ["Competitive Intelligence Sweep", "Invoice Reconciliation"], description: "Fact-checks claims against captured sources.", runs7d: 31, avgCostUsd: 0.22, tools: ["verify", "diff", "flag"] },
  { id: "lib-triage", name: "Triage", role: "analyst", model: "claude-haiku-4-5", status: "running", workflows: ["Inbox Triage Autopilot"], description: "Classifies and routes inbound support email.", runs7d: 1840, avgCostUsd: 0.02, tools: ["classify", "route", "draft"] },
  { id: "lib-coder", name: "Patcher", role: "coder", model: "claude-opus-4-8", status: "idle", workflows: ["Nightly Data QA"], description: "Writes and runs small fix-up scripts.", runs7d: 9, avgCostUsd: 0.71, tools: ["python", "shell", "test"] },
];

/* ================================================================== *
   GRAPH SYNTHESIS — gives every workflow a plausible agent graph
 * ================================================================== */

const ROLE_CYCLE: AgentRole[] = ["orchestrator", "researcher", "browser", "analyst", "writer", "validator", "coder"];

const GENERIC_NAMES: Record<AgentRole, string> = {
  orchestrator: "Coordinator",
  researcher: "Researcher",
  browser: "Navigator",
  analyst: "Analyst",
  writer: "Writer",
  validator: "Validator",
  coder: "Engineer",
};

export function agentsForWorkflow(wf: Workflow): AgentNode[] {
  if (wf.id === "wf-compint") return flagshipAgents;

  const n = wf.agentCount;
  const out: AgentNode[] = [];
  // Status distribution derived from the workflow's own status.
  const statusFor = (i: number): AgentStatus => {
    if (wf.status === "succeeded") return "succeeded";
    if (wf.status === "failed") return i < n - 1 ? "succeeded" : "failed";
    if (wf.status === "paused" || wf.status === "queued") return "idle";
    // running
    if (i === 0) return "running";
    if (i < Math.ceil(n / 2)) return "succeeded";
    if (i === Math.ceil(n / 2)) return "running";
    return "idle";
  };

  // Orchestrator in col 0; remaining fan into col 1, converge to col 2.
  for (let i = 0; i < n; i++) {
    const role = i === 0 ? "orchestrator" : ROLE_CYCLE[((i - 1) % (ROLE_CYCLE.length - 1)) + 1]!;
    const st = statusFor(i);
    const col = i === 0 ? 0 : i === n - 1 ? 2 : 1;
    const row = i === 0 ? Math.floor((n - 2) / 2) : i === n - 1 ? Math.floor((n - 2) / 2) : i - 1;
    const dependsOn =
      i === 0 ? [] : i === n - 1 ? out.filter((a) => a.col === 1).map((a) => a.id) : [out[0]!.id];
    out.push({
      id: `${wf.id}-a${i}`,
      name: GENERIC_NAMES[role] + (i > 0 && role !== "orchestrator" ? ` ${i}` : ""),
      role,
      model: i % 2 === 0 ? "claude-opus-4-8" : "claude-sonnet-4-6",
      status: st,
      dependsOn,
      col,
      row,
      summary: `${GENERIC_NAMES[role]} step in ${wf.name}.`,
      metrics: {
        tokens: st === "idle" ? 0 : 20000 + i * 9000,
        costUsd: st === "idle" ? 0 : 0.2 + i * 0.18,
        runtimeMs: st === "idle" ? null : 120000 + i * 60000,
        toolCalls: st === "idle" ? 0 : 6 + i * 4,
        progress: st === "succeeded" ? 1 : st === "running" ? 0.55 : st === "failed" ? 0.7 : 0,
      },
    });
  }
  return out;
}

/* ---- lookups ---- */
export function getWorkflow(slug: string): Workflow | undefined {
  return workflows.find((w) => w.slug === slug);
}
export function getRun(id: string): WorkflowRun | undefined {
  return runs.find((r) => r.id === id);
}
export function runsForWorkflow(workflowId: string): WorkflowRun[] {
  return runs.filter((r) => r.workflowId === workflowId);
}
