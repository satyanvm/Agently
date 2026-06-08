"use client";

import * as React from "react";
import Link from "next/link";
import {
  Square,
  RotateCw,
  Share2,
  Activity,
  Clock,
  CircleDollarSign,
  Cpu,
  Server,
  ListChecks,
  Terminal,
  Network,
  Globe,
  Package,
  ArrowRight,
  Calendar,
  Webhook,
  GitBranch,
  MousePointerClick,
} from "lucide-react";
import type { WorkflowRun, LogEntry, BrowserSession } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { StatusBadge, StatusDot, statusMeta } from "@/components/ui/status";
import { Avatar } from "@/components/ui/avatar";
import { AgentGlyph } from "@/components/agent-glyph";
import { LiveTimer, LiveCost, LivePill } from "@/components/live";
import { LogViewer } from "@/components/log-viewer";
import { AgentVisualization } from "@/components/agent-visualization";
import { BrowserSessionView } from "@/components/browser-session";
import { ArtifactList } from "@/components/artifact-list";
import { cn, formatCost, formatCompact, formatClock, formatDuration, NOW } from "@/lib/utils";

type Tab = "overview" | "logs" | "agents" | "browser" | "artifacts";

const TRIGGER = {
  manual: { icon: MousePointerClick, label: "Manual" },
  schedule: { icon: Calendar, label: "Scheduled" },
  webhook: { icon: Webhook, label: "Webhook" },
  event: { icon: GitBranch, label: "Event" },
} as const;

function runtimeOf(run: WorkflowRun): number | null {
  if (!run.startedAt) return null;
  const end = run.finishedAt ? Date.parse(run.finishedAt) : NOW;
  return end - Date.parse(run.startedAt);
}

