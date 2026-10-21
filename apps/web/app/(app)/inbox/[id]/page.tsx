"use client";

import * as React from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  Check,
  X,
  Radar,
  ExternalLink,
  Play,
  FileText,
  Gavel,
  CircleDot,
  Flag,
  RotateCcw,
} from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, SectionHeading } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { ApprovalStatusBadge } from "@/components/approval-status";
import {
  apiErrorMessage,
  decideApproval,
  fetchApprovalHistory,
  rememberName,
  rememberedName,
  type ApprovalHistory,
  type HistoryEvent,
} from "@/lib/marketing";
import { cn, formatCost, timeAgo } from "@/lib/utils";

const LANGFUSE_HOST = process.env.NEXT_PUBLIC_LANGFUSE_HOST ?? "http://localhost:3001";

const TEXTAREA =
  "w-full rounded-md border border-border bg-surface px-3 py-2.5 text-[13px] leading-relaxed text-fg placeholder:text-faint/70 transition-colors focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25 disabled:opacity-70";

export default function ApprovalPage() {
  const { id } = useParams<{ id: string }>();
  const [h, setH] = React.useState<ApprovalHistory | null>(null);
  const [state, setState] = React.useState<"loading" | "ready" | "notfound" | "error">("loading");
  const [loadError, setLoadError] = React.useState("");
  const [text, setText] = React.useState("");
  const [name, setName] = React.useState("");
  const [note, setNote] = React.useState("");
  const [busy, setBusy] = React.useState<"" | "approved" | "rejected">("");
  const [actionError, setActionError] = React.useState("");
  const seeded = React.useRef(false);

  React.useEffect(() => setName(rememberedName()), []);

  React.useEffect(() => {
    if (!id) return;
    let active = true;
    const load = () =>
      fetchApprovalHistory(id)
        .then((d) => {
          if (!active) return;
          setH(d);
          setState("ready");
          if (!seeded.current) {
            setText(d.finalContent ?? d.draft);
            seeded.current = true;
          }
        })
        .catch((e) => {
          if (!active) return;
          const msg = e instanceof Error ? e.message : String(e);
          if (msg.includes("API 404")) setState("notfound");
          else {
            setLoadError(msg);
            setState((s) => (s === "ready" ? s : "error"));
          }
        });
    load();
    // Keep polling: after a decision the run resumes and outcomes arrive.
    const t = setInterval(load, 4000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [id]);

  if (state === "notfound") {
    return (
      <>
        <TopBar crumbs={[{ label: "Inbox", href: "/inbox" }, { label: "Not found" }]} />
        <PageContainer>
          <p className="text-sm text-muted">This approval doesn&apos;t exist.</p>
        </PageContainer>
      </>
    );
  }
  if (!h) {
    return (
      <>
        <TopBar crumbs={[{ label: "Inbox", href: "/inbox" }, { label: "Loading…" }]} />
        <PageContainer>
          <p className="text-sm text-faint">{state === "error" ? loadError : "Loading…"}</p>
        </PageContainer>
      </>
    );
  }

  const pending = h.status === "pending";
  const edited = text !== h.draft;

  async function decide(decision: "approved" | "rejected") {
    const who = name.trim();
    if (!who) {
      setActionError("Enter your name so the decision is recorded against you.");
      return;
    }
    if (decision === "approved" && !text.trim()) {
      setActionError("The text is empty. Reject the draft instead.");
      return;
    }
    setBusy(decision);
    setActionError("");
    try {
      rememberName(who);
      await decideApproval(h!.id, {
        decision,
        decidedBy: who,
        ...(decision === "approved" && edited ? { content: text } : {}),
        ...(note.trim() ? { note: note.trim() } : {}),
      });
      setH(await fetchApprovalHistory(h!.id));
    } catch (e) {
      setActionError(apiErrorMessage(e));
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <TopBar crumbs={[{ label: "Inbox", href: "/inbox" }, { label: h.title || "Draft" }]} />
      <PageContainer>
        <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-[22px] font-semibold tracking-tight text-fg">{h.title || "Draft"}</h1>
              <ApprovalStatusBadge status={h.status} />
            </div>
            <p className="mt-1 text-[13px] text-muted">
              {[h.channel && `Publishes to ${h.channel}`, h.assignee && `for ${h.assignee}`, `opened ${timeAgo(h.createdAt)}`]
                .filter(Boolean)
                .join(" · ")}
              {pending && h.expiresAt && ` · times out ${timeUntil(h.expiresAt)}`}
            </p>
          </div>
        </div>

        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_360px]">
          {/* Draft + decision */}
          <div className="min-w-0 space-y-4">
            <Card className="p-4">
              <div className="mb-2 flex items-center justify-between">
                <SectionHeading className="mb-0">{pending ? "Draft" : "Final text"}</SectionHeading>
                {pending && edited && (
                  <button
                    onClick={() => setText(h.draft)}
                    className="inline-flex items-center gap-1 text-[12px] text-faint hover:text-fg"
                  >
                    <RotateCcw className="size-3" /> Undo edits
                  </button>
                )}
              </div>
              {pending ? (
                <textarea
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  rows={Math.min(24, Math.max(8, text.split("\n").length + 2))}
                  className={TEXTAREA}
                  aria-label="Draft text"
                />
              ) : (
                <div className="whitespace-pre-wrap rounded-md bg-surface-2/50 px-3 py-2.5 text-[13px] leading-relaxed text-fg">
                  {h.status === "approved" ? h.finalContent : h.draft}
                </div>
              )}
              {!pending && h.status === "approved" && h.edited && (
                <details className="mt-3 text-[12.5px] text-muted">
                  <summary className="cursor-pointer text-faint hover:text-fg">Show the original draft</summary>
                  <div className="mt-2 whitespace-pre-wrap rounded-md border border-border px-3 py-2.5">{h.draft}</div>
                </details>
              )}
            </Card>

            {pending ? (
              <Card className="p-4">
                <div className="grid gap-3 sm:grid-cols-2">
                  <label className="block">
                    <span className="mb-1 block text-[12px] font-medium text-muted">Your name</span>
                    <Input
                      value={name}
                      onChange={(e) => {
                        setName(e.target.value);
                        setActionError("");
                      }}
                      placeholder="Recorded with the decision"
                    />
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-[12px] font-medium text-muted">Note (optional)</span>
                    <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Why, for the history" />
                  </label>
                </div>
                {actionError && <p className="mt-3 text-[12.5px] text-danger">{actionError}</p>}
                <div className="mt-4 flex flex-wrap items-center gap-2">
                  <Button variant="primary" onClick={() => decide("approved")} disabled={busy !== ""}>
                    <Check /> {busy === "approved" ? "Approving…" : edited ? "Approve with edits" : "Approve"}
                  </Button>
                  <Button variant="danger" onClick={() => decide("rejected")} disabled={busy !== ""}>
                    <X /> {busy === "rejected" ? "Rejecting…" : "Reject"}
                  </Button>
                  <span className="text-[12px] text-faint">
                    Approving lets the playbook continue and publish this text. Rejecting skips everything after it.
                  </span>
                </div>
              </Card>
            ) : (
              <Card className="p-4 text-[13px] text-muted">
                {h.status === "timed_out"
                  ? "Nobody decided before the timeout, so nothing after it ran."
                  : `${h.status === "approved" ? "Approved" : "Rejected"} by ${h.decidedBy ?? "someone"}${
                      h.decidedAt ? ` ${timeAgo(h.decidedAt)}` : ""
                    }${h.decisionNote ? ` — “${h.decisionNote}”` : ""}.`}
                {h.status === "approved" && !h.signaledAt && " The playbook is picking up the decision…"}
              </Card>
            )}

            <Card className="p-4">
              <SectionHeading>History</SectionHeading>
              <Timeline events={h.timeline} />
            </Card>
          </div>

          {/* Why + where it went */}
          <div className="min-w-0 space-y-4">
            <Card className="p-4">
              <SectionHeading>Why this draft exists</SectionHeading>
              {h.signals.length === 0 ? (
                <p className="text-[12.5px] text-faint">No signal was recorded by the run that drafted this.</p>
              ) : (
                <ul className="space-y-3">
                  {h.signals.map((s) => (
                    <li key={s.id} className="text-[12.5px]">
                      <div className="flex items-center gap-1.5 font-medium text-fg">
                        <Radar className="size-3.5 text-success" />
                        {s.title}
                      </div>
                      <div className="mt-0.5 text-faint">
                        {s.source.replace("_", " ")} · {timeAgo(s.detectedAt)}
                        {s.summary && ` · ${s.summary}`}
                      </div>
                      <SignalDiff detail={s.detail} />
                      {s.url && <ExtLink href={s.url}>{s.url}</ExtLink>}
                    </li>
                  ))}
                </ul>
              )}
            </Card>

            {h.run && (
              <Card className="p-4 text-[12.5px]">
                <SectionHeading>Drafted by</SectionHeading>
                <Link href={`/runs/${h.run.id}`} className="font-medium text-accent-soft hover:underline">
                  {h.run.workflowName} · run #{h.run.number}
                </Link>
                <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-muted">
                  <dt className="text-faint">Status</dt>
                  <dd>{h.run.status}</dd>
                  <dt className="text-faint">Run cost</dt>
                  <dd>
                    {formatCost(h.run.costUsd)}
                    <span className="text-faint"> (whole run, shared by its drafts)</span>
                  </dd>
                  {h.run.langfuseTraceId && (
                    <>
                      <dt className="text-faint">Trace</dt>
                      <dd>
                        <ExtLink href={`${LANGFUSE_HOST}/project?session=${encodeURIComponent(h.run.langfuseTraceId)}`}>
                          Langfuse
                        </ExtLink>
                      </dd>
                    </>
                  )}
                </dl>
              </Card>
            )}

            <Card className="p-4 text-[12.5px]">
              <SectionHeading>Published</SectionHeading>
              {h.published.length === 0 ? (
                <p className="text-faint">
                  {h.status === "approved" && !["succeeded", "failed", "canceled"].includes(h.run?.status ?? "")
                    ? "Nothing has gone out yet."
                    : "Nothing went out."}
                </p>
              ) : (
                <ul className="space-y-2">
                  {h.published.map((o) => (
                    <li key={o.id}>
                      <span className="font-medium text-fg">{o.nodeKey}</span>{" "}
                      <span className="text-faint">{o.nodeType}</span>
                      {o.summary && <div className="text-muted">{o.summary}</div>}
                      {o.url && <ExtLink href={o.url}>{o.url}</ExtLink>}
                    </li>
                  ))}
                </ul>
              )}
            </Card>

            {(h.notify.targets?.length ?? 0) > 0 && (
              <Card className="p-4 text-[12.5px]">
                <SectionHeading>Notified</SectionHeading>
                <ul className="space-y-1">
                  {h.notify.targets!.map((t, i) => (
                    <li key={i} className={t.ok ? "text-muted" : "text-danger"}>
                      {t.ok ? "Sent to " : "Failed: "}
                      {t.target}
                      {!t.ok && ` — ${t.detail}`}
                    </li>
                  ))}
                </ul>
              </Card>
            )}
          </div>
        </div>
      </PageContainer>
    </>
  );
}

function ExtLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="mt-0.5 inline-flex max-w-full items-center gap-1 break-all text-accent-soft hover:underline"
    >
      {children}
      <ExternalLink className="size-3 shrink-0" />
    </a>
  );
}

