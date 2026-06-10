"use client";

import * as React from "react";
import Link from "next/link";
import {
  Play,
  Pause,
  Settings2,
  Bot,
  Gauge,
  Clock,
  CircleDollarSign,
  Calendar,
  GitBranch,
  ArrowRight,
  Wrench,
  Hash,
} from "lucide-react";
import type { Workflow, AgentNode, WorkflowRun } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { StatusBadge, StatusDot, statusMeta } from "@/components/ui/status";
import { Avatar } from "@/components/ui/avatar";
import { AgentGraph, GraphLegend } from "@/components/agent-graph";
import { AgentGlyph, roleMeta } from "@/components/agent-glyph";
import { LiveTimer } from "@/components/live";
import { RunWorkflowDialog } from "@/components/run-workflow-dialog";
import {
  cn,
  formatCost,
  formatDuration,
  formatCompact,
  timeAgo,
  NOW,
} from "@/lib/utils";

function runtimeOf(run: WorkflowRun): number | null {
  if (!run.startedAt) return null;
  const end = run.finishedAt ? Date.parse(run.finishedAt) : NOW;
  return end - Date.parse(run.startedAt);
}

export function WorkflowDetail({
  wf,
  agents,
  runs,
  activeRun,
}: {
  wf: Workflow;
  agents: AgentNode[];
  runs: WorkflowRun[];
  activeRun: WorkflowRun | null;
}) {
  const [selectedId, setSelectedId] = React.useState<string | null>(agents[0]?.id ?? null);
  const selected = agents.find((a) => a.id === selectedId) ?? agents[0];
  const [runOpen, setRunOpen] = React.useState(false);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-[640px]">
          <div className="flex items-center gap-3">
            <h1 className="text-[22px] font-semibold tracking-tight text-fg">{wf.name}</h1>
            <StatusBadge status={wf.status} />
          </div>
          <p className="mt-2 text-[14px] leading-relaxed text-muted">{wf.description}</p>
          <div className="mt-3 flex flex-wrap items-center gap-3 text-[12px] text-faint">
            <span className="inline-flex items-center gap-1.5">
              <Avatar initials={wf.owner.initials} size="xs" /> {wf.owner.name}
            </span>
            <span className="text-ghost">·</span>
            {wf.schedule ? (
              <span className="inline-flex items-center gap-1.5">
                <Calendar className="size-3.5" /> {wf.schedule}
              </span>
            ) : (
              <span className="inline-flex items-center gap-1.5 capitalize">
                <GitBranch className="size-3.5" /> {wf.trigger} trigger
              </span>
            )}
            <span className="text-ghost">·</span>
            <span>created {timeAgo(wf.createdAt)}</span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm">
            <Settings2 className="size-4" /> Configure
          </Button>
          {wf.status === "running" ? (
            <Button variant="secondary" size="sm">
              <Pause className="size-4" /> Pause
            </Button>
          ) : null}
          <Button variant="primary" size="sm" onClick={() => setRunOpen(true)}>
            <Play className="size-4" /> Run now
          </Button>
        </div>
      </div>

      <RunWorkflowDialog open={runOpen} onOpenChange={setRunOpen} workflow={wf} />

      {/* KPI strip */}
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-4 lg:grid-cols-5">
        <Kpi icon={<Gauge className="size-3.5 text-success" />} label="Success rate" value={`${(wf.stats.successRate * 100).toFixed(1)}%`} />
        <Kpi
          icon={<Clock className="size-3.5 text-running" />}
          label={activeRun ? "Live runtime" : "Avg runtime"}
          value={
            activeRun && activeRun.startedAt ? (
              <LiveTimer startISO={activeRun.startedAt} baseMs={runtimeOf(activeRun) ?? 0} />
            ) : (
              formatDuration(wf.stats.avgRuntimeMs)
            )
          }
        />
        <Kpi icon={<CircleDollarSign className="size-3.5 text-warn" />} label="Avg cost / run" value={formatCost(wf.stats.avgCostUsd)} />
        <Kpi icon={<Hash className="size-3.5 text-accent-soft" />} label="Total runs" value={`${wf.stats.totalRuns}`} />
        <Kpi icon={<Bot className="size-3.5 text-muted" />} label="Agents" value={`${wf.agentCount}`} className="col-span-2 sm:col-span-1" />
      </div>

      {/* Main grid */}
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
        {/* LEFT — graph + inspector */}
        <div className="space-y-6">
          <Card>
            <div className="flex items-center justify-between px-5 pt-4">
              <h3 className="text-[13px] font-semibold text-fg">Agent graph</h3>
              <GraphLegend />
            </div>
            <div className="px-3 pb-4 pt-3">
              <AgentGraph agents={agents} selectedId={selectedId} onSelect={setSelectedId} />
            </div>
          </Card>

          {selected && <AgentInspector agent={selected} />}
        </div>

        {/* RIGHT — statuses + timeline */}
        <div className="space-y-6">
          <Card>
            <div className="px-5 pb-1 pt-4">
              <h3 className="text-[13px] font-semibold text-fg">Agent statuses</h3>
            </div>
            <div className="px-2 pb-3">
              {agents.map((a) => {
                const m = statusMeta(a.status);
                const rm = roleMeta(a.role);
                return (
                  <button
                    key={a.id}
                    onClick={() => setSelectedId(a.id)}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors",
                      selectedId === a.id ? "bg-surface-2" : "hover:bg-surface-2/50",
                    )}
                  >
                    <AgentGlyph role={a.role} size="sm" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[13px] font-medium text-fg">{a.name}</div>
                      <div className={cn("text-[11px]", rm.tone)}>{rm.label}</div>
                    </div>
                    <span className={cn("text-[11px] font-medium", m.fg)}>{m.label}</span>
                    <StatusDot status={a.status} />
                  </button>
                );
              })}
            </div>
          </Card>

          <Card>
            <div className="flex items-center justify-between px-5 pb-1 pt-4">
              <h3 className="text-[13px] font-semibold text-fg">Execution timeline</h3>
              <Link href="/runs" className="text-[12px] text-muted hover:text-fg">All runs</Link>
            </div>
            <div className="px-5 pb-5 pt-3">
              <ol className="relative space-y-4 before:absolute before:left-[6px] before:top-2 before:h-[calc(100%-16px)] before:w-px before:bg-border">
                {runs.map((run) => {
                  const m = statusMeta(run.status);
                  return (
                    <li key={run.id} className="relative pl-6">
                      <span className={cn("absolute left-0 top-1 h-3 w-3 rounded-full ring-4 ring-surface", m.dot)} />
                      <Link href={`/runs/${run.id}`} className="group block">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-[13px] font-medium text-fg group-hover:underline">
                            Run #{run.number}
                          </span>
                          <StatusBadge status={run.status} size="sm" />
                        </div>
                        <div className="mt-0.5 flex items-center gap-2 text-[11px] tabular-nums text-faint">
                          <span>{timeAgo(run.startedAt ?? run.queuedAt)}</span>
                          <span className="text-ghost">·</span>
                          <span>{formatDuration(runtimeOf(run))}</span>
                          <span className="text-ghost">·</span>
                          <span>{formatCost(run.costUsd)}</span>
                        </div>
                      </Link>
                    </li>
                  );
                })}
              </ol>
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}

