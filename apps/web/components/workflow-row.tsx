import Link from "next/link";
import { Bot, ChevronRight } from "lucide-react";
import type { Workflow } from "@/lib/types";
import { StatusBadge } from "@/components/ui/status";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { HealthBar, Sparkline } from "@/components/ui/sparkline";
import { formatCost, formatDuration, timeAgo } from "@/lib/utils";

/** Rich card used on the Workflows index. */
export function WorkflowCard({ wf }: { wf: Workflow }) {
  return (
    <Link
      href={`/workflows/${wf.slug}`}
      className="group flex flex-col rounded-lg border border-border bg-surface p-4 shadow-card transition-colors hover:border-border-strong hover:bg-surface-2"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-[14px] font-semibold tracking-tight text-fg">{wf.name}</h3>
          <p className="mt-1 line-clamp-2 text-[12px] leading-relaxed text-muted">
            {wf.description}
          </p>
        </div>
        <StatusBadge status={wf.status} size="sm" />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        {wf.tags.map((t) => (
          <Badge key={t} variant="outline" size="sm">
            {t}
          </Badge>
        ))}
      </div>

      <div className="mt-4 flex items-end justify-between">
        <div className="space-y-1.5">
          <div className="flex items-center gap-3 text-[11px] text-faint">
            <span className="inline-flex items-center gap-1">
              <Bot className="size-3" /> {wf.agentCount} agents
            </span>
            <span className="text-ghost">·</span>
            <span>{(wf.stats.successRate * 100).toFixed(1)}% success</span>
          </div>
          <HealthBar recent={wf.stats.recent} />
        </div>
        <Sparkline data={wf.stats.trend} width={88} height={30} />
      </div>

      <div className="mt-4 flex items-center justify-between border-t border-border pt-3 text-[11px] text-faint">
        <span className="inline-flex items-center gap-1.5">
          <Avatar initials={wf.owner.initials} size="xs" />
          {wf.owner.name}
        </span>
        <span className="flex items-center gap-2.5 tabular-nums">
          <span>{formatDuration(wf.stats.avgRuntimeMs)} avg</span>
          <span className="text-ghost">·</span>
          <span>{formatCost(wf.stats.avgCostUsd)}/run</span>
          <ChevronRight className="size-3.5 text-ghost transition-transform group-hover:translate-x-0.5 group-hover:text-muted" />
        </span>
      </div>
    </Link>
  );
}

/** Compact health line for the dashboard "Workflow Health" panel. */
export function WorkflowHealthRow({ wf }: { wf: Workflow }) {
  return (
    <Link
      href={`/workflows/${wf.slug}`}
      className="flex items-center gap-3 rounded-md px-2 py-2 transition-colors hover:bg-surface-2"
    >
      <StatusBadge status={wf.status} size="sm" className="w-[92px] justify-center" />
      <span className="min-w-0 flex-1 truncate text-[13px] text-fg">{wf.name}</span>
      <HealthBar recent={wf.stats.recent.slice(-10)} className="hidden sm:flex" />
      <span className="w-12 text-right text-[12px] tabular-nums text-muted">
        {(wf.stats.successRate * 100).toFixed(0)}%
      </span>
    </Link>
  );
}
