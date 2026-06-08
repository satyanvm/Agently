"use client";

import * as React from "react";
import Link from "next/link";
import {
  CheckCircle2,
  XCircle,
  Globe,
  CircleDollarSign,
  PauseCircle,
  Play,
  CheckCheck,
  Settings2,
  ArrowRight,
} from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { Switch } from "@/components/ui/switch";
import { fetchNotifications } from "@/lib/api";
import type { AppNotification, NotificationType } from "@/lib/types";
import { cn, timeAgo } from "@/lib/utils";

const TYPE_META: Record<NotificationType, { icon: typeof CheckCircle2; tone: string; ring: string; label: string }> = {
  "workflow.completed": { icon: CheckCircle2, tone: "text-success", ring: "bg-success-bg ring-success/25", label: "Completed" },
  "workflow.failed": { icon: XCircle, tone: "text-danger", ring: "bg-danger-bg ring-danger/25", label: "Failed" },
  "browser.error": { icon: Globe, tone: "text-warn", ring: "bg-warn-bg ring-warn/25", label: "Browser error" },
  "cost.alert": { icon: CircleDollarSign, tone: "text-warn", ring: "bg-warn-bg ring-warn/25", label: "Cost alert" },
  "agent.blocked": { icon: PauseCircle, tone: "text-accent-soft", ring: "bg-accent-bg ring-accent/25", label: "Agent blocked" },
  "run.started": { icon: Play, tone: "text-running", ring: "bg-running-bg ring-running/25", label: "Run started" },
};

type Filter = "all" | "unread" | "failures" | "cost" | "browser";

