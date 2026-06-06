import type {
  Workspace,
  Member,
  AgentDefinition,
  Workflow,
  WorkflowVersion,
  Run,
  RunAgent,
  AgentMessage,
  Artifact,
  LogEntry,
  BrowserSession,
  BrowserAction,
  BrowserShot,
  BrowserConsoleLine,
  Notification,
  ActivityEvent,
  WorkflowId,
  WorkflowVersionId,
  AgentDefinitionId,
  RunId,
  RunAgentId,
  BrowserSessionId,
  NotificationId,
  WorkspaceId,
} from "@agently/contracts";
import { DomainError } from "@agently/contracts";
import type { Repositories } from "./types.js";

/** The data an in-memory store is constructed from (produced by the seed). */
export interface MemoryData {
  workspace: Workspace;
  members: Member[];
  agents: AgentDefinition[];
  workflows: Workflow[];
  versions: WorkflowVersion[];
  runs: Run[];
  runAgents: RunAgent[];
  messages: AgentMessage[];
  artifacts: Artifact[];
  logs: LogEntry[];
  browserSessions: BrowserSession[];
  browserActions: BrowserAction[];
  browserShots: BrowserShot[];
  browserConsole: { sessionId: BrowserSessionId; line: BrowserConsoleLine }[];
  notifications: Notification[];
  activity: ActivityEvent[];
}

function index<T, K>(items: T[], key: (t: T) => K): Map<K, T> {
  const m = new Map<K, T>();
  for (const it of items) m.set(key(it), it);
  return m;
}

/**
 * A complete in-memory implementation of every repository. Holds mutable Maps
 * seeded from `MemoryData`. Deterministic, dependency-free, and fast — ideal for
 * the mock backend, local dev, and tests.
 */
