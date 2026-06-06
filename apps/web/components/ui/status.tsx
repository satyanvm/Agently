import { cn } from "@/lib/utils";
import type { RunStatus, AgentStatus } from "@/lib/types";

type AnyStatus = RunStatus | AgentStatus;

interface StatusMeta {
  label: string;
  /** text/icon color */
  fg: string;
  /** soft background */
  bg: string;
  dot: string;
  pulse?: boolean;
}

const STATUS: Record<AnyStatus, StatusMeta> = {
  // run
  queued: { label: "Queued", fg: "text-muted", bg: "bg-neutral-bg", dot: "bg-neutral" },
  running: { label: "Running", fg: "text-running", bg: "bg-running-bg", dot: "bg-running", pulse: true },
  succeeded: { label: "Succeeded", fg: "text-success", bg: "bg-success-bg", dot: "bg-success" },
  failed: { label: "Failed", fg: "text-danger", bg: "bg-danger-bg", dot: "bg-danger" },
  canceled: { label: "Canceled", fg: "text-faint", bg: "bg-neutral-bg", dot: "bg-faint" },
  paused: { label: "Paused", fg: "text-warn", bg: "bg-warn-bg", dot: "bg-warn" },
  // agent
  idle: { label: "Idle", fg: "text-faint", bg: "bg-neutral-bg", dot: "bg-ghost" },
  blocked: { label: "Blocked", fg: "text-danger", bg: "bg-danger-bg", dot: "bg-danger" },
  waiting: { label: "Waiting", fg: "text-warn", bg: "bg-warn-bg", dot: "bg-warn", pulse: true },
};

export function statusMeta(s: AnyStatus): StatusMeta {
  return STATUS[s] ?? STATUS.idle;
}

export function StatusDot({
  status,
  className,
}: {
  status: AnyStatus;
  className?: string;
}) {
  const m = statusMeta(status);
  return (
    <span className={cn("relative inline-flex h-2 w-2 shrink-0", className)}>
      {m.pulse && (
        <span
          className={cn("absolute inset-0 rounded-full opacity-60", m.dot)}
          style={{ animation: "pulse-ring 1.8s var(--ease-out-soft) infinite" }}
        />
      )}
      <span className={cn("relative inline-flex h-2 w-2 rounded-full", m.dot, m.pulse && "live-dot")} />
    </span>
  );
}

export function StatusBadge({
  status,
  className,
  size = "md",
}: {
  status: AnyStatus;
  className?: string;
  size?: "sm" | "md";
}) {
  const m = statusMeta(status);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full font-medium tabular-nums",
        m.bg,
        m.fg,
        size === "sm" ? "px-2 py-0.5 text-[11px]" : "px-2.5 py-1 text-xs",
        className,
      )}
    >
      <StatusDot status={status} />
      {m.label}
    </span>
  );
}