export default function NotificationsPage() {
  const [items, setItems] = React.useState<AppNotification[]>([]);
  const [filter, setFilter] = React.useState<Filter>("all");

  React.useEffect(() => {
    let active = true;
    const load = () => fetchNotifications().then((n) => active && setItems(n)).catch(() => {});
    load();
    const t = setInterval(load, 5000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, []);

  const unread = items.filter((n) => !n.read).length;

  const filtered = items.filter((n) => {
    if (filter === "unread") return !n.read;
    if (filter === "failures") return n.type === "workflow.failed";
    if (filter === "cost") return n.type === "cost.alert";
    if (filter === "browser") return n.type === "browser.error";
    return true;
  });

  function markAll() {
    setItems((prev) => prev.map((n) => ({ ...n, read: true })));
  }
  function markRead(id: string) {
    setItems((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
  }

  return (
    <>
      <TopBar
        crumbs={[{ label: "Notifications" }]}
        actions={
          <Button variant="secondary" size="sm" onClick={markAll}>
            <CheckCheck className="size-4" /> Mark all read
          </Button>
        }
      />
      <PageContainer>
        <PageTitle
          title="Notifications"
          subtitle={unread > 0 ? `${unread} unread · across all workflows` : "You're all caught up."}
        />

        <div className="grid gap-6 lg:grid-cols-[1.7fr_1fr]">
          {/* Feed */}
          <div>
            <div className="mb-4">
              <Segmented<Filter>
                value={filter}
                onChange={setFilter}
                options={[
                  { value: "all", label: "All", count: items.length },
                  { value: "unread", label: "Unread", count: unread },
                  { value: "failures", label: "Failures" },
                  { value: "cost", label: "Cost" },
                  { value: "browser", label: "Browser" },
                ]}
              />
            </div>

            <Card className="overflow-hidden">
              {filtered.map((n) => {
                const m = TYPE_META[n.type];
                const Icon = m.icon;
                return (
                  <div
                    key={n.id}
                    className={cn(
                      "group relative flex gap-3 border-b border-border px-4 py-3.5 transition-colors last:border-0 hover:bg-surface-2/40",
                      !n.read && "bg-surface-2/20",
                    )}
                  >
                    {!n.read && <span className="absolute left-1.5 top-1/2 h-1.5 w-1.5 -translate-y-1/2 rounded-full bg-accent" />}
                    <span className={cn("mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ring-1", m.ring)}>
                      <Icon className={cn("size-4", m.tone)} />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className={cn("text-[13px] font-semibold", n.read ? "text-muted" : "text-fg")}>
                          {n.title}
                        </span>
                        <span className="text-[11px] text-ghost">{timeAgo(n.at)}</span>
                      </div>
                      <p className="mt-0.5 text-[12.5px] leading-relaxed text-muted">{n.body}</p>
                      {n.workflowSlug && (
                        <Link
                          href={n.runNumber ? `/runs/run-8842` : `/workflows/${n.workflowSlug}`}
                          onClick={() => markRead(n.id)}
                          className="mt-1.5 inline-flex items-center gap-1 text-[12px] text-accent-soft hover:underline"
                        >
                          View {n.runNumber ? `run #${n.runNumber}` : "workflow"}
                          <ArrowRight className="size-3" />
                        </Link>
                      )}
                    </div>
                    {!n.read && (
                      <button
                        onClick={() => markRead(n.id)}
                        className="shrink-0 self-center rounded-md px-2 py-1 text-[11px] text-faint opacity-0 transition-opacity hover:bg-surface-3 hover:text-fg group-hover:opacity-100"
                      >
                        Mark read
                      </button>
                    )}
                  </div>
                );
              })}
              {filtered.length === 0 && (
                <div className="py-16 text-center text-sm text-faint">Nothing here.</div>
              )}
            </Card>
          </div>

          {/* Preferences */}
          <div>
            <Card>
              <div className="flex items-center gap-2 px-5 pb-1 pt-4">
                <Settings2 className="size-4 text-muted" />
                <h3 className="text-[13px] font-semibold text-fg">Delivery preferences</h3>
              </div>
              <div className="px-5 pb-2 pt-3">
                <Pref label="In-app" desc="Show in the notification center" defaultOn />
                <Pref label="Email" desc="maya@northwind.dev" defaultOn />
                <Pref label="Slack" desc="#agent-ops channel" defaultOn />
                <Pref label="Webhook" desc="POST to your endpoint" />
              </div>
              <div className="border-t border-border px-5 pb-5 pt-4">
                <span className="text-[11px] font-medium uppercase tracking-wider text-ghost">
                  Notify me about
                </span>
                <div className="mt-3 space-y-1">
                  <Pref label="Workflow completed" defaultOn compact />
                  <Pref label="Workflow failed" defaultOn compact />
                  <Pref label="Browser errors" defaultOn compact />
                  <Pref label="Cost thresholds" defaultOn compact />
                  <Pref label="Agent blocked / needs input" defaultOn compact />
                </div>
              </div>
            </Card>

            <Card className="mt-5 px-5 py-4">
              <h3 className="text-[13px] font-semibold text-fg">Cost alert threshold</h3>
              <p className="mt-1 text-[12px] text-muted">Alert when a single run exceeds this budget.</p>
              <div className="mt-3 flex items-center gap-3">
                <div className="flex h-9 flex-1 items-center rounded-md border border-border bg-surface px-3">
                  <span className="text-faint">$</span>
                  <input
                    defaultValue="6.00"
                    className="ml-1 w-full bg-transparent text-sm tabular-nums text-fg focus:outline-none"
                  />
                </div>
                <Button variant="secondary" size="sm">Save</Button>
              </div>
            </Card>
          </div>
        </div>
      </PageContainer>
    </>
  );
}

function Pref({
  label,
  desc,
  defaultOn,
  compact,
}: {
  label: string;
  desc?: string;
  defaultOn?: boolean;
  compact?: boolean;
}) {
  const [on, setOn] = React.useState(!!defaultOn);
  return (
    <div className={cn("flex items-center justify-between", compact ? "py-1.5" : "py-2.5")}>
      <div>
        <div className="text-[13px] font-medium text-fg">{label}</div>
        {desc && <div className="text-[11px] text-faint">{desc}</div>}
      </div>
      <Switch checked={on} onChange={setOn} />
    </div>
  );
}
