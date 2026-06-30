/**
 * Mapping / serialization seam between the React Flow builder canvas and the
 * domain graph the API persists.
 *
 * The builder works in React Flow's native shape — nodes with `id`, `position`
 * and `data: { label, typeId, config }`, plus edges with `source`/`target`. The
 * API's `PUT /api/workflows/{slug}/graph` endpoint (see
 * apps/api/internal/domain/validate/api.go → ParseSaveGraphInput) expects domain
 * GraphNodes: `{ key, role, dependsOn, type, config, ... }` with no edge list —
 * the DAG is reconstructed from each node's `dependsOn`.
 *
 * This module owns both directions of that translation so the builder and the
 * API never have to know each other's shape.
 *
 * Round-trip design:
 *  - key:        the React Flow node `id` is used verbatim as the GraphNode `key`
 *                (a stable string within the version). On load we restore it as
 *                the node `id`, so a save→load round-trip is identity.
 *  - role:       derived from the node's catalog `type` via NODE_ROLE (the domain
 *                requires a valid AgentRole; non-agent palette nodes map to
 *                "orchestrator", matching the API's own fallback).
 *  - dependsOn:  computed from incoming edges — for a node N, every edge with
 *                `target === N.id` contributes its `source` to N.dependsOn. On
 *                load we invert this: one edge per (dep → node) pair.
 *  - positions:  React Flow's float `position {x, y}` is preserved exactly under
 *                `config._position` so layout survives a round-trip; we also
 *                derive integer `col`/`row` from it for the domain's grid fields
 *                (used by the read-only run graph), but `_position` is the source
 *                of truth the builder reads back.
 */

import type { Edge, Node } from "@xyflow/react";
import type { BuilderNode } from "./api";
import { findNodeSpec, type NodeKind } from "../components/builder/node-catalog";
import type { NodeData, WorkflowNodeType } from "../components/builder/workflow-builder";

/* ----------------------------- config types ----------------------------- */

/** Stored under config._position; the exact canvas coordinates of a node. */
export interface NodePosition {
  x: number;
  y: number;
}

/** Per-node config shapes, keyed by catalog `type`. These mirror NODE_FIELDS in
 *  node-catalog.ts and the reasoner's executors (apps/reasoner/reasoner/nodes.py).
 *  Every field is optional because the builder seeds empty strings and the user
 *  may leave fields blank; the serialization layer never drops unknown keys. */
export interface TriggerManualConfig {}
export interface TriggerWebhookConfig {
  path?: string;
}
export interface TriggerScheduleConfig {
  cron?: string;
}
export interface AgentLlmConfig {
  system?: string;
  prompt?: string;
  model?: string;
}
export interface AgentChatConfig {
  system?: string;
  prompt?: string;
  model?: string;
}
export interface ToolBrowserConfig {
  urls?: string;
}
export interface ToolHttpConfig {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  url?: string;
  headers?: string;
  body?: string;
}
export interface ToolCodeConfig {
  language?: "python" | "javascript";
  source?: string;
}
export interface ToolDbConfig {
  query?: string;
}
export interface LogicBranchConfig {
  condition?: string;
}
export interface LogicFilterConfig {
  condition?: string;
}
export interface LogicLoopConfig {
  items?: string;
}
export interface OutputEmailConfig {
  to?: string;
  subject?: string;
}
export interface OutputSlackConfig {
  webhookUrl?: string;
  message?: string;
}
export interface OutputReportConfig {
  title?: string;
  format?: "markdown" | "pdf";
}

/** Typed config per catalog id. The serialization layer always carries an open
 *  `Record<string, unknown>` (config is open by design on the wire), but the
 *  inspector and node editors can narrow to these per `typeId`. */
export interface NodeConfigByType {
  "trigger.manual": TriggerManualConfig;
  "trigger.webhook": TriggerWebhookConfig;
  "trigger.schedule": TriggerScheduleConfig;
  "agent.llm": AgentLlmConfig;
  "agent.chat": AgentChatConfig;
  "tool.browser": ToolBrowserConfig;
  "tool.http": ToolHttpConfig;
  "tool.code": ToolCodeConfig;
  "tool.db": ToolDbConfig;
  "logic.branch": LogicBranchConfig;
  "logic.filter": LogicFilterConfig;
  "logic.loop": LogicLoopConfig;
  "output.email": OutputEmailConfig;
  "output.slack": OutputSlackConfig;
  "output.report": OutputReportConfig;
}

export type NodeTypeId = keyof NodeConfigByType;

/** A persisted config: the typed per-type config plus the internal _position the
 *  builder uses to restore exact layout. Config is open on the wire, so this is a
 *  documentation/narrowing aid rather than an enforced wire contract. */
export type PersistedConfig<T extends NodeTypeId = NodeTypeId> =
  NodeConfigByType[T] & { _position?: NodePosition };

/* ------------------------------ role mapping ----------------------------- */

/** Domain AgentRole each palette kind maps to. The domain graph stores a `role`
 *  per node; non-agent palette nodes have no natural agent role, so they fall
 *  back to "orchestrator" (the same fallback the API applies server-side). */
const KIND_ROLE: Record<NodeKind, string> = {
  trigger: "orchestrator",
  agent: "orchestrator",
  tool: "browser",
  logic: "orchestrator",
  output: "writer",
};

