"use client";

import * as React from "react";
import { Search, Download } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Segmented } from "@/components/ui/segmented";
import { SearchInput } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { RunRow } from "@/components/run-row";
import { fetchRuns } from "@/lib/api";
import type { WorkflowRun } from "@/lib/types";

type Filter = "all" | "running" | "succeeded" | "failed" | "canceled";

export default function RunsPage() {
  const [filter, setFilter] = React.useState<Filter>("all");
  const [q, setQ] = React.useState("");

  // Live data from the Go API. Polls every 2s so a run created by the worker
  // appears here and a running run's progress updates without a refresh.
  const [runs, setRuns] = React.useState<WorkflowRun[]>([]);
  const [loaded, setLoaded] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const data = await fetchRuns();
        if (active) {
          setRuns(data);
          setLoaded(true);
          setError(null);
        }
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : "Failed to load runs");
      }
    };
    load();
    const t = setInterval(load, 2000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, []);

  const counts = React.useMemo(() => {
    const c: Record<string, number> = { all: runs.length };
    for (const r of runs) c[r.status] = (c[r.status] ?? 0) + 1;
    return c;
  }, [runs]);

  const list = runs.filter((r) => {
    const okStatus = filter === "all" || r.status === filter;
    const okQ = !q || r.workflowName.toLowerCase().includes(q.toLowerCase()) || `${r.number}`.includes(q);
    return okStatus && okQ;
  });

  const liveBadge = (
    <span className="inline-flex items-center gap-1.5 text-[11px] text-muted">
      <span className="size-1.5 rounded-full bg-running animate-pulse" />
      Live · {loaded ? `${runs.length} runs` : "connecting…"}
    </span>
  );

  return (
    <>
      <TopBar
        crumbs={[{ label: "Runs" }]}
        actions={
          <Button variant="secondary" size="sm">
            <Download className="size-4" /> Export
          </Button>
        }
      />
      <PageContainer>
        <PageTitle
          title="Runs"
          subtitle="Every execution across your workspace, newest first."
          actions={liveBadge}
        />
        {error && (
          <div className="mb-4 rounded-md border border-danger/30 bg-danger-bg px-3 py-2 text-[12px] text-danger">
            {error} — is the API running on :8080?
          </div>
        )}

        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <Segmented<Filter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All", count: counts.all },
              { value: "running", label: "Running", count: counts.running ?? 0 },
              { value: "succeeded", label: "Succeeded", count: counts.succeeded ?? 0 },
              { value: "failed", label: "Failed", count: counts.failed ?? 0 },
              { value: "canceled", label: "Canceled", count: counts.canceled ?? 0 },
            ]}
          />
          <SearchInput
            icon={<Search className="size-3.5" />}
            placeholder="Search runs…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            className="w-[220px]"
          />
        </div>

        <Card className="overflow-hidden">
          <div className="grid grid-cols-[140px_1fr_120px_110px_92px_40px] items-center gap-3 border-b border-border bg-surface-2/40 px-4 py-2 text-[11px] font-medium uppercase tracking-wider text-faint">
            <span>Status</span>
            <span>Workflow</span>
            <span className="hidden sm:block">Triggered by</span>
            <span className="hidden sm:block">Runtime</span>
            <span className="hidden text-right sm:block">Cost</span>
            <span />
          </div>
          {list.map((run) => (
            <RunRow key={run.id} run={run} />
          ))}
          {list.length === 0 && (
            <div className="py-14 text-center text-sm text-faint">No runs match your filters.</div>
          )}
        </Card>
      </PageContainer>
    </>
  );
}
