import { notFound } from "next/navigation";
import { TopBar } from "@/components/shell/topbar";
import { PageContainer } from "@/components/shell/page";
import { RunDetail } from "@/components/run-detail";
import { getRun, runs, flagshipLogs, browserSession } from "@/lib/mock-data";
import type { LogEntry } from "@/lib/types";

export function generateStaticParams() {
  return runs.map((r) => ({ id: r.id }));
}

export default async function RunPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const run = getRun(id);
  if (!run) notFound();

  // Only the flagship run has a full mock log + browser stream; others reuse a
  // trimmed slice so every run page is populated.
  const isFlagship = run.id === "run-8842";
  const logs: LogEntry[] = isFlagship
    ? flagshipLogs
    : flagshipLogs.slice(0, 14).map((l) => ({ ...l, runId: run.id }));
  const session = run.browserSessionId ? browserSession : null;

  return (
    <>
      <TopBar
        crumbs={[
          { label: "Runs", href: "/runs" },
          { label: `${run.workflowName} #${run.number}` },
        ]}
      />
      <PageContainer>
        <RunDetail run={run} logs={logs} session={session} />
      </PageContainer>
    </>
  );
}
