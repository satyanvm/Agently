import type {
  Run,
  RunDetail,
  RunAgent,
  RunStatus,
  AgentStatus,
  RunId,
  RunAgentId,
  Page,
  LaunchRunInput,
  ListRunsQuery,
  StepProgress,
} from "@agently/contracts";
import { DomainError, ids, isTerminalRunStatus } from "@agently/contracts";
import type { PlatformDeps, Emitter } from "./context.js";
import type { LogService } from "./logService.js";
import { paginate, byNewest } from "../util.js";

export interface RunService {
  list(query: ListRunsQuery): Page<Run>;
  get(id: RunId): RunDetail;
  launch(slug: string, input: LaunchRunInput): RunDetail;
  cancel(id: RunId): Run;
  /* ---- lifecycle write-path (what a real runner/worker will call) ---- */
  start(id: RunId): Run;
  progress(id: RunId, patch: { steps: StepProgress; currentStep: string }): Run;
  transitionAgent(runId: RunId, agentId: RunAgentId, to: AgentStatus): RunAgent;
  finish(id: RunId, status: Extract<RunStatus, "succeeded" | "failed">, error?: string): Run;
}

export function createRunService(
  deps: PlatformDeps,
  emit: Emitter,
  logs: LogService,
): RunService {
  const { repos, clock } = deps;

  function detail(run: Run): RunDetail {
    return {
      ...run,
      agents: repos.runAgents.listByRun(run.id),
      messages: repos.messages.listByRun(run.id),
      artifacts: repos.artifacts.listByRun(run.id),
    };
  }

  function requireRun(id: RunId): Run {
    const run = repos.runs.getById(id);
    if (!run) throw DomainError.notFound("run");
    return run;
  }

  return {
    list(query) {
      let items = repos.runs.all().sort(byNewest((r) => r.startedAt ?? r.queuedAt));
      if (query.status) items = items.filter((r) => r.status === query.status);
      if (query.workflowId) items = items.filter((r) => r.workflowId === query.workflowId);
      return paginate(items, query);
    },

    get(id) {
      return detail(requireRun(id));
    },

    launch(slug, input) {
      const wf = repos.workflows.getBySlug(slug);
      if (!wf) throw DomainError.notFound("workflow");

      const now = clock.iso();
      const runId = ids.run();
      const principal = repos.members.all()[0];

      const run: Run = {
        id: runId,
        workspaceId: wf.workspaceId,
        workflowId: wf.id,
        workflowVersionId: wf.currentVersionId,
        workflowName: wf.name,
        workflowSlug: wf.slug,
        number: repos.runs.nextNumber(wf.id),
        status: "running",
        trigger: input.trigger,
        triggeredBy: principal
          ? { name: principal.name, initials: principal.initials }
          : { name: "Manual", initials: "MA" },
        region: input.region ?? repos.workspace.get().defaultRegion,
        steps: { done: 0, total: Math.max(1, wf.agentCount * 4) },
        currentStep: "Provisioning sandbox",
        costUsd: 0,
        usage: { tokensIn: 0, tokensOut: 0 },
        error: null,
        browserSessionId: null,
        queuedAt: now,
        startedAt: now,
        finishedAt: null,
      };
      repos.runs.insert(run);

      // Materialize run agents from the workflow's current version graph.
      const version = wf.currentVersionId ? repos.versions.getById(wf.currentVersionId) : undefined;
      const nodes = version?.nodes ?? [];
      const keyToId = new Map<string, RunAgentId>();
      for (const n of nodes) keyToId.set(n.key, ids.runAgent());
      const runAgents: RunAgent[] = nodes.map((n) => {
        const entry = n.dependsOn.length === 0;
        return {
          id: keyToId.get(n.key)!,
          runId,
          agentDefinitionId: n.agentDefinitionId,
          name: n.name,
          role: n.role,
          model: n.model,
          status: entry ? "running" : "idle",
          dependsOn: n.dependsOn.map((k) => keyToId.get(k)!).filter(Boolean) as RunAgentId[],
          col: n.col,
          row: n.row,
          summary: "",
          metrics: { tokens: 0, costUsd: 0, runtimeMs: entry ? 0 : null, toolCalls: 0, progress: 0 },
          startedAt: entry ? now : null,
          finishedAt: null,
        };
      });
      repos.runAgents.insertMany(runAgents);

      emit({ type: "run.queued", runId, workflowId: wf.id });
      emit({ type: "run.started", runId });

      logs.append(runId, { level: "info", channel: "system", source: "scheduler", message: `Run #${run.number} launched (${input.trigger})` });
      logs.append(runId, { level: "info", channel: "system", source: "runtime", message: `Provisioning sandbox · ${run.region}` });

      repos.activity.insert({
        id: ids.activity(),
        workspaceId: wf.workspaceId,
        kind: "run",
        actor: run.triggeredBy.name,
        text: `started run #${run.number} of ${wf.name}`,
        workflowSlug: wf.slug,
        runId,
        at: now,
      });

      return detail(run);
    },

    cancel(id) {
      const run = requireRun(id);
      if (isTerminalRunStatus(run.status)) {
        throw DomainError.conflict(`run is already ${run.status}`);
      }
      const now = clock.iso();
      const updated = repos.runs.update(id, { status: "canceled", finishedAt: now, currentStep: "Canceled" });
      logs.append(id, { level: "warn", channel: "system", source: "runtime", message: "Run canceled by user" });
      emit({ type: "run.finished", runId: id, status: "canceled", error: null });
      return updated;
    },

    start(id) {
      const run = requireRun(id);
      const updated = repos.runs.update(id, { status: "running", startedAt: run.startedAt ?? clock.iso() });
      emit({ type: "run.started", runId: id });
      return updated;
    },

    progress(id, patch) {
      requireRun(id);
      const updated = repos.runs.update(id, { steps: patch.steps, currentStep: patch.currentStep });
      emit({
        type: "run.progress",
        runId: id,
        stepsDone: patch.steps.done,
        stepsTotal: patch.steps.total,
        currentStep: patch.currentStep,
      });
      return updated;
    },

    transitionAgent(runId, agentId, to) {
      const agent = repos.runAgents.getById(agentId);
      if (!agent) throw DomainError.notFound("run agent");
      const from = agent.status;
      const updated = repos.runAgents.update(agentId, {
        status: to,
        finishedAt: to === "succeeded" || to === "failed" ? clock.iso() : agent.finishedAt,
      });
      emit({ type: "run.agent.transitioned", runId, agentId, from, to });
      return updated;
    },

    finish(id, status, error) {
      requireRun(id);
      const now = clock.iso();
      const updated = repos.runs.update(id, {
        status,
        finishedAt: now,
        error: error ?? null,
        currentStep: status === "succeeded" ? "Completed" : (error ?? "Failed"),
      });
      emit({ type: "run.finished", runId: id, status, error: error ?? null });
      return updated;
    },
  };
}
