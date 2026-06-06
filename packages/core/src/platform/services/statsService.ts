import type { WorkspaceStats, WorkflowStats, WorkflowId } from "@agently/contracts";
import type { PlatformDeps } from "./context.js";

export interface StatsService {
  workspace(): WorkspaceStats;
  workflow(workflowId: WorkflowId): WorkflowStats | null;
}

export function createStatsService(deps: PlatformDeps): StatsService {
  const { repos, seedStats } = deps;
  return {
    workspace() {
      // Recompute the volatile counters live; keep the seeded time-series.
      const activeRuns = repos.runs.all().filter((r) => r.status === "running").length;
      return { ...seedStats.workspaceStats, activeRuns };
    },
    workflow(workflowId) {
      return seedStats.workflowStats[workflowId] ?? null;
    },
  };
}
