"use client";

import * as React from "react";
import Link from "next/link";
import { Inbox as InboxIcon, Radar, ArrowRight } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Segmented } from "@/components/ui/segmented";
import { ApprovalStatusBadge } from "@/components/approval-status";
import { fetchApprovals, type ApprovalListItem, type ApprovalStatus } from "@/lib/marketing";
import { timeAgo } from "@/lib/utils";

type Filter = "pending" | "decided" | "all";

export default function InboxPage() {
  const [items, setItems] = React.useState<ApprovalListItem[] | null>(null);
  const [error, setError] = React.useState("");
  const [filter, setFilter] = React.useState<Filter>("pending");

  React.useEffect(() => {
    let active = true;
    const load = () =>
      fetchApprovals()
        .then((rows) => {
          if (!active) return;
          setItems(rows);
          setError("");
        })
        .catch((e) => active && setError(e instanceof Error ? e.message : String(e)));
    load();
    const t = setInterval(load, 5000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, []);

  const all = items ?? [];
  const pending = all.filter((a) => a.status === "pending");
  const shown = filter === "pending" ? pending : filter === "decided" ? all.filter((a) => a.status !== "pending") : all;

  return (
    <>
      <TopBar crumbs={[{ label: "Inbox" }]} />
      <PageContainer>
        <PageTitle
          title="Inbox"
          subtitle={
            pending.length > 0
              ? `${pending.length} draft${pending.length === 1 ? "" : "s"} waiting for a decision. Nothing is published until someone approves it.`
              : "Nothing is waiting for you. Drafts appear here when a playbook needs a decision."
          }
        />

        <div className="mb-4">
          <Segmented<Filter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: "pending", label: "Waiting", count: pending.length },
              { value: "decided", label: "Decided" },
              { value: "all", label: "All", count: all.length },
            ]}
          />
        </div>

        {error && (
          <Card className="mb-4 border-danger/30 px-4 py-3 text-[13px] text-danger">{error}</Card>
        )}

        <Card className="overflow-hidden">
          {shown.map((a) => (
            <Link
              key={a.id}
              href={`/inbox/${a.id}`}
              className="group flex items-start gap-3 border-b border-border px-4 py-3.5 transition-colors last:border-0 hover:bg-surface-2/40"
            >
              <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-warn-bg ring-1 ring-warn/25">
                <InboxIcon className="size-4 text-warn" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[13px] font-semibold text-fg">{a.title || "Draft"}</span>
                  {a.channel && <span className="text-[12px] text-muted">→ {a.channel}</span>}
                  <ApprovalStatusBadge status={a.status as ApprovalStatus} />
                </div>
                <p className="mt-0.5 line-clamp-2 text-[12.5px] leading-relaxed text-muted">{a.draft}</p>
                <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11.5px] text-faint">
                  {a.signal ? (
                    <span className="inline-flex items-center gap-1">
                      <Radar className="size-3" />
                      {a.signal.title}
                      {a.signalCount > 1 && ` +${a.signalCount - 1} more`}
                    </span>
                  ) : (
                    <span>No signal recorded</span>
                  )}
                  {a.workflowName && <span>{a.workflowName}</span>}
                  {a.assignee && <span>For {a.assignee}</span>}
                  <span>
                    {a.status === "pending"
                      ? `Waiting ${timeAgo(a.createdAt).replace(" ago", "")}`
                      : a.decidedAt
                        ? `${a.decidedBy ? `${a.decidedBy} · ` : ""}${timeAgo(a.decidedAt)}`
                        : timeAgo(a.createdAt)}
                  </span>
                </div>
              </div>
              <ArrowRight className="mt-1 size-4 shrink-0 text-ghost transition-colors group-hover:text-muted" />
            </Link>
          ))}
          {items === null && !error && <div className="py-16 text-center text-sm text-faint">Loading…</div>}
          {items !== null && shown.length === 0 && (
            <div className="py-16 text-center text-sm text-faint">
              {filter === "pending" ? "No drafts are waiting." : "Nothing here yet."}
            </div>
          )}
        </Card>
      </PageContainer>
    </>
  );
}
