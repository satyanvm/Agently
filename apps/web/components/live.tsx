"use client";

import * as React from "react";
import { formatDuration, formatCost } from "@/lib/utils";

/** Ticks elapsed time from a fixed start, only after mount (SSR-stable). */
export function LiveTimer({
  startISO,
  baseMs,
  className,
}: {
  startISO: string;
  /** Deterministic value rendered on the server / first paint. */
  baseMs: number;
  className?: string;
}) {
  const [ms, setMs] = React.useState(baseMs);
  const start = React.useMemo(() => Date.parse(startISO), [startISO]);

  React.useEffect(() => {
    const id = setInterval(() => setMs(Date.now() - start), 1000);
    return () => clearInterval(id);
  }, [start]);

  return (
    <span className={className} suppressHydrationWarning>
      {formatDuration(ms)}
    </span>
  );
}

/** Slowly accrues cost to feel live, anchored to a base value. */
export function LiveCost({
  base,
  ratePerSec = 0.0021,
  className,
}: {
  base: number;
  ratePerSec?: number;
  className?: string;
}) {
  const [v, setV] = React.useState(base);
  React.useEffect(() => {
    const id = setInterval(() => setV((c) => c + ratePerSec), 1000);
    return () => clearInterval(id);
  }, [ratePerSec]);
  return (
    <span className={className} suppressHydrationWarning>
      {formatCost(v)}
    </span>
  );
}

/** A small "Live" pill with a breathing dot. */
export function LivePill({ label = "Live" }: { label?: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-running-bg px-2 py-0.5 text-[11px] font-medium text-running">
      <span className="relative flex h-1.5 w-1.5">
        <span className="absolute inline-flex h-full w-full rounded-full bg-running opacity-60 live-dot" />
        <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-running" />
      </span>
      {label}
    </span>
  );
}