/** Resolve the domain role for a catalog type id. */
function roleForType(typeId: string): string {
  const spec = findNodeSpec(typeId);
  return spec ? KIND_ROLE[spec.kind] : "orchestrator";
}

/* ------------------------------- save path ------------------------------- */

const DEFAULT_MODEL = "claude-sonnet-4-6";

/** Quantize a canvas coordinate into a nonnegative grid index for the domain's
 *  integer col/row (the read-only run-graph view uses these). The exact float
 *  position is preserved separately under config._position. */
function toGridIndex(coord: number, cell: number): number {
  const idx = Math.round(coord / cell);
  return idx < 0 ? 0 : idx;
}

/**
 * React Flow `{ nodes, edges }` → domain `BuilderNode[]` (the PUT payload).
 *
 * - `key` is the React Flow node id.
 * - `role` comes from the node spec.
 * - `dependsOn` is collected from every edge whose `target` is this node.
 * - the exact `position` is preserved under `config._position`; `col`/`row` are
 *   derived from it for the domain grid.
 * - `config` is otherwise passed through untouched.
 */
export function toBuilderNodes(
  nodes: WorkflowNodeType[],
  edges: Edge[],
): BuilderNode[] {
  // Build an adjacency map: target id -> set of source ids (incoming edges).
  const incoming = new Map<string, string[]>();
  for (const e of edges) {
    if (!e.source || !e.target) continue;
    const list = incoming.get(e.target) ?? [];
    if (!list.includes(e.source)) list.push(e.source);
    incoming.set(e.target, list);
  }

  return nodes.map((n) => {
    const typeId = n.data.typeId;
    const pos: NodePosition = { x: n.position?.x ?? 0, y: n.position?.y ?? 0 };
    const config: Record<string, unknown> = {
      ...(n.data.config ?? {}),
      _position: pos,
    };
    // Prefer an explicit model in config; otherwise fall back to the default so
    // the domain's required `model` is never empty.
    const model =
      typeof (n.data.config as { model?: unknown })?.model === "string" &&
      (n.data.config as { model?: string }).model
        ? (n.data.config as { model: string }).model
        : DEFAULT_MODEL;

    return {
      key: n.id,
      type: typeId || "agent.llm",
      name: n.data.label ?? typeId,
      role: roleForType(typeId),
      model,
      col: toGridIndex(pos.x, 280),
      row: toGridIndex(pos.y, 160),
      dependsOn: incoming.get(n.id) ?? [],
      config,
    };
  });
}

/* ------------------------------- load path ------------------------------- */

/** Read the preserved canvas position from a persisted config, falling back to a
 *  grid layout derived from col/row when a node predates the builder (compiled
 *  from a prompt) and so has no _position. */
function positionFor(node: BuilderNode): NodePosition {
  const raw = (node.config ?? {})["_position"];
  if (
    raw &&
    typeof raw === "object" &&
    typeof (raw as NodePosition).x === "number" &&
    typeof (raw as NodePosition).y === "number"
  ) {
    return { x: (raw as NodePosition).x, y: (raw as NodePosition).y };
  }
  // Fall back to col/row grid placement (matches the save-side cell sizes).
  return { x: (node.col ?? 0) * 280, y: (node.row ?? 0) * 160 };
}

/** Strip the internal _position key so the inspector only sees real config. */
function stripInternal(config: Record<string, unknown>): Record<string, unknown> {
  const { _position, ...rest } = config ?? {};
  void _position;
  return rest;
}

/**
 * Domain `BuilderNode[]` → React Flow `{ nodes, edges }` (for LOAD).
 *
 * - one React Flow node per GraphNode, id = key, position restored from
 *   config._position (or col/row fallback).
 * - one edge per `dependsOn` entry: dep (source) → node (target).
 */
export function fromBuilderNodes(graphNodes: BuilderNode[]): {
  nodes: WorkflowNodeType[];
  edges: Edge[];
} {
  const ids = new Set(graphNodes.map((n) => n.key));

  const nodes: WorkflowNodeType[] = graphNodes.map((gn) => {
    const typeId = gn.type || "agent.llm";
    const spec = findNodeSpec(typeId);
    const data: NodeData = {
      label: gn.name || spec?.label || typeId,
      typeId,
      config: stripInternal(gn.config ?? {}),
    };
    return {
      id: gn.key,
      type: "workflow",
      position: positionFor(gn),
      data,
    };
  });

  const edges: Edge[] = [];
  for (const gn of graphNodes) {
    for (const dep of gn.dependsOn ?? []) {
      if (!ids.has(dep)) continue; // drop dangling deps defensively
      edges.push({
        id: `e-${dep}-${gn.key}`,
        source: dep,
        target: gn.key,
      });
    }
  }

  return { nodes, edges };
}

/** Largest numeric React Flow id present, so a freshly-loaded canvas can resume
 *  the id counter without colliding with loaded nodes. Returns -1 if none. */
export function maxNumericNodeId(nodes: { id: string }[]): number {
  return nodes.reduce((max, n) => {
    const v = parseInt(n.id, 10);
    return Number.isFinite(v) && v > max ? v : max;
  }, -1);
}
