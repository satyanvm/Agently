"use client";

import * as React from "react";
import type { AgentNode } from "@/lib/types";
import { AgentGlyph } from "@/components/agent-glyph";
import { StatusDot, statusMeta } from "@/components/ui/status";
import { cn, formatCompact, formatCost } from "@/lib/utils";

const COL_W = 224;
const ROW_H = 128;
const NODE_W = 184;
const NODE_H = 96;
const PAD_X = 16;
const PAD_Y = 16;

function pos(node: AgentNode) {
  return {
    x: PAD_X + node.col * COL_W,
    y: PAD_Y + node.row * ROW_H,
  };
}

type EdgeKind = "flowing" | "done" | "pending";

function edgeKind(from: AgentNode, to: AgentNode): EdgeKind {
  if (from.status === "succeeded" && (to.status === "running" || to.status === "succeeded"))
    return "done";
  if (from.status === "running" || from.status === "succeeded") return "flowing";
  return "pending";
}

export function AgentGraph({
  agents,
  selectedId,
  onSelect,
  className,
}: {
  agents: AgentNode[];
  selectedId?: string | null;
  onSelect?: (id: string) => void;
  className?: string;
}) {
  const byId = React.useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  // Empty graph (e.g. a workflow that hasn't run yet, so no run_agents exist).
  // Guard before any Math.max — Math.max(...[]) is -Infinity, which yields a
  // non-finite width and crashes React's style serializer.
  if (agents.length === 0) {
    return (
      <div
        className={cn(
          "flex min-h-[160px] w-full items-center justify-center rounded-lg border border-dashed border-border text-[12px] text-faint",
          className,
        )}
      >
        Run this workflow to see its agent graph.
      </div>
    );
  }

  const maxCol = Math.max(...agents.map((a) => a.col));
  const maxRow = Math.max(...agents.map((a) => a.row));
  const width = PAD_X * 2 + maxCol * COL_W + NODE_W;
  const height = PAD_Y * 2 + maxRow * ROW_H + NODE_H;

  const edges = agents.flatMap((a) =>
    a.dependsOn.map((depId) => {
      const from = byId.get(depId);
      if (!from) return null;
      return { from, to: a, kind: edgeKind(from, a) };
    }),
  ).filter(Boolean) as { from: AgentNode; to: AgentNode; kind: EdgeKind }[];

  return (
    <div className={cn("relative w-full min-w-0 max-w-full overflow-x-auto", className)}>
      <div className="relative" style={{ width, height, minWidth: width }}>
        {/* edges */}
        <svg
          width={width}
          height={height}
          className="absolute inset-0"
          style={{ pointerEvents: "none" }}
        >
          <defs>
            <marker id="arrow-flow" markerWidth="7" markerHeight="7" refX="5" refY="3" orient="auto">
              <path d="M0,0 L6,3 L0,6 Z" fill="var(--color-accent-soft)" />
            </marker>
            <marker id="arrow-done" markerWidth="7" markerHeight="7" refX="5" refY="3" orient="auto">
              <path d="M0,0 L6,3 L0,6 Z" fill="var(--color-success)" />
            </marker>
            <marker id="arrow-pending" markerWidth="7" markerHeight="7" refX="5" refY="3" orient="auto">
              <path d="M0,0 L6,3 L0,6 Z" fill="var(--color-ghost)" />
            </marker>
          </defs>
          {edges.map(({ from, to, kind }, i) => {
            const a = pos(from);
            const b = pos(to);
            const x1 = a.x + NODE_W;
            const y1 = a.y + NODE_H / 2;
            const x2 = b.x;
            const y2 = b.y + NODE_H / 2;
            const mx = (x1 + x2) / 2;
            const d = `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2 - 7},${y2}`;
            const isFlow = kind === "flowing";
            const isDone = kind === "done";
            const stroke = isFlow
              ? "var(--color-accent-soft)"
              : isDone
                ? "var(--color-success)"
                : "var(--color-ghost)";
            return (
              <g key={i}>
                <path
                  d={d}
                  fill="none"
                  stroke={stroke}
                  strokeWidth={1.5}
                  strokeOpacity={isFlow ? 0.9 : isDone ? 0.55 : 0.5}
                  className={isFlow ? "flow-line" : undefined}
                  markerEnd={`url(#arrow-${isFlow ? "flow" : isDone ? "done" : "pending"})`}
                />
              </g>
            );
          })}
        </svg>

        {/* nodes */}
        {agents.map((node) => {
          const p = pos(node);
          const selected = selectedId === node.id;
          return (
            <GraphNode
              key={node.id}
              node={node}
              selected={selected}
              onSelect={onSelect}
              style={{ left: p.x, top: p.y, width: NODE_W, height: NODE_H }}
            />
          );
        })}
      </div>
    </div>
  );
}

function GraphNode({
  node,
  selected,
  onSelect,
  style,
}: {
  node: AgentNode;
  selected: boolean;
  onSelect?: (id: string) => void;
  style: React.CSSProperties;
}) {
  const m = statusMeta(node.status);
  const active = node.status === "running";
  return (
    <button
      onClick={() => onSelect?.(node.id)}
      style={style}
      className={cn(
        "absolute flex flex-col rounded-xl border bg-surface p-3 text-left shadow-card transition-all",
        selected
          ? "border-accent/60 ring-2 ring-accent/30"
          : "border-border hover:border-border-strong hover:bg-surface-2",
        active && !selected && "border-running/40",
      )}
    >
      {active && (
        <span className="absolute -inset-px -z-10 rounded-xl bg-running/10 blur-[2px]" />
      )}
      <div className="flex items-center gap-2.5">
        <AgentGlyph role={node.role} size="sm" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] font-semibold leading-tight text-fg">
            {node.name}
          </div>
          <div className="truncate font-mono text-[10px] text-faint">
            {node.model.replace("claude-", "")}
          </div>
        </div>
        <StatusDot status={node.status} />
      </div>

      <div className="mt-auto pt-2.5">
        {node.status !== "idle" ? (
          <>
            <div className="h-1 w-full overflow-hidden rounded-full bg-surface-3">
              <div
                className={cn("h-full rounded-full", m.dot)}
                style={{ width: `${Math.round(node.metrics.progress * 100)}%` }}
              />
            </div>
            <div className="mt-1.5 flex items-center justify-between text-[10px] tabular-nums text-faint">
              <span>{formatCompact(node.metrics.tokens)} tok</span>
              <span>{formatCost(node.metrics.costUsd)}</span>
            </div>
          </>
        ) : (
          <div className="text-[10px] text-ghost">queued · awaiting dependency</div>
        )}
      </div>
    </button>
  );
}

/** Compact legend for the graph. */
export function GraphLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-faint">
      <span className="flex items-center gap-1.5">
        <span className="h-px w-5 bg-accent-soft" /> active hand-off
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-px w-5 bg-success/60" /> completed
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-px w-5 bg-ghost" /> pending
      </span>
    </div>
  );
}
