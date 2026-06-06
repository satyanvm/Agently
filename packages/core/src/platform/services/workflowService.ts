import type {
  Workflow,
  WorkflowSummary,
  WorkflowStats,
  RunStatus,
  Principal,
  Page,
  PageQuery,
  CreateWorkflowInput,
} from "@agently/contracts";
import { DomainError, ids } from "@agently/contracts";
import type { PlatformDeps } from "./context.js";
import { paginate, byNewest } from "../util.js";

const FALLBACK_STATS: WorkflowStats = {
  successRate: 1,
  avgRuntimeMs: 0,
  avgCostUsd: 0,
  totalRuns: 0,
  recent: [],
  trend: [],
};

export interface WorkflowService {
  list(query: PageQuery): Page<WorkflowSummary>;
  getBySlug(slug: string): WorkflowSummary;
  create(input: CreateWorkflowInput): WorkflowSummary;
  toSummary(workflow: Workflow): WorkflowSummary;
}

export function createWorkflowService(deps: PlatformDeps): WorkflowService {
  const { repos, clock, seedStats } = deps;

  function ownerOf(workflow: Workflow): Principal | null {
    if (!workflow.ownerId) return null;
    const m = repos.members.getById(workflow.ownerId);
    return m ? { name: m.name, initials: m.initials } : null;
  }

  function deriveStatus(workflowId: Workflow["id"]): RunStatus {
    const wfRuns = repos.runs.listByWorkflow(workflowId);
    if (wfRuns.some((r) => r.status === "running")) return "running";
    const latest = [...wfRuns].sort(byNewest((r) => r.startedAt ?? r.queuedAt))[0];
    return latest?.status ?? "queued";
  }

  function lastRunAt(workflowId: Workflow["id"]): string | null {
    const wfRuns = repos.runs.listByWorkflow(workflowId);
    const latest = [...wfRuns].sort(byNewest((r) => r.startedAt ?? r.queuedAt))[0];
    return latest?.startedAt ?? latest?.queuedAt ?? null;
  }

  function toSummary(workflow: Workflow): WorkflowSummary {
    return {
      ...workflow,
      status: deriveStatus(workflow.id),
      owner: ownerOf(workflow),
      lastRunAt: lastRunAt(workflow.id),
      stats: seedStats.workflowStats[workflow.id] ?? FALLBACK_STATS,
    };
  }

  return {
    toSummary,
    list(query) {
      const items = repos.workflows
        .all()
        .filter((w) => w.archivedAt === null)
        .map(toSummary);
      return paginate(items, query);
    },
    getBySlug(slug) {
      const wf = repos.workflows.getBySlug(slug);
      if (!wf) throw DomainError.notFound("workflow");
      return toSummary(wf);
    },
    create(input) {
      const now = clock.iso();
      const slug = input.name
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "");
      if (repos.workflows.getBySlug(slug)) {
        throw DomainError.conflict(`a workflow with slug "${slug}" already exists`);
      }
      const workflow: Workflow = {
        id: ids.workflow(),
        workspaceId: repos.workspace.get().id,
        slug,
        name: input.name,
        description: input.description,
        trigger: input.trigger,
        schedule: input.schedule,
        tags: input.tags,
        ownerId: repos.members.all()[0]?.id ?? null,
        agentCount: 0,
        currentVersionId: null,
        createdAt: now,
        updatedAt: now,
        archivedAt: null,
      };
      repos.workflows.insert(workflow);
      return toSummary(workflow);
    },
  };
}
