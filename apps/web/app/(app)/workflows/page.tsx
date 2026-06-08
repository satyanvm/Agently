"use client";

import * as React from "react";
import { Search, Plus, SlidersHorizontal } from "lucide-react";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer, PageTitle } from "@/components/shell/page";
import { Segmented } from "@/components/ui/segmented";
import { SearchInput } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { WorkflowCard } from "@/components/workflow-row";
import { fetchWorkflows } from "@/lib/api";
import type { RunStatus, Workflow } from "@/lib/types";

type Filter = "all" | "running" | "succeeded" | "failed" | "paused";

export default function WorkflowsPage() {
  const [filter, setFilter] = React.useState<Filter>("all");
  const [q, setQ] = React.useState("");
  const [workflows, setWorkflows] = React.useState<Workflow[]>([]);

  React.useEffect(() => {
    let active = true;
    fetchWorkflows().then((w) => active && setWorkflows(w)).catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  const counts = React.useMemo(() => {
    const c: Record<string, number> = { all: workflows.length };
    for (const w of workflows) c[w.status] = (c[w.status] ?? 0) + 1;
    return c;
  }, [workflows]);

  const list = workflows.filter((w) => {
    const okStatus = filter === "all" || w.status === (filter as RunStatus);
    const okQ = !q || w.name.toLowerCase().includes(q.toLowerCase());
    return okStatus && okQ;
  });

  return (
    <>
      <TopBar
        crumbs={[{ label: "Workflows" }]}
        actions={
          <Button variant="primary" size="sm">
            <Plus className="size-4" /> New workflow
          </Button>
        }
      />
      <PageContainer>
        <PageTitle
          title="Workflows"
          subtitle="Reusable agent crews you can launch, schedule, or trigger from events."
        />

        <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
          <Segmented<Filter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All", count: counts.all },
              { value: "running", label: "Running", count: counts.running ?? 0 },
              { value: "succeeded", label: "Healthy", count: counts.succeeded ?? 0 },
              { value: "failed", label: "Failing", count: counts.failed ?? 0 },
              { value: "paused", label: "Paused", count: counts.paused ?? 0 },
            ]}
          />
          <div className="flex items-center gap-2">
            <SearchInput
              icon={<Search className="size-3.5" />}
              placeholder="Search workflows…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className="w-[220px]"
            />
            <Button variant="secondary" size="icon-sm" aria-label="Filters">
              <SlidersHorizontal className="size-4" />
            </Button>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {list.map((wf) => (
            <WorkflowCard key={wf.id} wf={wf} />
          ))}
        </div>

        {list.length === 0 && (
          <div className="rounded-lg border border-dashed border-border py-16 text-center text-sm text-faint">
            No workflows match your filters.
          </div>
        )}
      </PageContainer>
    </>
  );
}
