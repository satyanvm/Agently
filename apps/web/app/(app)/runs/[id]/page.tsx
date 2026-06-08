"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { RunDetail } from "@/components/run-detail";
import { fetchRun, fetchRunLogsAfter, fetchBrowserSession } from "@/lib/api";
import type { LogEntry, WorkflowRun, BrowserSession } from "@/lib/types";

const TERMINAL = new Set(["succeeded", "failed", "canceled"]);

export default function RunPage() {
  const { id } = useParams<{ id: string }>();
  const [run, setRun] = React.useState<WorkflowRun | null>(null);
  const [logs, setLogs] = React.useState<LogEntry[]>([]);
  const [session, setSession] = React.useState<BrowserSession | null>(null);
  const [state, setState] = React.useState<"loading" | "ready" | "notfound" | "error">("loading");

  // Highest log seq we've ingested, so we only fetch NEW lines each tick.
  const sinceSeq = React.useRef(-1);

  React.useEffect(() => {
    if (!id) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout>;

    const tick = async () => {
      try {
        const [r, newLogs, sess] = await Promise.all([
          fetchRun(id),
          fetchRunLogsAfter(id, sinceSeq.current),
          fetchBrowserSession(id),
        ]);
        if (!active) return;

        if (!r) {
          setState("notfound");
          return;
        }
        setRun(r);
        setSession(sess);
        if (newLogs.length > 0) {
          // append only the genuinely-new lines; advance the high-water mark
          setLogs((prev) => [...prev, ...newLogs]);
          sinceSeq.current = Math.max(sinceSeq.current, ...newLogs.map((l) => l.seq));
        }
        setState("ready");

        // Keep polling only while the run is live; stop once terminal (the pass
        // above already captured the final logs + status).
        if (!TERMINAL.has(r.status)) {
          timer = setTimeout(tick, 1500);
        }
      } catch {
        if (active) {
          setState((s) => (s === "loading" ? "error" : s));
          timer = setTimeout(tick, 3000); // transient: back off and retry
        }
      }
    };
    tick();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [id]);

  if (state === "loading") {
    return (
      <>
        <TopBar crumbs={[{ label: "Runs", href: "/runs" }, { label: "Loading…" }]} />
        <PageContainer>
          <div className="py-20 text-center text-sm text-faint">Loading run…</div>
        </PageContainer>
      </>
    );
  }

  if (state === "notfound" || !run) {
    return (
      <>
        <TopBar crumbs={[{ label: "Runs", href: "/runs" }, { label: "Not found" }]} />
        <PageContainer>
          <div className="py-20 text-center text-sm text-faint">
            Run not found, or the API isn’t running on :8080.
          </div>
        </PageContainer>
      </>
    );
  }

  return (
    <>
      <TopBar
        crumbs={[
          { label: "Runs", href: "/runs" },
          { label: `${run.workflowName} #${run.number}` },
        ]}
      />
      <PageContainer>
        {/* Agents, logs, artifacts, and the browser session are all live from the
            API now — a browser-role agent (e.g. Navigator) populates the session. */}
        <RunDetail run={run} logs={logs} session={session} />
      </PageContainer>
    </>
  );
}