export function RunDetail({
  run,
  logs,
  session,
}: {
  run: WorkflowRun;
  logs: LogEntry[];
  session: BrowserSession | null;
}) {
  const [tab, setTab] = React.useState<Tab>("overview");
  const isLive = run.status === "running";
  const pct = run.steps.total ? run.steps.done / run.steps.total : 0;
  const Trigger = TRIGGER[run.trigger];

  const tabs: { id: Tab; label: string; icon: typeof Terminal; count?: number }[] = [
    { id: "overview", label: "Overview", icon: ListChecks },
    { id: "logs", label: "Logs", icon: Terminal, count: logs.length },
    { id: "agents", label: "Agents", icon: Network, count: run.agents.length },
    { id: "browser", label: "Browser", icon: Globe, count: session?.actionsCount },
    { id: "artifacts", label: "Artifacts", icon: Package, count: run.artifacts.length },
  ];

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2.5">
            <Link
              href={`/workflows/${run.workflowSlug}`}
              className="text-[20px] font-semibold tracking-tight text-fg hover:underline"
            >
              {run.workflowName}
            </Link>
            <span className="font-mono text-[15px] text-faint">#{run.number}</span>
            <StatusBadge status={run.status} />
            {isLive && <LivePill />}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-3 text-[12px] text-faint">
            <span className="inline-flex items-center gap-1.5">
              <Trigger.icon className="size-3.5" /> {Trigger.label}
            </span>
            <span className="text-ghost">·</span>
            <span className="inline-flex items-center gap-1.5">
              <Avatar initials={run.triggeredBy.initials} size="xs" /> {run.triggeredBy.name}
            </span>
            <span className="text-ghost">·</span>
            <span className="inline-flex items-center gap-1.5">
              <Server className="size-3.5" /> {run.region}
            </span>
            <span className="text-ghost">·</span>
            <span>started {formatClock(run.startedAt ?? run.queuedAt)}</span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm">
            <Share2 className="size-4" /> Share
          </Button>
          <Button variant="secondary" size="sm">
            <RotateCw className="size-4" /> Re-run
          </Button>
          {isLive && (
            <Button variant="danger" size="sm">
              <Square className="size-3.5" /> Stop
            </Button>
          )}
        </div>
      </div>

      {/* Hero metrics */}
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-border bg-border md:grid-cols-3 lg:grid-cols-6">
        <Metric
          icon={<Activity className="size-3.5 text-running" />}
          label="Status"
          value={<span className={statusMeta(run.status).fg}>{statusMeta(run.status).label}</span>}
        />
        <Metric
          icon={<Clock className="size-3.5 text-accent-soft" />}
          label="Runtime"
          value={
            isLive && run.startedAt ? (
              <LiveTimer startISO={run.startedAt} baseMs={runtimeOf(run) ?? 0} />
            ) : (
              formatDuration(runtimeOf(run))
            )
          }
        />
        <Metric
          icon={<CircleDollarSign className="size-3.5 text-warn" />}
          label="Cost"
          value={isLive ? <LiveCost base={run.costUsd} /> : formatCost(run.costUsd)}
        />
        <Metric
          icon={<ListChecks className="size-3.5 text-success" />}
          label="Progress"
          value={`${run.steps.done}/${run.steps.total}`}
        />
        <Metric
          icon={<Cpu className="size-3.5 text-sky-600" />}
          label="Tokens"
          value={formatCompact(run.tokensIn + run.tokensOut)}
        />
        <Metric
          icon={<Network className="size-3.5 text-fuchsia-600" />}
          label="Agents"
          value={`${run.agents.filter((a) => a.status === "running").length}/${run.agents.length}`}
          sub="active"
        />
      </div>

      {/* Progress bar */}
      {isLive && (
        <Card className="px-5 py-4">
          <div className="mb-2 flex items-center justify-between">
            <span className="flex items-center gap-2 text-[13px] text-fg">
              <StatusDot status="running" />
              {run.currentStep}
            </span>
            <span className="text-[12px] tabular-nums text-faint">{Math.round(pct * 100)}% complete</span>
          </div>
          <Progress value={pct} tone="running" showShimmer />
        </Card>
      )}

      {/* Tabs */}
      <div className="sticky top-14 z-10 -mx-1 flex items-center gap-1 border-b border-border bg-bg/80 px-1 backdrop-blur">
        {tabs.map((t) => {
          const Icon = t.icon;
          const active = tab === t.id;
          return (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={cn(
                "relative flex items-center gap-2 px-3 py-2.5 text-[13px] font-medium transition-colors",
                active ? "text-fg" : "text-muted hover:text-fg",
              )}
            >
              <Icon className={cn("size-4", active ? "text-accent-soft" : "text-faint")} />
              {t.label}
              {t.count != null && (
                <span
                  className={cn(
                    "rounded px-1.5 text-[10px] tabular-nums",
                    active ? "bg-accent-bg text-accent-soft" : "bg-surface-2 text-faint",
                  )}
                >
                  {t.count}
                </span>
              )}
              {active && <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-accent" />}
            </button>
          );
        })}
      </div>

      {/* Tab content */}
      {tab === "overview" && <Overview run={run} logs={logs} session={session} onTab={setTab} />}
      {tab === "logs" && <LogViewer logs={logs} runStartISO={run.startedAt ?? run.queuedAt} live={isLive} />}
      {tab === "agents" && <AgentVisualization agents={run.agents} messages={run.messages} />}
      {tab === "browser" &&
        (session ? (
          <BrowserSessionView session={session} />
        ) : (
          <EmptyTab icon={Globe} text="This run didn't use a browser session." />
        ))}
      {tab === "artifacts" && (
        run.artifacts.length ? (
          <ArtifactList artifacts={run.artifacts} />
        ) : (
          <EmptyTab icon={Package} text="No artifacts produced yet." />
        )
      )}
    </div>
  );
}

