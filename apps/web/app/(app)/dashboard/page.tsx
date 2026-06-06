import Link from "next/link";
import {
  Activity,
  CircleDollarSign,
  Gauge,
  ArrowUpRight,
  Rocket,
  CheckCircle2,
  XCircle,
  GitCommitHorizontal,
  Server,
  MessageSquare,
} from "lucide-react";
import { TopBar, NewRunButton } from "@/components/shell/topbar";
import { PageContainer, PageTitle, SectionHeading } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { Stat } from "@/components/ui/stat";
import { Sparkline, BarChart } from "@/components/ui/sparkline";
import { ActiveRunCard, RunRow } from "@/components/run-row";
import { WorkflowHealthRow } from "@/components/workflow-row";
import { LivePill } from "@/components/live";
import {
  runs,
  workflows,
  dashboardStats as s,
  activity,
} from "@/lib/mock-data";
import { formatCompact, formatCost, timeAgo } from "@/lib/utils";

export default function DashboardPage() {
  const active = runs.filter((r) => r.status === "running");
  const recent = runs.filter((r) => r.status !== "running" && r.status !== "queued").slice(0, 5);

  return (
    <>
      <TopBar crumbs={[{ label: "Dashboard" }]} actions={<NewRunButton />} />
      <PageContainer>
        <PageTitle
          title="Good afternoon, Maya"
          subtitle="Here's what your agents have been doing while you were away."
          actions={<LivePill />}
        />

        {/* KPI row */}
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Stat
            label="Active runs"
            icon={<Activity className="size-3.5 text-running" />}
            value={s.activeRuns}
            sub="across 2 workflows"
            visual={<BarChart data={s.runVolume.slice(-12)} height={36} highlightLast />}
          />
          <Stat
            label="Runs today"
            icon={<GitCommitHorizontal className="size-3.5 text-accent-soft" />}
            value={s.runsToday}
            delta={{ value: "+12%", positive: true }}
            sub={`${formatCompact(s.tokensToday)} tokens`}
          />
          <Stat
            label="Success rate"
            icon={<Gauge className="size-3.5 text-success" />}
            value={`${(s.successRate * 100).toFixed(1)}%`}
            delta={{ value: "+0.4%", positive: true }}
            sub="last 24 hours"
          />
          <Stat
            label="Spend today"
            icon={<CircleDollarSign className="size-3.5 text-warn" />}
            value={formatCost(s.spendToday)}
            sub={`of $${s.spendBudget} budget`}
            visual={<Sparkline data={s.spendSeries} width={90} height={36} stroke="var(--color-warn)" />}
          />
        </div>

        {/* Main grid */}
        <div className="mt-6 grid gap-6 lg:grid-cols-[1.6fr_1fr]">
          {/* LEFT */}
          <div className="space-y-6">
            <section>
              <SectionHeading action={<Link href="/runs" className="text-[12px] text-muted hover:text-fg">View all</Link>}>
                Active runs
              </SectionHeading>
              <div className="grid gap-3 sm:grid-cols-2">
                {active.map((run) => (
                  <ActiveRunCard key={run.id} run={run} />
                ))}
              </div>
            </section>

            <section>
              <SectionHeading action={<Link href="/runs" className="text-[12px] text-muted hover:text-fg">All runs</Link>}>
                Recent results
              </SectionHeading>
              <Card className="overflow-hidden">
                <div className="flex items-center justify-between border-b border-border bg-surface-2/40 px-4 py-2 text-[11px] font-medium uppercase tracking-wider text-faint">
                  <span>Status · Workflow</span>
                  <span className="hidden gap-12 sm:flex">
                    <span>Triggered by</span>
                    <span>Runtime</span>
                    <span>Cost</span>
                  </span>
                </div>
                {recent.map((run) => (
                  <RunRow key={run.id} run={run} />
                ))}
              </Card>
            </section>
          </div>

          {/* RIGHT */}
          <div className="space-y-6">
            {/* Runtime statistics */}
            <Card>
              <div className="flex items-center justify-between px-5 pt-4">
                <h3 className="text-[13px] font-semibold text-fg">Runtime statistics</h3>
                <span className="text-[11px] text-faint">last 24h</span>
              </div>
              <div className="px-5 pb-5 pt-3">
                <div className="flex items-baseline justify-between">
                  <div>
                    <div className="text-2xl font-semibold tabular-nums">{s.computeHours}h</div>
                    <div className="text-[11px] text-faint">compute consumed</div>
                  </div>
                  <span className="inline-flex items-center gap-1 rounded bg-success-bg px-1.5 py-0.5 text-[11px] text-success">
                    <ArrowUpRight className="size-3" /> 8.2%
                  </span>
                </div>
                <BarChart data={s.runVolume} height={64} className="mt-4" />
                <div className="mt-2 flex justify-between text-[10px] text-ghost">
                  <span>00:00</span>
                  <span>12:00</span>
                  <span>now</span>
                </div>
                <div className="mt-4 grid grid-cols-3 gap-3 border-t border-border pt-4 text-center">
                  <MiniMetric label="Avg runtime" value="11m" />
                  <MiniMetric label="Sandboxes" value="6" />
                  <MiniMetric label="Queue" value="0" />
                </div>
              </div>
            </Card>

            {/* Workflow health */}
            <Card>
              <div className="flex items-center justify-between px-4 pb-1 pt-4">
                <h3 className="text-[13px] font-semibold text-fg">Workflow health</h3>
                <Link href="/workflows" className="text-[12px] text-muted hover:text-fg">All</Link>
              </div>
              <div className="px-2 pb-3">
                {workflows.slice(0, 6).map((wf) => (
                  <WorkflowHealthRow key={wf.id} wf={wf} />
                ))}
              </div>
            </Card>

            {/* Recent activity */}
            <Card>
              <div className="px-5 pb-1 pt-4">
                <h3 className="text-[13px] font-semibold text-fg">Recent activity</h3>
              </div>
              <div className="px-5 pb-5 pt-2">
                <ol className="relative space-y-4 before:absolute before:left-[7px] before:top-1 before:h-[calc(100%-12px)] before:w-px before:bg-border">
                  {activity.slice(0, 6).map((a) => (
                    <li key={a.id} className="relative flex gap-3 pl-6">
                      <ActivityDot kind={a.kind} />
                      <div className="min-w-0 flex-1">
                        <p className="text-[12.5px] leading-snug text-muted">
                          <span className="font-medium text-fg">{a.actor}</span> {a.text}
                        </p>
                        <span className="text-[11px] text-ghost">{timeAgo(a.at)}</span>
                      </div>
                    </li>
                  ))}
                </ol>
              </div>
            </Card>
          </div>
        </div>
      </PageContainer>
    </>
  );
}

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[15px] font-semibold tabular-nums text-fg">{value}</div>
      <div className="text-[10px] text-faint">{label}</div>
    </div>
  );
}

const ACTIVITY_META = {
  deploy: { icon: Rocket, cls: "text-accent-soft bg-accent-bg ring-accent/25" },
  run: { icon: Activity, cls: "text-running bg-running-bg ring-running/25" },
  complete: { icon: CheckCircle2, cls: "text-success bg-success-bg ring-success/25" },
  fail: { icon: XCircle, cls: "text-danger bg-danger-bg ring-danger/25" },
  comment: { icon: MessageSquare, cls: "text-muted bg-surface-2 ring-border" },
  scale: { icon: Server, cls: "text-warn bg-warn-bg ring-warn/25" },
} as const;

function ActivityDot({ kind }: { kind: keyof typeof ACTIVITY_META }) {
  const m = ACTIVITY_META[kind];
  const Icon = m.icon;
  return (
    <span
      className={`absolute left-0 top-0 flex h-[15px] w-[15px] items-center justify-center rounded-full ring-1 ${m.cls}`}
    >
      <Icon className="size-2.5" />
    </span>
  );
}