/** The added/removed lines a page_change signal carries. */
function SignalDiff({ detail }: { detail: Record<string, unknown> }) {
  const added = Array.isArray(detail.added) ? (detail.added as string[]) : [];
  const removed = Array.isArray(detail.removed) ? (detail.removed as string[]) : [];
  if (!added.length && !removed.length) return null;
  return (
    <div className="mt-1.5 max-h-48 overflow-auto rounded-md border border-border bg-surface-2/40 px-2 py-1.5 font-mono text-[11.5px] leading-relaxed">
      {removed.slice(0, 20).map((l, i) => (
        <div key={`r${i}`} className="text-danger">
          − {l}
        </div>
      ))}
      {added.slice(0, 20).map((l, i) => (
        <div key={`a${i}`} className="text-success">
          + {l}
        </div>
      ))}
    </div>
  );
}

const EVENT_ICON: Record<HistoryEvent["kind"], typeof Radar> = {
  signal: Radar,
  run_started: Play,
  draft: FileText,
  decision: Gavel,
  outcome: Flag,
  run_finished: CircleDot,
};

function Timeline({ events }: { events: HistoryEvent[] }) {
  if (events.length === 0) return <p className="text-[12.5px] text-faint">Nothing recorded yet.</p>;
  return (
    <ol className="relative space-y-3 border-l border-border pl-5">
      {events.map((e, i) => {
        const Icon = EVENT_ICON[e.kind] ?? CircleDot;
        const bad = e.status === "failed" || e.status === "rejected";
        return (
          <li key={`${e.kind}-${e.ref ?? i}-${i}`} className="relative text-[12.5px]">
            <span className="absolute -left-[27px] flex h-[18px] w-[18px] items-center justify-center rounded-full bg-surface ring-1 ring-border">
              <Icon className={cn("size-3", bad ? "text-danger" : "text-faint")} />
            </span>
            <div className="flex flex-wrap items-center gap-2">
              <span className={cn("font-medium", bad ? "text-danger" : "text-fg")}>{e.title}</span>
              {e.kind === "outcome" && e.status && (
                <Badge size="sm" variant={e.status === "succeeded" ? "success" : e.status === "failed" ? "danger" : "neutral"}>
                  {e.status}
                </Badge>
              )}
              <span className="text-[11px] text-ghost" title={e.at}>
                {timeAgo(e.at)}
              </span>
            </div>
            {e.detail && <div className="mt-0.5 text-muted">{e.detail}</div>}
            {e.url && <ExtLink href={e.url}>{e.url}</ExtLink>}
          </li>
        );
      })}
    </ol>
  );
}

function timeUntil(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now();
  if (ms <= 0) return "now";
  const h = Math.round(ms / 3_600_000);
  if (h < 1) return `in ${Math.max(1, Math.round(ms / 60_000))} min`;
  if (h < 48) return `in ${h} h`;
  return `in ${Math.round(h / 24)} days`;
}
