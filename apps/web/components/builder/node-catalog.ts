import {
  Bot,
  Webhook,
  Clock,
  MousePointerClick,
  Globe,
  Mail,
  Database,
  GitBranch,
  Filter,
  Repeat,
  Wrench,
  FileText,
  Code2,
  Send,
  MessageSquare,
  MessageCircle,
  Newspaper,
  Share2,
  ShoppingCart,
  Cloud,
  Cpu,
  Megaphone,
  CalendarCheck,
  FlaskConical,
  type LucideIcon,
} from "lucide-react";

import integrationCatalog from "./integration-catalog.generated.json";

export type NodeKind = "trigger" | "agent" | "tool" | "logic" | "output";

export interface NodeSpec {
  /** Unique catalog id (used as the React Flow node type if "custom" routing was used). */
  id: string;
  /** Display name. */
  label: string;
  /** Short one-line description shown in the palette + inspector. */
  description: string;
  kind: NodeKind;
  icon: LucideIcon;
  /** Tailwind text class for the icon (mapped onto the design system status palette). */
  tone: string;
  /** Tailwind background class for the icon chip. */
  toneBg: string;
  /** Integration cluster ("communication", "devtools", …); unset for built-ins. */
  cluster?: string;
  /** Human label of the cluster, for palette group headers. */
  clusterLabel?: string;
  /**
   * Credential type id (docs/credentials-contract.md §2/§3). Non-null means the
   * node needs credentials; unset/null (legacy nodes) means no credential UI.
   */
  credentialType?: string | null;
  /**
   * Piece trigger strategy (docs/pieces-runtime-contract.md §7a): "webhook",
   * "polling", or "app_webhook". Present only on `pieces.*` trigger nodes.
   */
  strategy?: "webhook" | "polling" | "app_webhook";
}

export const NODE_CATALOG: NodeSpec[] = [
  // ── Triggers ────────────────────────────────────────────────────────────
  {
    id: "trigger.manual",
    label: "Manual trigger",
    description: "Start the workflow on demand from the dashboard.",
    kind: "trigger",
    icon: MousePointerClick,
    tone: "text-running",
    toneBg: "bg-running-bg",
  },
  {
    id: "trigger.webhook",
    label: "Webhook",
    description: "Receive an HTTP request and start the workflow.",
    kind: "trigger",
    icon: Webhook,
    tone: "text-running",
    toneBg: "bg-running-bg",
  },
  {
    id: "trigger.schedule",
    label: "Schedule",
    description: "Run on a cron schedule (e.g. every weekday at 9am).",
    kind: "trigger",
    icon: Clock,
    tone: "text-running",
    toneBg: "bg-running-bg",
  },

  // ── Agents ──────────────────────────────────────────────────────────────
  {
    id: "agent.llm",
    label: "AI agent",
    description: "Reason, plan, and call tools with a large language model.",
    kind: "agent",
    icon: Bot,
    tone: "text-accent",
    toneBg: "bg-accent-bg",
  },
  {
    id: "agent.chat",
    label: "Chat completion",
    description: "Single-turn prompt → response, no tool use.",
    kind: "agent",
    icon: MessageSquare,
    tone: "text-accent",
    toneBg: "bg-accent-bg",
  },

  // ── Tools ───────────────────────────────────────────────────────────────
  {
    id: "tool.browser",
    label: "Browser",
    description: "Navigate, click, extract — drive a real browser.",
    kind: "tool",
    icon: Globe,
    tone: "text-success",
    toneBg: "bg-success-bg",
  },
  {
    id: "tool.http",
    label: "HTTP request",
    description: "Call any REST API.",
    kind: "tool",
    icon: Wrench,
    tone: "text-success",
    toneBg: "bg-success-bg",
  },
  {
    id: "tool.code",
    label: "Run code",
    description: "Execute a sandboxed JavaScript or Python snippet.",
    kind: "tool",
    icon: Code2,
    tone: "text-success",
    toneBg: "bg-success-bg",
  },
  {
    id: "tool.db",
    label: "Database query",
    description: "Read or write rows in a connected database.",
    kind: "tool",
    icon: Database,
    tone: "text-success",
    toneBg: "bg-success-bg",
  },

  // ── Logic ───────────────────────────────────────────────────────────────
  {
    id: "logic.branch",
    label: "If / Else",
    description: "Branch the flow on a boolean condition.",
    kind: "logic",
    icon: GitBranch,
    tone: "text-warn",
    toneBg: "bg-warn-bg",
  },
  {
    id: "logic.filter",
    label: "Filter",
    description: "Pass items through only if they match a rule.",
    kind: "logic",
    icon: Filter,
    tone: "text-warn",
    toneBg: "bg-warn-bg",
  },
  {
    id: "logic.loop",
    label: "Loop",
    description: "Iterate over a list and run the downstream branch for each item.",
    kind: "logic",
    icon: Repeat,
    tone: "text-warn",
    toneBg: "bg-warn-bg",
  },

  // ── Outputs ─────────────────────────────────────────────────────────────
  {
    id: "output.email",
    label: "Send email",
    description: "Deliver a formatted email when this step is reached.",
    kind: "output",
    icon: Mail,
    tone: "text-danger",
    toneBg: "bg-danger-bg",
  },
  {
    id: "output.slack",
    label: "Send to Slack",
    description: "Post a message to a Slack channel or DM.",
    kind: "output",
    icon: Send,
    tone: "text-danger",
    toneBg: "bg-danger-bg",
  },
  {
    id: "output.report",
    label: "Generate report",
    description: "Compose a structured report artifact (markdown / PDF).",
    kind: "output",
    icon: FileText,
    tone: "text-danger",
    toneBg: "bg-danger-bg",
  },
];

