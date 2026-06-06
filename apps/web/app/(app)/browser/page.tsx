import Link from "next/link";
import { Globe, MousePointerClick, Camera, ArrowRight } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Card } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/status";
import { LivePill } from "@/components/live";
import { browserSession } from "@/lib/mock-data";
import { cn, timeAgo } from "@/lib/utils";
import type { RunStatus } from "@/lib/types";

interface SessionSummary {
  id: string;
  runLabel: string;
  agentName: string;
  status: RunStatus;
  url: string;
  pages: number;
  actions: number;
  shots: number;
  tone: string;
  at: string;
}

const sessions: SessionSummary[] = [
  {
    id: browserSession.id,
    runLabel: "Competitive Intelligence Sweep #142",
    agentName: browserSession.agentName,
    status: "running",
    url: browserSession.currentUrl,
    pages: browserSession.pagesVisited,
    actions: browserSession.actionsCount,
    shots: browserSession.shots.length,
    tone: browserSession.shots.at(-1)!.tone,
    at: browserSession.startedAt,
  },
  {
    id: "bs-2",
    runLabel: "Candidate Sourcing Pipeline #31",
    agentName: "Navigator",
    status: "failed",
    url: "https://legacy-corp.com/careers",
    pages: 6,
    actions: 22,
    shots: 9,
    tone: "from-rose-500 via-pink-500 to-orange-500",
    at: "2026-06-06T14:22:00Z",
  },
  {
    id: "bs-3",
    runLabel: "SEO Content Engine #63",
    agentName: "Navigator",
    status: "succeeded",
    url: "https://cms.northwind.dev/posts",
    pages: 11,
    actions: 41,
    shots: 18,
    tone: "from-emerald-500 via-teal-500 to-cyan-500",
    at: "2026-06-05T06:02:00Z",
  },
  {
    id: "bs-4",
    runLabel: "Competitive Intelligence Sweep #141",
    agentName: "Navigator",
    status: "succeeded",
    url: "https://rival.io/product",
    pages: 14,
    actions: 53,
    shots: 24,
    tone: "from-indigo-500 via-violet-500 to-sky-500",
    at: "2026-06-03T09:18:00Z",
  },
];

export default function BrowserPage() {
  return (
    <>
      <TopBar crumbs={[{ label: "Browser Sessions" }]} />
      <PageContainer>
        <PageTitle
          title="Browser Sessions"
          subtitle="Every headless browser your agents have driven — captured and replayable."
        />

        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {sessions.map((s) => (
            <Link key={s.id} href={`/browser/${s.id}`} className="group block">
              <Card hover className="overflow-hidden p-0">
                <div className={cn("relative aspect-[16/9] w-full bg-gradient-to-br", s.tone)}>
                  <div className="absolute inset-0 bg-grid opacity-30" />
                  <div className="absolute left-3 top-3">
                    {s.status === "running" ? <LivePill /> : <StatusBadge status={s.status} size="sm" />}
                  </div>
                  <div className="absolute inset-x-3 bottom-3 flex items-center gap-1.5 truncate rounded-md bg-black/50 px-2 py-1 font-mono text-[11px] text-white/80 backdrop-blur">
                    <Globe className="size-3 shrink-0" />
                    <span className="truncate">{s.url}</span>
                  </div>
                </div>
                <div className="p-4">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-[13px] font-semibold text-fg">{s.agentName}</span>
                    <span className="text-[11px] text-faint">{timeAgo(s.at)}</span>
                  </div>
                  <p className="mt-0.5 truncate text-[12px] text-muted">{s.runLabel}</p>
                  <div className="mt-3 flex items-center gap-4 border-t border-border pt-3 text-[11px] text-faint">
                    <span className="inline-flex items-center gap-1"><Globe className="size-3" /> {s.pages} pages</span>
                    <span className="inline-flex items-center gap-1"><MousePointerClick className="size-3" /> {s.actions}</span>
                    <span className="inline-flex items-center gap-1"><Camera className="size-3" /> {s.shots}</span>
                    <ArrowRight className="ml-auto size-3.5 text-ghost transition-transform group-hover:translate-x-0.5 group-hover:text-muted" />
                  </div>
                </div>
              </Card>
            </Link>
          ))}
        </div>
      </PageContainer>
    </>
  );
}