function Kpi({
  icon,
  label,
  value,
  className,
}: {
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("bg-surface px-4 py-3", className)}>
      <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted">
        {icon}
        {label}
      </div>
      <div className="mt-1 text-[18px] font-semibold tabular-nums text-fg">{value}</div>
    </div>
  );
}

function AgentInspector({ agent }: { agent: AgentNode }) {
  const rm = roleMeta(agent.role);
  return (
    <Card>
      <div className="flex items-start gap-3 border-b border-border px-5 py-4">
        <AgentGlyph role={agent.role} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h3 className="text-[15px] font-semibold tracking-tight text-fg">{agent.name}</h3>
            <StatusBadge status={agent.status} size="sm" />
          </div>
          <div className="mt-0.5 flex items-center gap-2 text-[12px]">
            <span className={rm.tone}>{rm.label}</span>
            <span className="text-ghost">·</span>
            <span className="font-mono text-faint">{agent.model}</span>
          </div>
        </div>
        <Link
          href="#"
          className="hidden items-center gap-1 text-[12px] text-muted hover:text-fg sm:flex"
        >
          View logs <ArrowRight className="size-3.5" />
        </Link>
      </div>
      <div className="px-5 py-4">
        <p className="text-[13px] leading-relaxed text-muted">{agent.summary}</p>
        <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Metric label="Tokens" value={formatCompact(agent.metrics.tokens)} />
          <Metric label="Cost" value={formatCost(agent.metrics.costUsd)} />
          <Metric label="Runtime" value={formatDuration(agent.metrics.runtimeMs)} />
          <Metric label="Tool calls" value={`${agent.metrics.toolCalls}`} />
        </div>
        {agent.dependsOn.length > 0 && (
          <div className="mt-4 flex items-center gap-2 border-t border-border pt-3 text-[11px] text-faint">
            <Wrench className="size-3" />
            depends on {agent.dependsOn.length} upstream agent{agent.dependsOn.length > 1 ? "s" : ""}
          </div>
        )}
      </div>
    </Card>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-surface-2/40 px-3 py-2">
      <div className="text-[15px] font-semibold tabular-nums text-fg">{value}</div>
      <div className="text-[10px] text-faint">{label}</div>
    </div>
  );
}