/* ── Integration nodes (generated from packages/nodes/catalog) ─────────────────
 * Hundreds of service nodes join the palette as data. Regenerate with:
 *   node packages/nodes/build-web.mjs
 * The reasoner executes these via its generic integration runtime; the ids are
 * shared across the Go planner, the Python executor, and this palette. */

interface GeneratedNode {
  id: string;
  label: string;
  description: string;
  kind: string;
  runtime: string;
  cluster: string;
  clusterLabel: string;
  config: NodeField[];
  credentials: string[];
  /** Optional — older generator output omits it (legacy nodes: no credential UI). */
  credentialType?: string | null;
  /** Piece trigger strategy; present only on `pieces.*` trigger nodes. */
  strategy?: "webhook" | "polling" | "app_webhook";
}

const CLUSTER_ICON: Record<string, LucideIcon> = {
  communication: MessageCircle,
  productivity: CalendarCheck,
  devtools: Code2,
  "data-web": Globe,
  "research-news": Newspaper,
  social: Share2,
  "commerce-finance": ShoppingCart,
  "cloud-storage": Cloud,
  "ai-ml": Cpu,
  "crm-marketing": Megaphone,
  databases: Database,
  "utilities-files": FlaskConical,
};

/** Integration kinds map onto the palette's tone system: actions read as tools. */
function paletteKind(kind: string): NodeKind {
  if (kind === "trigger" || kind === "logic" || kind === "output") return kind;
  return "tool";
}

const INTEGRATION_TONE: Record<NodeKind, { tone: string; toneBg: string }> = {
  trigger: { tone: "text-running", toneBg: "bg-running-bg" },
  agent: { tone: "text-accent", toneBg: "bg-accent-bg" },
  tool: { tone: "text-success", toneBg: "bg-success-bg" },
  logic: { tone: "text-warn", toneBg: "bg-warn-bg" },
  output: { tone: "text-danger", toneBg: "bg-danger-bg" },
};

const INTEGRATION_NODES: NodeSpec[] = (integrationCatalog as GeneratedNode[]).map((n) => {
  const kind = paletteKind(n.kind);
  return {
    id: n.id,
    label: n.label,
    description: n.description,
    kind,
    icon: CLUSTER_ICON[n.cluster] ?? Wrench,
    tone: INTEGRATION_TONE[kind].tone,
    toneBg: INTEGRATION_TONE[kind].toneBg,
    cluster: n.cluster,
    clusterLabel: n.clusterLabel,
    credentialType: n.credentialType ?? null,
    ...(n.strategy ? { strategy: n.strategy } : {}),
  };
});

NODE_CATALOG.push(...INTEGRATION_NODES);

export const NODE_KIND_META: Record<NodeKind, { label: string; tone: string }> = {
  trigger: { label: "Triggers", tone: "text-running" },
  agent: { label: "Agents", tone: "text-accent" },
  tool: { label: "Tools", tone: "text-success" },
  logic: { label: "Logic", tone: "text-warn" },
  output: { label: "Outputs", tone: "text-danger" },
};

export function findNodeSpec(id: string): NodeSpec | undefined {
  return NODE_CATALOG.find((n) => n.id === id);
}

/**
 * A single editable field, shared by node config forms AND credential forms
 * (docs/credentials-contract.md §1). One dynamic renderer maps control →
 * component: components/builder/dynamic-field.tsx.
 */
