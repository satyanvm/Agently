"use client";

import * as React from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { ArrowUpRight } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { BrowserSessionView } from "@/components/browser-session";
import { Button } from "@/components/ui/button";
import { fetchBrowserSession } from "@/lib/api";
import type { BrowserSession } from "@/lib/types";

// The route param is the RUN id (the index links by run); a run has one primary
// browser session, fetched via /api/runs/{id}/browser.
export default function BrowserSessionPage() {
  const { id: runId } = useParams<{ id: string }>();
  const [session, setSession] = React.useState<BrowserSession | null>(null);
  const [loading, setLoading] = React.useState(true);

  React.useEffect(() => {
    if (!runId) return;
    let active = true;
    const load = () =>
      fetchBrowserSession(runId)
        .then((s) => {
          if (active) {
            setSession(s);
            setLoading(false);
          }
        })
        .catch(() => active && setLoading(false));
    load();
    const t = setInterval(load, 2500);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [runId]);

  if (loading) {
    return (
      <>
        <TopBar crumbs={[{ label: "Browser Sessions", href: "/browser" }, { label: "Loading…" }]} />
        <PageContainer>
          <div className="py-20 text-center text-sm text-faint">Loading session…</div>
        </PageContainer>
      </>
    );
  }
  if (!session) {
    return (
      <>
        <TopBar crumbs={[{ label: "Browser Sessions", href: "/browser" }, { label: "Not found" }]} />
        <PageContainer>
          <div className="py-20 text-center text-sm text-faint">No browser session for this run.</div>
        </PageContainer>
      </>
    );
  }

  return (
    <>
      <TopBar
        crumbs={[
          { label: "Browser Sessions", href: "/browser" },
          { label: session.agentName },
        ]}
        actions={
          <Link href={`/runs/${runId}`}>
            <Button variant="secondary" size="sm">
              View run <ArrowUpRight className="size-3.5" />
            </Button>
          </Link>
        }
      />
      <PageContainer>
        <div className="mb-5">
          <h1 className="text-[20px] font-semibold tracking-tight text-fg">{session.pageTitle || "Browser session"}</h1>
          <p className="mt-1 font-mono text-[12px] text-muted">{session.currentUrl}</p>
        </div>
        <BrowserSessionView session={session} />
      </PageContainer>
    </>
  );
}
