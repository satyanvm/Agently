"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { WorkflowDetail } from "@/components/workflow-detail";
import { fetchWorkflow, fetchWorkflowRuns, fetchRun, fetchWorkflowGraph } from "@/lib/api";
import type { Workflow, WorkflowRun, AgentNode } from "@/lib/types";

export default function WorkflowPage() {
  const { slug } = useParams<{ slug: string }>();
  const [wf, setWf] = React.useState<Workflow | null>(null);
  const [runs, setRuns] = React.useState<WorkflowRun[]>([]);
  const [agents, setAgents] = React.useState<AgentNode[]>([]);
  const [state, setState] = React.useState<"loading" | "ready" | "notfound">("loading");

  React.useEffect(() => {
    if (!slug) return;
    let active = true;
    const load = async () => {
      const w = await fetchWorkflow(slug);
      if (!active) return;
      if (!w) {
        setState("notfound");
        return;
      }
      setWf(w);
      const wfRuns = await fetchWorkflowRuns(slug);
      if (!active) return;
      setRuns(wfRuns);
      // Derive the agent graph from the most recent run (live statuses). If the
      // workflow has never run, fall back to its PLANNED graph (the version's nodes)
      // so a just-created workflow shows its agents immediately.
      if (wfRuns.length > 0) {
        const detail = await fetchRun(wfRuns[0]!.id);
        if (active && detail) setAgents(detail.agents);
      } else {
        const planned = await fetchWorkflowGraph(slug).catch(() => []);
        if (active) setAgents(planned);
      }
      setState("ready");
    };
    load();
    const t = setInterval(load, 3000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [slug]);

  if (state === "notfound") {
    return (
      <>
        <TopBar crumbs={[{ label: "Workflows", href: "/workflows" }, { label: "Not found" }]} />
        <PageContainer>
          <div className="py-20 text-center text-sm text-faint">Workflow not found.</div>
        </PageContainer>
      </>
    );
  }
  if (!wf) {
    return (
      <>
        <TopBar crumbs={[{ label: "Workflows", href: "/workflows" }, { label: "Loading…" }]} />
        <PageContainer>
          <div className="py-20 text-center text-sm text-faint">Loading workflow…</div>
        </PageContainer>
      </>
    );
  }

  const activeRun = runs.find((r) => r.status === "running") ?? null;

  return (
    <>
      <TopBar crumbs={[{ label: "Workflows", href: "/workflows" }, { label: wf.name }]} />
      <PageContainer>
        <WorkflowDetail wf={wf} agents={agents} runs={runs.slice(0, 6)} activeRun={activeRun} />
      </PageContainer>
    </>
  );
}
