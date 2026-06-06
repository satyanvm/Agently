import { cn } from "@/lib/utils";

/** Lightweight inline SVG sparkline / area chart. Deterministic, no deps. */
export function Sparkline({
  data,
  className,
  stroke = "var(--color-accent-soft)",
  fill = true,
  width = 120,
  height = 32,
  strokeWidth = 1.5,
}: {
  data: number[];
  className?: string;
  stroke?: string;
  fill?: boolean;
  width?: number;
  height?: number;
  strokeWidth?: number;
}) {
  if (data.length === 0) return null;
  const max = Math.max(...data, 1);
  const min = Math.min(...data, 0);
  const range = max - min || 1;
  const step = width / (data.length - 1 || 1);
  const pts = data.map((d, i) => {
    const x = i * step;
    const y = height - ((d - min) / range) * (height - strokeWidth * 2) - strokeWidth;
    return [x, y] as const;
  });
  const line = pts.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `${line} L${width},${height} L0,${height} Z`;
  const gid = `spark-${data.length}-${Math.round(max)}-${Math.round(min)}`;

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      className={cn("overflow-visible", className)}
      preserveAspectRatio="none"
      width={width}
      height={height}
    >
      <defs>
        <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={stroke} stopOpacity="0.22" />
          <stop offset="100%" stopColor={stroke} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill && <path d={area} fill={`url(#${gid})`} />}
      <path d={line} fill="none" stroke={stroke} strokeWidth={strokeWidth} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/** A column/bar micro-chart used for run volume & spend. */
export function BarChart({
  data,
  className,
  height = 56,
  accent = "var(--color-accent)",
  highlightLast = false,
}: {
  data: number[];
  className?: string;
  height?: number;
  accent?: string;
  highlightLast?: boolean;
}) {
  const max = Math.max(...data, 1);
  return (
    <div className={cn("flex items-end gap-[3px]", className)} style={{ height }}>
      {data.map((d, i) => {
        const h = Math.max((d / max) * 100, 3);
        const isLast = highlightLast && i === data.length - 1;
        return (
          <div
            key={i}
            className="group relative flex-1 rounded-[2px] transition-colors"
            style={{
              height: `${h}%`,
              background: isLast ? accent : "color-mix(in oklab, var(--color-accent) 38%, transparent)",
            }}
          />
        );
      })}
    </div>
  );
}

/** Health bar: a row of pass/fail ticks (last N runs). */
export function HealthBar({ recent, className }: { recent: (0 | 1)[]; className?: string }) {
  return (
    <div className={cn("flex items-center gap-[3px]", className)}>
      {recent.map((r, i) => (
        <span
          key={i}
          className={cn(
            "h-4 w-1 rounded-full",
            r === 1 ? "bg-success/70" : "bg-danger/80",
          )}
          title={r === 1 ? "Succeeded" : "Failed"}
        />
      ))}
    </div>
  );
}
