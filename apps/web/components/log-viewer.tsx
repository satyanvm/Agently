"use client";

import * as React from "react";
import {
  Search,
  Pause,
  Play,
  ArrowDownToLine,
  ChevronRight,
  AlertTriangle,
  XCircle,
  Cog,
  Bot,
  Wrench,
  Globe,
  Brain,
  WrapText,
  Download,
} from "lucide-react";
import type { LogEntry, LogLevel, LogChannel } from "@/lib/types";
import { cn, formatClock, formatDuration } from "@/lib/utils";

const CHANNEL_META: Record<LogChannel, { icon: typeof Cog; label: string; tone: string }> = {
  system: { icon: Cog, label: "system", tone: "text-slate-500" },
  agent: { icon: Bot, label: "agent", tone: "text-blue-600" },
  tool: { icon: Wrench, label: "tool", tone: "text-fuchsia-600" },
  browser: { icon: Globe, label: "browser", tone: "text-emerald-600" },
  model: { icon: Brain, label: "model", tone: "text-accent" },
};

const LEVEL_META: Record<LogLevel, { tone: string; dot: string; label: string }> = {
  debug: { tone: "text-slate-400", dot: "bg-ghost", label: "debug" },
  info: { tone: "text-slate-600", dot: "bg-neutral", label: "info" },
  success: { tone: "text-emerald-600", dot: "bg-success", label: "ok" },
  warn: { tone: "text-amber-600", dot: "bg-warn", label: "warn" },
  error: { tone: "text-red-600", dot: "bg-danger", label: "error" },
};

const CHANNELS: LogChannel[] = ["system", "agent", "model", "tool", "browser"];

// Pool of plausible live lines appended to simulate realtime streaming.
const LIVE_POOL: Omit<LogEntry, "id" | "seq" | "ts" | "offsetMs" | "runId">[] = [
  { level: "debug", channel: "browser", source: "Navigator", message: "scroll → viewport bottom · 6 images loaded" },
  { level: "info", channel: "tool", source: "Scout · Launches", message: "fetch https://rival.io/blog · 200 · 118ms" },
  { level: "info", channel: "model", source: "Synthesizer", message: "scoring competitive dimensions", reasoning: true },
  { level: "debug", channel: "agent", source: "Conductor", message: "heartbeat · 6 agents healthy" },
  { level: "warn", channel: "browser", source: "Navigator", message: "slow response from nimbus.ai (2.4s)" },
  { level: "info", channel: "browser", source: "Navigator", message: "screenshot captured · nimbus-pricing.png" },
  { level: "success", channel: "tool", source: "Scout · Launches", message: "12 changelog entries parsed" },
];

