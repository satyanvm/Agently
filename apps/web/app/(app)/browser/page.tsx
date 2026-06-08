"use client";

import * as React from "react";
import Link from "next/link";
import { Globe, MousePointerClick, Camera, ArrowRight } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/status";
import { LivePill } from "@/components/live";
import { fetchRuns, fetchBrowserSession } from "@/lib/api";
import { cn, timeAgo } from "@/lib/utils";
import type { BrowserSession, WorkflowRun } from "@/lib/types";

interface SessionCard {
  runId: string;
  runLabel: string;
  session: BrowserSession;
}

export default function BrowserPage() {
  const [cards, setCards] = React.useState<SessionCard[]>([]);

  React.useEffect(() => {
    let active = true;
    const load = async () => {
      const runs = await fetchRuns();
      const withBrowser = runs.filter((r: WorkflowRun) => r.browserSessionId);
      const out: SessionCard[] = [];
      for (const r of withBrowser.slice(0, 24)) {
        const s = await fetchBrowserSession(r.id);
        if (s) out.push({ runId: r.id, runLabel: `${r.workflowName} #${r.number}`, session: s });
      }
      if (active) setCards(out);
    };
    load().catch(() => {});
    const t = setInterval(() => load().catch(() => {}), 4000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, []);

  return (
    <>
      <TopBar crumbs={[{ label: "Browser Sessions" }]} />
      <PageContainer>
        <PageTitle
          title="Browser Sessions"
          subtitle="Every headless browser your agents have driven — captured and replayable."
        />

        {cards.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border py-16 text-center text-sm text-faint">
            No browser sessions yet. Run a workflow with a browser-role agent to see live sessions here.
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {cards.map(({ runId, runLabel, session: s }) => {
              const tone = s.shots.at(-1)?.tone ?? "from-indigo-500 via-violet-500 to-sky-500";
              return (
                <Link key={s.id} href={`/browser/${runId}`} className="group block">
                  <Card hover className="overflow-hidden p-0">
                    <div className={cn("relative aspect-[16/9] w-full bg-gradient-to-br", tone)}>
                      <div className="absolute inset-0 bg-grid opacity-30" />
                      <div className="absolute left-3 top-3">
                        {s.status === "running" ? <LivePill /> : <StatusBadge status={s.status} size="sm" />}
                      </div>
                      <div className="absolute inset-x-3 bottom-3 flex items-center gap-1.5 truncate rounded-md bg-black/50 px-2 py-1 font-mono text-[11px] text-white/80 backdrop-blur">
                        <Globe className="size-3 shrink-0" />
                        <span className="truncate">{s.currentUrl || "—"}</span>
                      </div>
                    </div>
                    <div className="p-4">
                      <div className="flex items-center justify-between gap-2">
                        <span className="truncate text-[13px] font-semibold text-fg">{s.agentName}</span>
                        <span className="text-[11px] text-faint">{timeAgo(s.startedAt)}</span>
                      </div>
                      <p className="mt-0.5 truncate text-[12px] text-muted">{runLabel}</p>
                      <div className="mt-3 flex items-center gap-4 border-t border-border pt-3 text-[11px] text-faint">
                        <span className="inline-flex items-center gap-1"><Globe className="size-3" /> {s.pagesVisited} pages</span>
                        <span className="inline-flex items-center gap-1"><MousePointerClick className="size-3" /> {s.actionsCount}</span>
                        <span className="inline-flex items-center gap-1"><Camera className="size-3" /> {s.shots.length}</span>
                        <ArrowRight className="ml-auto size-3.5 text-ghost transition-transform group-hover:translate-x-0.5 group-hover:text-muted" />
                      </div>
                    </div>
                  </Card>
                </Link>
              );
            })}
          </div>
        )}
      </PageContainer>
    </>
  );
}
