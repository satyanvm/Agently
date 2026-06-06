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

/**
 * Storage interfaces. Services depend only on these, never on a concrete store,
 * so the in-memory implementation used by the mock backend can be replaced by a
 * Supabase/Postgres implementation without changing a single service or route.
 *
 * Reads return plain arrays; pagination/filtering is composed in services via
 * the shared `paginate` helper. Writes are explicit and minimal.
 */

export interface WorkspaceRepo {
  get(): Workspace;
}

export interface MemberRepo {
  all(): Member[];
  getById(id: string): Member | undefined;
}

export interface AgentDefinitionRepo {
  all(): AgentDefinition[];
  getById(id: AgentDefinitionId): AgentDefinition | undefined;
  insert(agent: AgentDefinition): AgentDefinition;
}

export interface WorkflowRepo {
  all(): Workflow[];
  getById(id: WorkflowId): Workflow | undefined;
  getBySlug(slug: string): Workflow | undefined;
  insert(workflow: Workflow): Workflow;
  update(id: WorkflowId, patch: Partial<Workflow>): Workflow;
}

export interface WorkflowVersionRepo {
  getById(id: WorkflowVersionId): WorkflowVersion | undefined;
  listByWorkflow(workflowId: WorkflowId): WorkflowVersion[];
  insert(version: WorkflowVersion): WorkflowVersion;
}

export interface RunRepo {
  all(): Run[];
  getById(id: RunId): Run | undefined;
  listByWorkflow(workflowId: WorkflowId): Run[];
  insert(run: Run): Run;
  update(id: RunId, patch: Partial<Run>): Run;
  /** Next monotonic run number for a workflow. */
  nextNumber(workflowId: WorkflowId): number;
}

export interface RunAgentRepo {
  listByRun(runId: RunId): RunAgent[];
  getById(id: RunAgentId): RunAgent | undefined;
  insertMany(agents: RunAgent[]): void;
  update(id: RunAgentId, patch: Partial<RunAgent>): RunAgent;
}

export interface MessageRepo {
  listByRun(runId: RunId): AgentMessage[];
  insert(message: AgentMessage): AgentMessage;
}

export interface ArtifactRepo {
  listByRun(runId: RunId): Artifact[];
  insert(artifact: Artifact): Artifact;
}

export interface LogRepo {
  listByRun(runId: RunId): LogEntry[];
  insert(entry: LogEntry): LogEntry;
  /** Highest seq written for a run (-1 if none). */
  maxSeq(runId: RunId): number;
}

export interface BrowserRepo {
  getById(id: BrowserSessionId): BrowserSession | undefined;
  getByRun(runId: RunId): BrowserSession | undefined;
  insertSession(session: BrowserSession): BrowserSession;
  update(id: BrowserSessionId, patch: Partial<BrowserSession>): BrowserSession;
  listActions(id: BrowserSessionId): BrowserAction[];
  listShots(id: BrowserSessionId): BrowserShot[];
  listConsole(id: BrowserSessionId): BrowserConsoleLine[];
  insertAction(action: BrowserAction): BrowserAction;
  insertShot(shot: BrowserShot): BrowserShot;
  insertConsole(id: BrowserSessionId, line: BrowserConsoleLine): void;
}

export interface NotificationRepo {
  all(): Notification[];
  getById(id: NotificationId): Notification | undefined;
  insert(notification: Notification): Notification;
  update(id: NotificationId, patch: Partial<Notification>): Notification;
  markAllRead(workspaceId: WorkspaceId, at: string): number;
}

export interface ActivityRepo {
  all(): ActivityEvent[];
  insert(event: ActivityEvent): ActivityEvent;
}

/** The full set of repositories, handed to services as one dependency. */
export interface Repositories {
  workspace: WorkspaceRepo;
  members: MemberRepo;
  agents: AgentDefinitionRepo;
  workflows: WorkflowRepo;
  versions: WorkflowVersionRepo;
  runs: RunRepo;
  runAgents: RunAgentRepo;
  messages: MessageRepo;
  artifacts: ArtifactRepo;
  logs: LogRepo;
  browser: BrowserRepo;
  notifications: NotificationRepo;
  activity: ActivityRepo;
}