export function createMemoryRepositories(data: MemoryData): Repositories {
  const workspace = data.workspace;
  const members = index(data.members, (m) => m.id as string);
  const agents = index(data.agents, (a) => a.id);
  const workflows = [...data.workflows];
  const wfById = index(workflows, (w) => w.id);
  const wfBySlug = index(workflows, (w) => w.slug);
  const versions = index(data.versions, (v) => v.id);
  const runs = [...data.runs];
  const runById = index(runs, (r) => r.id);
  const runAgents = [...data.runAgents];
  const messages = [...data.messages];
  const artifacts = [...data.artifacts];
  const logs = [...data.logs];
  const sessions = index(data.browserSessions, (s) => s.id);
  const actions = [...data.browserActions];
  const shots = [...data.browserShots];
  const consoleLines = [...data.browserConsole];
  const notifications = [...data.notifications];
  const notifById = index(notifications, (n) => n.id);
  const activity = [...data.activity];

  return {
    workspace: {
      get: () => workspace,
    },

    members: {
      all: () => [...members.values()],
      getById: (id) => members.get(id),
    },

    agents: {
      all: () => [...agents.values()],
      getById: (id: AgentDefinitionId) => agents.get(id),
      insert: (agent) => {
        agents.set(agent.id, agent);
        return agent;
      },
    },

    workflows: {
      all: () => [...workflows],
      getById: (id: WorkflowId) => wfById.get(id),
      getBySlug: (slug) => wfBySlug.get(slug),
      insert: (w) => {
        workflows.push(w);
        wfById.set(w.id, w);
        wfBySlug.set(w.slug, w);
        return w;
      },
      update: (id: WorkflowId, patch) => {
        const cur = wfById.get(id);
        if (!cur) throw DomainError.notFound("workflow");
        const next = { ...cur, ...patch };
        wfById.set(id, next);
        wfBySlug.set(next.slug, next);
        const i = workflows.findIndex((w) => w.id === id);
        if (i >= 0) workflows[i] = next;
        return next;
      },
    },

    versions: {
      getById: (id: WorkflowVersionId) => versions.get(id),
      listByWorkflow: (workflowId: WorkflowId) =>
        [...versions.values()].filter((v) => v.workflowId === workflowId),
      insert: (v) => {
        versions.set(v.id, v);
        return v;
      },
    },

    runs: {
      all: () => [...runs],
      getById: (id: RunId) => runById.get(id),
      listByWorkflow: (workflowId: WorkflowId) =>
        runs.filter((r) => r.workflowId === workflowId),
      insert: (run) => {
        runs.push(run);
        runById.set(run.id, run);
        return run;
      },
      update: (id: RunId, patch) => {
        const cur = runById.get(id);
        if (!cur) throw DomainError.notFound("run");
        const next = { ...cur, ...patch };
        runById.set(id, next);
        const i = runs.findIndex((r) => r.id === id);
        if (i >= 0) runs[i] = next;
        return next;
      },
      nextNumber: (workflowId: WorkflowId) => {
        const existing = runs.filter((r) => r.workflowId === workflowId);
        return existing.reduce((max, r) => Math.max(max, r.number), 0) + 1;
      },
    },

    runAgents: {
      listByRun: (runId: RunId) => runAgents.filter((a) => a.runId === runId),
      getById: (id: RunAgentId) => runAgents.find((a) => a.id === id),
      insertMany: (items) => {
        runAgents.push(...items);
      },
      update: (id: RunAgentId, patch) => {
        const i = runAgents.findIndex((a) => a.id === id);
        if (i < 0) throw DomainError.notFound("run agent");
        const next = { ...runAgents[i]!, ...patch };
        runAgents[i] = next;
        return next;
      },
    },

    messages: {
      listByRun: (runId: RunId) => messages.filter((m) => m.runId === runId),
      insert: (m) => {
        messages.push(m);
        return m;
      },
    },

    artifacts: {
      listByRun: (runId: RunId) => artifacts.filter((a) => a.runId === runId),
      insert: (a) => {
        artifacts.push(a);
        return a;
      },
    },

    logs: {
      listByRun: (runId: RunId) =>
        logs.filter((l) => l.runId === runId).sort((a, b) => a.seq - b.seq),
      insert: (entry) => {
        logs.push(entry);
        return entry;
      },
      maxSeq: (runId: RunId) =>
        logs.reduce((max, l) => (l.runId === runId ? Math.max(max, l.seq) : max), -1),
    },

    browser: {
      getById: (id: BrowserSessionId) => sessions.get(id),
      getByRun: (runId: RunId) =>
        [...sessions.values()].find((s) => s.runId === runId),
      insertSession: (s) => {
        sessions.set(s.id, s);
        return s;
      },
      update: (id: BrowserSessionId, patch) => {
        const cur = sessions.get(id);
        if (!cur) throw DomainError.notFound("browser session");
        const next = { ...cur, ...patch };
        sessions.set(id, next);
        return next;
      },
      listActions: (id: BrowserSessionId) => actions.filter((a) => a.sessionId === id),
      listShots: (id: BrowserSessionId) => shots.filter((s) => s.sessionId === id),
      listConsole: (id: BrowserSessionId) =>
        consoleLines.filter((c) => c.sessionId === id).map((c) => c.line),
      insertAction: (a) => {
        actions.push(a);
        return a;
      },
      insertShot: (s) => {
        shots.push(s);
        return s;
      },
      insertConsole: (id: BrowserSessionId, line) => {
        consoleLines.push({ sessionId: id, line });
      },
    },

    notifications: {
      all: () => [...notifications],
      getById: (id: NotificationId) => notifById.get(id),
      insert: (n) => {
        notifications.push(n);
        notifById.set(n.id, n);
        return n;
      },
      update: (id: NotificationId, patch) => {
        const cur = notifById.get(id);
        if (!cur) throw DomainError.notFound("notification");
        const next = { ...cur, ...patch };
        notifById.set(id, next);
        const i = notifications.findIndex((n) => n.id === id);
        if (i >= 0) notifications[i] = next;
        return next;
      },
      markAllRead: (workspaceId: WorkspaceId, at) => {
        let updated = 0;
        for (let i = 0; i < notifications.length; i++) {
          const n = notifications[i]!;
          if (n.workspaceId === workspaceId && n.readAt === null) {
            const next = { ...n, readAt: at };
            notifications[i] = next;
            notifById.set(n.id, next);
            updated++;
          }
        }
        return updated;
      },
    },

    activity: {
      all: () => [...activity],
      insert: (e) => {
        activity.push(e);
        return e;
      },
    },
  };
}
