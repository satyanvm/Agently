import { cn } from "@/lib/utils";

export function Progress({
  value,
  className,
  tone = "accent",
  showShimmer = false,
}: {
  value: number; // 0..1
  className?: string;
  tone?: "accent" | "success" | "running" | "warn" | "danger";
  showShimmer?: boolean;
}) {
  const pct = Math.max(0, Math.min(1, value)) * 100;
  const bg =
    tone === "success"
      ? "bg-success"
      : tone === "running"
        ? "bg-running"
        : tone === "warn"
          ? "bg-warn"
          : tone === "danger"
            ? "bg-danger"
            : "bg-accent";
  return (
    <div className={cn("relative h-1.5 w-full overflow-hidden rounded-full bg-surface-3", className)}>
      <div
        className={cn("relative h-full rounded-full transition-[width] duration-700 ease-out", bg)}
        style={{ width: `${pct}%` }}
      >
        {showShimmer && (
          <div
            className="absolute inset-0 -translate-x-full"
            style={{
              background:
                "linear-gradient(90deg, transparent, rgba(255,255,255,0.45), transparent)",
              animation: "shimmer 1.6s ease-in-out infinite",
            }}
          />
        )}
      </div>
    </div>
  );
}