/* ----------------------------------------------------------- Overview */
function Overview({
  run,
  logs,
  session,
  onTab,
}: {
  run: WorkflowRun;
  logs: LogEntry[];
  session: BrowserSession | null;
  onTab: (t: Tab) => void;
}) {
  const recentLogs = logs.slice(-7);
  const costByAgent = [...run.agents]
    .filter((a) => a.metrics.costUsd > 0)
    .sort((a, b) => b.metrics.costUsd - a.metrics.costUsd);
  const maxCost = Math.max(...costByAgent.map((a) => a.metrics.costUsd), 0.01);

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
      {/* left */}
      <div className="space-y-5">
        {/* agents grid */}
        <Card>
          <div className="flex items-center justify-between px-5 pt-4">
            <h3 className="text-[13px] font-semibold text-fg">Agents</h3>
            <button onClick={() => onTab("agents")} className="flex items-center gap-1 text-[12px] text-muted hover:text-fg">
              Topology <ArrowRight className="size-3.5" />
            </button>
          </div>
          <div className="grid gap-2.5 p-4 sm:grid-cols-2">
            {run.agents.map((a) => (
              <div key={a.id} className="rounded-lg border border-border bg-surface-2/40 p-3">
                <div className="flex items-center gap-2.5">
                  <AgentGlyph role={a.role} size="sm" />
                  <span className="min-w-0 flex-1 truncate text-[12.5px] font-medium text-fg">
                    {a.name}
                  </span>
                  <StatusDot status={a.status} />
                </div>
                <Progress
                  value={a.metrics.progress}
                  tone={a.status === "succeeded" ? "success" : a.status === "running" ? "running" : "accent"}
                  className="mt-2.5"
                />
                <div className="mt-1.5 flex justify-between text-[10px] tabular-nums text-faint">
                  <span>{formatCompact(a.metrics.tokens)} tok</span>
                  <span>{formatCost(a.metrics.costUsd)}</span>
                </div>
              </div>
            ))}
          </div>
        </Card>

        {/* recent logs preview */}
        <Card>
          <div className="flex items-center justify-between px-5 pt-4">
            <h3 className="text-[13px] font-semibold text-fg">Live activity</h3>
            <button onClick={() => onTab("logs")} className="flex items-center gap-1 text-[12px] text-muted hover:text-fg">
              Open logs <ArrowRight className="size-3.5" />
            </button>
          </div>
          <div className="m-4 mt-3 rounded-lg border border-border bg-inset p-3 font-mono text-[11.5px] leading-relaxed">
            {recentLogs.map((l) => (
              <div key={l.id} className="flex gap-2">
                <span className="shrink-0 text-ghost">{formatClock(l.ts)}</span>
                <span
                  className={cn(
                    "shrink-0",
                    l.channel === "agent" && "text-blue-600",
                    l.channel === "model" && "text-accent",
                    l.channel === "browser" && "text-emerald-600",
                    l.channel === "tool" && "text-fuchsia-600",
                    l.channel === "system" && "text-faint",
                  )}
                >
                  {l.source}
                </span>
                <span
                  className={cn(
                    "truncate",
                    l.level === "error" ? "text-danger" : l.level === "warn" ? "text-warn" : l.level === "success" ? "text-success" : "text-muted",
                  )}
                >
                  {l.message}
                </span>
              </div>
            ))}
            {run.status === "running" && (
              <div className="mt-1 flex items-center gap-1.5 text-running">
                <span className="caret">▍</span>
              </div>
            )}
          </div>
        </Card>
      </div>

      {/* right */}
      <div className="space-y-5">
        {/* cost & usage */}
        <Card>
          <div className="px-5 pb-1 pt-4">
            <h3 className="text-[13px] font-semibold text-fg">Cost & usage</h3>
          </div>
          <div className="px-5 pb-4 pt-2">
            <div className="flex items-baseline justify-between">
              <span className="text-[24px] font-semibold tabular-nums text-fg">
                {run.status === "running" ? <LiveCost base={run.costUsd} /> : formatCost(run.costUsd)}
              </span>
              <span className="text-[11px] text-faint">of $6.00 budget</span>
            </div>
            <Progress value={run.costUsd / 6} tone="warn" className="mt-2" />
            <div className="mt-3 grid grid-cols-2 gap-3 border-t border-border pt-3 text-center">
              <div>
                <div className="text-[14px] font-semibold tabular-nums text-fg">{formatCompact(run.tokensIn)}</div>
                <div className="text-[10px] text-faint">tokens in</div>
              </div>
              <div>
                <div className="text-[14px] font-semibold tabular-nums text-fg">{formatCompact(run.tokensOut)}</div>
                <div className="text-[10px] text-faint">tokens out</div>
              </div>
            </div>
            {costByAgent.length > 0 && (
              <div className="mt-4 space-y-2 border-t border-border pt-3">
                <span className="text-[11px] font-medium text-muted">Cost by agent</span>
                {costByAgent.map((a) => (
                  <div key={a.id} className="flex items-center gap-2">
                    <span className="w-28 shrink-0 truncate text-[11px] text-muted">{a.name}</span>
                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-3">
                      <div className="h-full rounded-full bg-accent" style={{ width: `${(a.metrics.costUsd / maxCost) * 100}%` }} />
                    </div>
                    <span className="w-12 shrink-0 text-right text-[11px] tabular-nums text-faint">
                      {formatCost(a.metrics.costUsd)}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Card>

        {/* browser preview */}
        {session && (
          <Card className="overflow-hidden p-0">
            <button onClick={() => onTab("browser")} className="block w-full text-left">
              <div className={cn("relative aspect-[16/9] w-full bg-gradient-to-br", session.shots.at(-1)?.tone)}>
                <div className="absolute inset-0 bg-grid opacity-30" />
                <div className="absolute left-3 top-3 flex items-center gap-1.5 rounded-md bg-black/50 px-2 py-1 text-[11px] text-white/85 backdrop-blur">
                  <Globe className="size-3" /> {session.agentName}
                </div>
                {session.status === "running" && (
                  <div className="absolute right-3 top-3"><LivePill /></div>
                )}
                <div className="absolute inset-x-3 bottom-3 truncate rounded-md bg-black/50 px-2 py-1 font-mono text-[11px] text-white/80 backdrop-blur">
                  {session.currentUrl}
                </div>
              </div>
            </button>
            <div className="flex items-center justify-between px-4 py-2.5">
              <span className="text-[12px] text-muted">{session.pagesVisited} pages · {session.actionsCount} actions</span>
              <button onClick={() => onTab("browser")} className="flex items-center gap-1 text-[12px] text-accent-soft hover:underline">
                Replay <ArrowRight className="size-3.5" />
              </button>
            </div>
          </Card>
        )}

        {/* artifacts preview */}
        <Card>
          <div className="flex items-center justify-between px-5 pb-1 pt-4">
            <h3 className="text-[13px] font-semibold text-fg">Artifacts</h3>
            <button onClick={() => onTab("artifacts")} className="text-[12px] text-muted hover:text-fg">
              All {run.artifacts.length}
            </button>
          </div>
          <div className="p-3 pt-2">
            <ArtifactList artifacts={run.artifacts.slice(0, 3)} columns={false} />
          </div>
        </Card>
      </div>
    </div>
  );
}

function Metric({
  icon,
  label,
  value,
  sub,
}: {
  icon: React.ReactNode;
  label: string;
  value: React.ReactNode;
  sub?: string;
}) {
  return (
    <div className="bg-surface px-4 py-3.5">
      <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted">
        {icon}
        {label}
      </div>
      <div className="mt-1.5 text-[19px] font-semibold tabular-nums text-fg">
        {value}
        {sub && <span className="ml-1 text-[11px] font-normal text-faint">{sub}</span>}
      </div>
    </div>
  );
}

function EmptyTab({ icon: Icon, text }: { icon: typeof Globe; text: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border py-20 text-center">
      <Icon className="size-8 text-ghost" />
      <p className="text-sm text-faint">{text}</p>
    </div>
  );
}