export function LogViewer({
  logs,
  runStartISO,
  live = true,
  height = "h-[560px]",
}: {
  logs: LogEntry[];
  runStartISO: string;
  live?: boolean;
  height?: string;
}) {
  const [q, setQ] = React.useState("");
  const [activeChannels, setActiveChannels] = React.useState<Set<LogChannel>>(new Set(CHANNELS));
  const [severity, setSeverity] = React.useState<"all" | "warn" | "error">("all");
  const [source, setSource] = React.useState<string>("all");
  const [tailing, setTailing] = React.useState(live);
  const [wrap, setWrap] = React.useState(true);
  const [extra, setExtra] = React.useState<LogEntry[]>([]);
  const [expanded, setExpanded] = React.useState<Set<string>>(new Set());

  const scrollRef = React.useRef<HTMLDivElement>(null);
  const runStart = React.useMemo(() => Date.parse(runStartISO), [runStartISO]);

  const sources = React.useMemo(
    () => Array.from(new Set(logs.map((l) => l.source))),
    [logs],
  );

  // Simulated realtime stream.
  React.useEffect(() => {
    if (!tailing) return;
    let n = 0;
    const id = setInterval(() => {
      const tmpl = LIVE_POOL[n % LIVE_POOL.length]!;
      const now = Date.now();
      setExtra((prev) => [
        ...prev,
        {
          ...tmpl,
          id: `live-${now}-${n}`,
          runId: logs[0]?.runId ?? "run",
          seq: 10000 + n,
          ts: new Date(now).toISOString(),
          offsetMs: now - runStart,
        },
      ]);
      n++;
    }, 1900);
    return () => clearInterval(id);
  }, [tailing, runStart, logs]);

  const all = React.useMemo(() => [...logs, ...extra], [logs, extra]);

  const filtered = React.useMemo(() => {
    const needle = q.toLowerCase();
    return all.filter((l) => {
      if (!activeChannels.has(l.channel)) return false;
      if (severity === "warn" && !(l.level === "warn" || l.level === "error")) return false;
      if (severity === "error" && l.level !== "error") return false;
      if (source !== "all" && l.source !== source) return false;
      if (needle && !(l.message.toLowerCase().includes(needle) || l.source.toLowerCase().includes(needle)))
        return false;
      return true;
    });
  }, [all, q, activeChannels, severity, source]);

  const errors = all.filter((l) => l.level === "error").length;
  const warns = all.filter((l) => l.level === "warn").length;

  // Autoscroll when tailing.
  React.useEffect(() => {
    if (tailing && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [filtered.length, tailing]);

  function toggleChannel(c: LogChannel) {
    setActiveChannels((prev) => {
      const next = new Set(prev);
      if (next.has(c)) next.delete(c);
      else next.add(c);
      return next;
    });
  }

  function toggleExpand(id: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface shadow-card">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-surface-2/40 px-3 py-2">
        <div className="flex h-8 min-w-[200px] flex-1 items-center gap-2 rounded-md border border-border bg-surface px-2.5 focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/20">
          <Search className="size-3.5 text-faint" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search logs…"
            className="h-full w-full bg-transparent font-mono text-[12px] text-fg placeholder:text-faint focus:outline-none"
          />
          {q && (
            <button onClick={() => setQ("")} className="text-faint hover:text-fg">
              <XCircle className="size-3.5" />
            </button>
          )}
        </div>

        {/* severity */}
        <div className="flex items-center gap-0.5 rounded-md border border-border bg-surface p-0.5">
          {(["all", "warn", "error"] as const).map((s) => (
            <button
              key={s}
              onClick={() => setSeverity(s)}
              className={cn(
                "rounded px-2 py-1 text-[11px] font-medium capitalize transition-colors",
                severity === s ? "bg-surface-3 text-fg" : "text-muted hover:text-fg",
              )}
            >
              {s === "all" ? "All" : s === "warn" ? "Warnings" : "Errors"}
            </button>
          ))}
        </div>

        {/* source */}
        <select
          value={source}
          onChange={(e) => setSource(e.target.value)}
          className="h-8 rounded-md border border-border bg-surface px-2 text-[12px] text-muted focus:border-accent/50 focus:outline-none"
        >
          <option value="all">All agents</option>
          {sources.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>

        <div className="ml-auto flex items-center gap-1.5">
          <span className="flex items-center gap-1 rounded bg-danger-bg px-1.5 py-1 text-[11px] tabular-nums text-danger">
            <XCircle className="size-3" /> {errors}
          </span>
          <span className="flex items-center gap-1 rounded bg-warn-bg px-1.5 py-1 text-[11px] tabular-nums text-warn">
            <AlertTriangle className="size-3" /> {warns}
          </span>
          <button
            onClick={() => setWrap((w) => !w)}
            className={cn(
              "flex h-8 w-8 items-center justify-center rounded-md border border-border transition-colors",
              wrap ? "bg-surface-3 text-fg" : "bg-surface text-muted hover:text-fg",
            )}
            title="Toggle wrap"
          >
            <WrapText className="size-3.5" />
          </button>
          <button
            className="flex h-8 w-8 items-center justify-center rounded-md border border-border bg-surface text-muted transition-colors hover:text-fg"
            title="Download"
          >
            <Download className="size-3.5" />
          </button>
          <button
            onClick={() => setTailing((t) => !t)}
            className={cn(
              "flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[12px] font-medium transition-colors",
              tailing
                ? "border-running/40 bg-running-bg text-running"
                : "border-border bg-surface text-muted hover:text-fg",
            )}
          >
            {tailing ? <Pause className="size-3.5" /> : <Play className="size-3.5" />}
            {tailing ? "Live" : "Paused"}
          </button>
        </div>
      </div>

      {/* Channel chips */}
      <div className="flex flex-wrap items-center gap-1.5 border-b border-border px-3 py-2">
        <span className="mr-1 text-[10px] font-medium uppercase tracking-wider text-ghost">
          Channels
        </span>
        {CHANNELS.map((c) => {
          const meta = CHANNEL_META[c];
          const Icon = meta.icon;
          const on = activeChannels.has(c);
          return (
            <button
              key={c}
              onClick={() => toggleChannel(c)}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium transition-colors",
                on
                  ? "border-border-strong bg-surface-2 text-fg"
                  : "border-border bg-surface text-ghost hover:text-muted",
              )}
            >
              <Icon className={cn("size-3", on ? meta.tone : "text-ghost")} />
              {meta.label}
            </button>
          );
        })}
      </div>

      {/* Stream */}
      <div
        ref={scrollRef}
        className={cn("overflow-auto bg-inset font-mono text-[12px] leading-[1.7]", height)}
      >
        {filtered.map((l) => (
          <LogLine
            key={l.id}
            entry={l}
            runStart={runStart}
            wrap={wrap}
            expanded={expanded.has(l.id)}
            onToggle={() => toggleExpand(l.id)}
          />
        ))}
        {filtered.length === 0 && (
          <div className="px-4 py-16 text-center text-faint">No log lines match your filters.</div>
        )}
        {tailing && (
          <div className="flex items-center gap-2 px-4 py-2 text-[11px] text-running">
            <span className="relative flex h-1.5 w-1.5">
              <span className="absolute inline-flex h-full w-full rounded-full bg-running opacity-60 live-dot" />
              <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-running" />
            </span>
            streaming live
            <span className="caret text-running">▍</span>
          </div>
        )}
      </div>
    </div>
  );
}

function LogLine({
  entry,
  runStart,
  wrap,
  expanded,
  onToggle,
}: {
  entry: LogEntry;
  runStart: number;
  wrap: boolean;
  expanded: boolean;
  onToggle: () => void;
}) {
  const ch = CHANNEL_META[entry.channel];
  const lv = LEVEL_META[entry.level];
  const ChIcon = ch.icon;
  const isError = entry.level === "error";
  const isWarn = entry.level === "warn";
  const offset = Date.parse(entry.ts) - runStart;
  const hasDetail = !!entry.detail;

  return (
    <div
      className={cn(
        "group flex items-start gap-3 border-l-2 px-3 py-[3px] transition-colors hover:bg-surface/40",
        isError
          ? "border-danger/70 bg-danger-bg/30"
          : isWarn
            ? "border-warn/60 bg-warn-bg/20"
            : "border-transparent",
      )}
    >
      {/* timestamp */}
      <span className="shrink-0 select-none text-faint">{formatClock(entry.ts)}</span>
      {/* offset */}
      <span className="hidden w-12 shrink-0 select-none text-right tabular-nums text-ghost sm:inline">
        +{formatDuration(Math.max(0, offset))}
      </span>
      {/* channel */}
      <span className="flex w-[78px] shrink-0 items-center gap-1.5">
        <ChIcon className={cn("size-3", ch.tone)} />
        <span className={cn("truncate text-[11px]", ch.tone)}>{ch.label}</span>
      </span>
      {/* source */}
      <span className="hidden w-[120px] shrink-0 truncate text-muted md:inline" title={entry.source}>
        {entry.source}
      </span>
      {/* message */}
      <div className="min-w-0 flex-1">
        <div className={cn("flex items-start gap-1.5", !wrap && "truncate")}>
          {hasDetail && (
            <button onClick={onToggle} className="mt-0.5 shrink-0 text-faint hover:text-fg">
              <ChevronRight className={cn("size-3 transition-transform", expanded && "rotate-90")} />
            </button>
          )}
          <span className={cn(lv.tone, !wrap && "truncate", entry.reasoning && "italic")}>
            {entry.reasoning && <span className="mr-1 text-accent-soft/70">⟢</span>}
            {entry.message}
          </span>
        </div>
        {hasDetail && expanded && (
          <pre className="mt-1 whitespace-pre-wrap rounded-md border border-border bg-surface-2/40 px-3 py-2 text-[11px] leading-relaxed text-faint">
            {entry.detail}
          </pre>
        )}
      </div>
    </div>
  );
}
