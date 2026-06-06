import { cn } from "@/lib/utils";

/** A compact KPI tile: label, big value, optional delta + trailing visual. */
export function Stat({
  label,
  value,
  sub,
  delta,
  icon,
  visual,
  className,
}: {
  label: string;
  value: React.ReactNode;
  sub?: React.ReactNode;
  delta?: { value: string; positive?: boolean };
  icon?: React.ReactNode;
  visual?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col gap-3 rounded-lg border border-border bg-surface p-4 shadow-card", className)}>
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-2 text-[12px] font-medium text-muted">
          {icon}
          {label}
        </span>
        {delta && (
          <span
            className={cn(
              "rounded px-1.5 py-0.5 text-[11px] font-medium tabular-nums",
              delta.positive ? "bg-success-bg text-success" : "bg-danger-bg text-danger",
            )}
          >
            {delta.value}
          </span>
        )}
      </div>
      <div className="flex items-end justify-between gap-3">
        <div>
          <div className="text-2xl font-semibold tracking-tight tabular-nums text-fg">
            {value}
          </div>
          {sub && <div className="mt-0.5 text-[12px] text-faint">{sub}</div>}
        </div>
        {visual && <div className="shrink-0">{visual}</div>}
      </div>
    </div>
  );
}