export interface NodeField {
  key: string;
  label: string;
  control: "text" | "secret" | "textarea" | "number" | "checkbox" | "select" | "json";
  required?: boolean;
  placeholder?: string;
  options?: string[];
  help?: string;
  /** Activepieces dynamic/dropdown props: rendered as text, raw-ID fallback. */
  dynamic?: boolean;
}

/**
 * Reserved config key holding the selected credential id (contract §6). Never
 * rendered as a normal field and excluded from the "n/m configured" count.
 */
export const CREDENTIAL_ID_KEY = "__credentialId";

/**
 * The config form for each node type, keyed by catalog id. The inspector renders
 * these generically; the reasoner's executor for each type reads the same keys.
 * Keep the two in sync (apps/reasoner/reasoner/nodes.py).
 */
export const NODE_FIELDS: Record<string, NodeField[]> = {
  "trigger.manual": [],
  "trigger.webhook": [
    { key: "path", label: "Path", control: "text", placeholder: "/hooks/my-workflow" },
  ],
  "trigger.schedule": [
    { key: "cron", label: "Cron", control: "text", placeholder: "0 9 * * 1-5" },
  ],
  "agent.llm": [
    { key: "system", label: "System prompt", control: "textarea", placeholder: "You are a helpful agent…" },
    { key: "prompt", label: "Prompt", control: "textarea", placeholder: "Use {{input.topic}} and upstream outputs…" },
    { key: "model", label: "Model", control: "select", options: ["gpt-4o-mini", "gpt-4o"] },
  ],
  "agent.chat": [
    { key: "system", label: "System prompt", control: "textarea" },
    { key: "prompt", label: "Prompt", control: "textarea" },
    { key: "model", label: "Model", control: "select", options: ["gpt-4o-mini", "gpt-4o"] },
  ],
  "tool.browser": [
    { key: "urls", label: "URLs (one per line)", control: "textarea", placeholder: "https://example.com" },
  ],
  "tool.http": [
    { key: "method", label: "Method", control: "select", options: ["GET", "POST", "PUT", "PATCH", "DELETE"] },
    { key: "url", label: "URL", control: "text", placeholder: "https://api.example.com/v1/resource" },
    { key: "headers", label: "Headers (JSON)", control: "textarea", placeholder: '{ "Authorization": "Bearer …" }' },
    { key: "body", label: "Body", control: "textarea" },
  ],
  "tool.code": [
    { key: "language", label: "Language", control: "select", options: ["python", "javascript"] },
    { key: "source", label: "Source", control: "textarea", help: "Runs in a sandboxed subprocess when TOOL_CODE_ENABLED=1; recorded otherwise. stdin = {input, outputs, config} JSON; print a JSON object to emit fields." },
  ],
  "tool.db": [
    { key: "query", label: "SQL", control: "textarea", help: "Runs against the operator-configured TOOL_DB_URL database (never the platform DB); recorded when unset." },
  ],
  "logic.branch": [
    { key: "condition", label: "Condition", control: "text", placeholder: "outputs.fetch.status == 200" },
  ],
  "logic.filter": [
    { key: "condition", label: "Keep when", control: "text", placeholder: "item.score > 0.5" },
  ],
  "logic.loop": [
    { key: "items", label: "Items expression", control: "text", placeholder: "outputs.fetch.items" },
  ],
  "output.email": [
    { key: "to", label: "To", control: "text", placeholder: "you@example.com" },
    { key: "subject", label: "Subject", control: "text" },
  ],
  "output.slack": [
    { key: "webhookUrl", label: "Webhook URL", control: "text", placeholder: "https://hooks.slack.com/…" },
    { key: "message", label: "Message", control: "textarea" },
  ],
  "output.report": [
    { key: "title", label: "Title", control: "text", placeholder: "Digest" },
    { key: "format", label: "Format", control: "select", options: ["markdown", "pdf"] },
  ],
};

// Integration nodes' inspector forms come straight from the generated catalog.
for (const n of integrationCatalog as GeneratedNode[]) {
  NODE_FIELDS[n.id] = n.config;
}

export function nodeFields(typeId: string): NodeField[] {
  return NODE_FIELDS[typeId] ?? [];
}

/** Seed config (empty strings) so a freshly-dropped node has all its keys. */
export function defaultConfig(typeId: string): Record<string, unknown> {
  const cfg: Record<string, unknown> = {};
  for (const f of nodeFields(typeId)) {
    if (f.control === "select") cfg[f.key] = f.options?.[0] ?? "";
    else if (f.control === "checkbox") cfg[f.key] = false;
    else cfg[f.key] = "";
  }
  return cfg;
}
