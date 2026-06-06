import { notFound } from "next/navigation";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { WorkflowDetail } from "@/components/workflow-detail";
import {
  getWorkflow,
  agentsForWorkflow,
  runsForWorkflow,
  workflows,
  runs as allRuns,
} from "@/lib/mock-data";

export function generateStaticParams() {
  return workflows.map((w) => ({ slug: w.slug }));
}

export default async function WorkflowPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;
  const wf = getWorkflow(slug);
  if (!wf) notFound();

  const agents = agentsForWorkflow(wf);
  const wfRuns = runsForWorkflow(wf.id);
  // Ensure at least a couple of timeline entries even for sparse workflows.
  const timeline = (wfRuns.length ? wfRuns : allRuns.slice(0, 3)).slice(0, 6);
  const activeRun = wfRuns.find((r) => r.status === "running") ?? null;

  return (
    <>
      <TopBar
        crumbs={[{ label: "Workflows", href: "/workflows" }, { label: wf.name }]}
      />
      <PageContainer>
        <WorkflowDetail wf={wf} agents={agents} runs={timeline} activeRun={activeRun} />
      </PageContainer>
    </>
  );
}
