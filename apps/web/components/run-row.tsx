import Link from "next/link";
import { ChevronRight, GitBranch, Calendar, Webhook, MousePointerClick } from "lucide-react";
import type { WorkflowRun } from "@/lib/types";
import { StatusBadge, StatusDot } from "@/components/ui/status";
import { Avatar } from "@/components/ui/avatar";
import { Progress } from "@/components/ui/progress";
import { LiveTimer } from "@/components/live";
import { formatCost, formatDuration, timeAgo, nowMs } from "@/lib/utils";
import { cn } from "@/lib/utils";

const TRIGGER_ICON = {
  manual: MousePointerClick,
  schedule: Calendar,
  webhook: Webhook,
  event: GitBranch,
} as const;

function runtime(run: WorkflowRun): number | null {
  if (!run.startedAt) return null;
  const end = run.finishedAt ? Date.parse(run.finishedAt) : nowMs();
  return end - Date.parse(run.startedAt);
}

/** Prominent card for a live / active run (dashboard hero list). */
export function ActiveRunCard({ run }: { run: WorkflowRun }) {
  const pct = run.steps.total ? run.steps.done / run.steps.total : 0;
  return (
    <Link
      href={`/runs/${run.id}`}
      className="group block rounded-lg border border-border bg-surface p-4 shadow-card transition-colors hover:border-border-strong hover:bg-surface-2"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate text-[14px] font-semibold tracking-tight text-fg">
              {run.workflowName}
            </span>
            <span className="font-mono text-[11px] text-faint">#{run.number}</span>
          </div>
          <p className="mt-0.5 truncate text-[12px] text-muted">{run.currentStep}</p>
        </div>
        <StatusBadge status={run.status} size="sm" />
      </div>

      <div className="mt-3">
        <div className="mb-1.5 flex items-center justify-between text-[11px] text-faint">
          <span className="tabular-nums">
            {run.steps.done}/{run.steps.total} steps
          </span>
          <span className="tabular-nums">{Math.round(pct * 100)}%</span>
        </div>
        <Progress value={pct} tone="running" showShimmer={run.status === "running"} />
      </div>

      <div className="mt-3.5 flex items-center gap-4 text-[12px] text-muted">
        <span className="inline-flex items-center gap-1.5 tabular-nums">
          <StatusDot status="running" className="opacity-70" />
          {run.startedAt ? (
            <LiveTimer startISO={run.startedAt} baseMs={runtime(run) ?? 0} />
          ) : (
            "—"
          )}
        </span>
        <span className="tabular-nums text-faint">
          {formatCost(run.costUsd)}
        </span>
        <span className="ml-auto flex items-center gap-1 text-faint">
          <Avatar initials={run.triggeredBy.initials} size="xs" />
        </span>
        <ChevronRight className="size-4 text-ghost transition-transform group-hover:translate-x-0.5 group-hover:text-muted" />
      </div>
    </Link>
  );
}

/** Dense table-style row for the Runs list / recent results. */
export function RunRow({ run, showWorkflow = true }: { run: WorkflowRun; showWorkflow?: boolean }) {
  const Trigger = TRIGGER_ICON[run.trigger];
  const rt = runtime(run);
  return (
    <Link
      href={`/runs/${run.id}`}
      className="group grid grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-surface-2/50 sm:grid-cols-[140px_1fr_120px_110px_92px_40px]"
    >
      <StatusBadge status={run.status} size="sm" />

      <div className="min-w-0">
        <div className="flex items-center gap-2">
          {showWorkflow && (
            <span className="truncate text-[13px] font-medium text-fg">{run.workflowName}</span>
          )}
          <span className="font-mono text-[11px] text-faint">#{run.number}</span>
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-faint">
          <Trigger className="size-3" />
          <span className="capitalize">{run.trigger}</span>
          <span className="text-ghost">·</span>
          <span>{timeAgo(run.startedAt ?? run.queuedAt)}</span>
        </div>
      </div>

      <span className="hidden items-center gap-1.5 text-[12px] tabular-nums text-muted sm:flex">
        <Avatar initials={run.triggeredBy.initials} size="xs" />
        <span className="truncate">{run.triggeredBy.name}</span>
      </span>

      <span className="hidden text-[12px] tabular-nums text-muted sm:block">
        {run.status === "running" && run.startedAt ? (
          <LiveTimer startISO={run.startedAt} baseMs={rt ?? 0} />
        ) : (
          formatDuration(rt)
        )}
      </span>

      <span className="hidden text-right text-[12px] tabular-nums text-muted sm:block">
        {formatCost(run.costUsd)}
      </span>

      <ChevronRight className="hidden size-4 justify-self-end text-ghost transition-transform group-hover:translate-x-0.5 group-hover:text-muted sm:block" />
    </Link>
  );
}

export function runtimeOf(run: WorkflowRun) {
  return runtime(run);
}

export { cn };
