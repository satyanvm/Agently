"use client";

import * as React from "react";
import {
  Lock,
  RotateCw,
  ArrowLeft,
  ArrowRight,
  MousePointerClick,
  Keyboard,
  Navigation,
  ScrollText,
  Camera,
  Download as Extract,
  Clock3,
  Globe,
  Maximize2,
  AlertTriangle,
} from "lucide-react";
import type { BrowserSession, BrowserActionType } from "@/lib/types";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { StatusBadge } from "@/components/ui/status";
import { Segmented } from "@/components/ui/segmented";
import { LivePill } from "@/components/live";
import { cn, formatClock, formatDuration } from "@/lib/utils";

const ACTION_META: Record<BrowserActionType, { icon: typeof Navigation; tone: string; label: string }> = {
  navigate: { icon: Navigation, tone: "text-sky-600", label: "Navigate" },
  click: { icon: MousePointerClick, tone: "text-accent-soft", label: "Click" },
  type: { icon: Keyboard, tone: "text-emerald-600", label: "Type" },
  scroll: { icon: ScrollText, tone: "text-muted", label: "Scroll" },
  extract: { icon: Extract, tone: "text-amber-600", label: "Extract" },
  wait: { icon: Clock3, tone: "text-faint", label: "Wait" },
  screenshot: { icon: Camera, tone: "text-fuchsia-600", label: "Screenshot" },
  submit: { icon: ArrowRight, tone: "text-teal-600", label: "Submit" },
};

export function BrowserSessionView({ session }: { session: BrowserSession }) {
  const hasShots = session.shots.length > 0;
  const [shotIdx, setShotIdx] = React.useState(Math.max(0, session.shots.length - 1));
  const [tab, setTab] = React.useState<"actions" | "console">("actions");
  // A session can exist with no screenshots (e.g. a real browse that navigated but
  // captured no frames). Guard the viewport so the tab still renders its actions/
  // console instead of crashing on an undefined shot.
  const shot = session.shots[shotIdx];

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
      {/* Viewport */}
      <div className="space-y-3">
        <Card className="overflow-hidden p-0">
          {/* browser chrome */}
          <div className="flex items-center gap-2 border-b border-border bg-surface-2/60 px-3 py-2">
            <div className="flex items-center gap-1.5 text-faint">
              <ArrowLeft className="size-3.5" />
              <ArrowRight className="size-3.5 opacity-40" />
              <RotateCw className="size-3.5" />
            </div>
            <div className="flex h-7 flex-1 items-center gap-2 rounded-md border border-border bg-inset px-2.5">
              <Lock className="size-3 text-success" />
              <span className="truncate font-mono text-[11px] text-muted">
                {shot?.url ?? session.currentUrl ?? "—"}
              </span>
            </div>
            <Badge variant="neutral" size="sm">
              {session.viewport.w}×{session.viewport.h}
            </Badge>
          </div>

          {/* fake page */}
          <div className={cn("relative aspect-[16/10] w-full bg-gradient-to-br", shot?.tone ?? "from-surface-2 to-surface")}>
            <div className="absolute inset-0 bg-grid opacity-30" />
            {/* faux page skeleton */}
            <div className="absolute inset-0 flex flex-col gap-3 p-6">
              <div className="flex items-center justify-between">
                <div className="h-3 w-28 rounded-full bg-white/15" />
                <div className="flex gap-2">
                  <div className="h-3 w-12 rounded-full bg-white/10" />
                  <div className="h-3 w-12 rounded-full bg-white/10" />
                  <div className="h-6 w-16 rounded-md bg-white/20" />
                </div>
              </div>
              <div className="mt-6 h-6 w-2/3 rounded-md bg-white/20" />
              <div className="h-3 w-1/2 rounded-full bg-white/10" />
              <div className="mt-4 grid grid-cols-3 gap-3">
                {[0, 1, 2].map((i) => (
                  <div key={i} className="h-24 rounded-lg bg-white/10 ring-1 ring-white/10" />
                ))}
              </div>
            </div>
            {/* overlay meta */}
            <div className="absolute bottom-3 left-3 flex items-center gap-2">
              <span className="rounded-md bg-black/50 px-2 py-1 text-[11px] text-white/80 backdrop-blur">
                {shot?.label ?? "No screenshot captured"}
              </span>
            </div>
            <div className="absolute right-3 top-3 flex items-center gap-2">
              {session.status === "running" && <LivePill />}
              <button className="flex h-7 w-7 items-center justify-center rounded-md bg-black/50 text-white/80 backdrop-blur hover:bg-black/70">
                <Maximize2 className="size-3.5" />
              </button>
            </div>
            {/* fake cursor */}
            <MousePointerClick className="absolute left-[44%] top-[58%] size-5 text-white drop-shadow-[0_2px_6px_rgba(0,0,0,0.6)]" />
          </div>
        </Card>

        {/* Filmstrip / timeline scrubber */}
        <div>
          <div className="mb-2 flex items-center justify-between">
            <span className="text-[11px] font-medium uppercase tracking-wider text-ghost">
              Browser timeline
            </span>
            <span className="font-mono text-[11px] text-faint">{shot ? formatClock(shot.ts) : "—"}</span>
          </div>
          <div className="flex gap-2 overflow-x-auto pb-1">
            {!hasShots && (
              <div className="flex h-16 w-full items-center justify-center rounded-md border border-dashed border-border text-[11px] text-faint">
                No screenshots captured for this session
              </div>
            )}
            {session.shots.map((s, i) => (
              <button
                key={s.id}
                onClick={() => setShotIdx(i)}
                className={cn(
                  "group relative aspect-[16/10] w-28 shrink-0 overflow-hidden rounded-md border transition-all",
                  i === shotIdx
                    ? "border-accent ring-2 ring-accent/30"
                    : "border-border opacity-70 hover:opacity-100",
                )}
              >
                <span className={cn("absolute inset-0 bg-gradient-to-br", s.tone)} />
                <span className="absolute inset-0 bg-grid opacity-30" />
                <span className="absolute bottom-1 left-1 right-1 truncate rounded bg-black/50 px-1 py-0.5 text-left text-[9px] text-white/80 backdrop-blur">
                  {formatClock(s.ts)}
                </span>
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Actions + console */}
      <Card className="flex flex-col">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <Globe className="size-4 text-emerald-600" />
            <h3 className="text-[13px] font-semibold text-fg">{session.agentName}</h3>
            <StatusBadge status={session.status} size="sm" />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-px border-b border-border bg-border text-center">
          <SessionStat label="Pages visited" value={`${session.pagesVisited}`} />
          <SessionStat label="Actions" value={`${session.actionsCount}`} />
        </div>
        <div className="border-b border-border px-4 py-2.5">
          <Segmented<"actions" | "console">
            size="sm"
            value={tab}
            onChange={setTab}
            options={[
              { value: "actions", label: "Actions", count: session.actions.length },
              { value: "console", label: "Console", count: session.console.length },
            ]}
          />
        </div>

        {tab === "actions" ? (
          <div className="max-h-[460px] flex-1 overflow-y-auto p-2">
            {session.actions.map((a) => {
              const m = ACTION_META[a.type];
              const Icon = m.icon;
              const failed = a.status === "error";
              return (
                <div
                  key={a.id}
                  className={cn(
                    "flex items-start gap-2.5 rounded-md px-2.5 py-2",
                    failed ? "bg-danger-bg/40" : "hover:bg-surface-2/50",
                  )}
                >
                  <span
                    className={cn(
                      "mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-md ring-1",
                      failed ? "bg-danger-bg ring-danger/30" : "bg-surface-2 ring-border",
                    )}
                  >
                    <Icon className={cn("size-3.5", failed ? "text-danger" : m.tone)} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-[12px] font-medium text-fg">{m.label}</span>
                      <span className="font-mono text-[10px] text-ghost">{formatClock(a.ts)}</span>
                      {failed && <AlertTriangle className="size-3 text-danger" />}
                    </div>
                    <div className="truncate font-mono text-[11px] text-muted" title={a.target}>
                      {a.target}
                    </div>
                    {a.value && <div className="text-[11px] text-faint">{a.value}</div>}
                  </div>
                  <span className={cn("shrink-0 text-[10px] tabular-nums", failed ? "text-danger" : "text-ghost")}>
                    {formatDuration(a.durationMs)}
                  </span>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="max-h-[460px] flex-1 overflow-y-auto bg-inset p-2 font-mono text-[11px] leading-relaxed">
            {session.console.map((c, i) => (
              <div
                key={i}
                className={cn(
                  "flex gap-2 px-1.5 py-0.5",
                  c.level === "error" && "text-danger",
                  c.level === "warn" && "text-warn",
                  (c.level === "info" || c.level === "debug" || c.level === "success") && "text-muted",
                )}
              >
                <span className="text-ghost">{formatClock(c.ts)}</span>
                <span>{c.text}</span>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function SessionStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface px-4 py-2.5">
      <div className="text-[16px] font-semibold tabular-nums text-fg">{value}</div>
      <div className="text-[10px] text-faint">{label}</div>
    </div>
  );
}
