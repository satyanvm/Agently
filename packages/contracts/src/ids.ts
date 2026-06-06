import { z } from "zod";

/**
 * Prefixed, branded identifiers.
 *
 * Every entity has a string id with a stable, human-legible prefix
 * (`wf_compint`, `run_8842`, `log_19f3…`). The prefix makes ids self-describing
 * in logs, URLs, and API payloads, and lets us validate that the *right kind*
 * of id was passed without a database round-trip.
 *
 * Branding (`z.brand`) gives compile-time safety: a `RunId` is not assignable
 * to a `WorkflowId` even though both are strings. This is pure type-level and
 * survives any storage choice — Postgres, DynamoDB, or a flat file.
 */

const idPattern = (prefix: string) => new RegExp(`^${prefix}_[0-9a-z]+$`);

function brandedId<B extends string>(prefix: string, brand: B) {
  return z
    .string()
    .regex(idPattern(prefix), `expected a "${prefix}_" id`)
    .brand(brand);
}

/** Map of entity → id prefix. The single place prefixes are declared. */
export const ID_PREFIX = {
  workspace: "ws",
  member: "mem",
  apiKey: "key",
  workflow: "wf",
  workflowVersion: "wfv",
  agentDefinition: "agt",
  run: "run",
  runAgent: "ra",
  agentMessage: "msg",
  log: "log",
  artifact: "art",
  browserSession: "bs",
  browserAction: "ba",
  browserShot: "shot",
  notification: "ntf",
  activity: "act",
  event: "evt",
} as const;

export type EntityKind = keyof typeof ID_PREFIX;

export const WorkspaceId = brandedId(ID_PREFIX.workspace, "WorkspaceId");
export type WorkspaceId = z.infer<typeof WorkspaceId>;

export const MemberId = brandedId(ID_PREFIX.member, "MemberId");
export type MemberId = z.infer<typeof MemberId>;

export const ApiKeyId = brandedId(ID_PREFIX.apiKey, "ApiKeyId");
export type ApiKeyId = z.infer<typeof ApiKeyId>;

export const WorkflowId = brandedId(ID_PREFIX.workflow, "WorkflowId");
export type WorkflowId = z.infer<typeof WorkflowId>;

export const WorkflowVersionId = brandedId(ID_PREFIX.workflowVersion, "WorkflowVersionId");
export type WorkflowVersionId = z.infer<typeof WorkflowVersionId>;

export const AgentDefinitionId = brandedId(ID_PREFIX.agentDefinition, "AgentDefinitionId");
export type AgentDefinitionId = z.infer<typeof AgentDefinitionId>;

export const RunId = brandedId(ID_PREFIX.run, "RunId");
export type RunId = z.infer<typeof RunId>;

export const RunAgentId = brandedId(ID_PREFIX.runAgent, "RunAgentId");
export type RunAgentId = z.infer<typeof RunAgentId>;

export const AgentMessageId = brandedId(ID_PREFIX.agentMessage, "AgentMessageId");
export type AgentMessageId = z.infer<typeof AgentMessageId>;

export const LogId = brandedId(ID_PREFIX.log, "LogId");
export type LogId = z.infer<typeof LogId>;

export const ArtifactId = brandedId(ID_PREFIX.artifact, "ArtifactId");
export type ArtifactId = z.infer<typeof ArtifactId>;

export const BrowserSessionId = brandedId(ID_PREFIX.browserSession, "BrowserSessionId");
export type BrowserSessionId = z.infer<typeof BrowserSessionId>;

export const BrowserActionId = brandedId(ID_PREFIX.browserAction, "BrowserActionId");
export type BrowserActionId = z.infer<typeof BrowserActionId>;

export const BrowserShotId = brandedId(ID_PREFIX.browserShot, "BrowserShotId");
export type BrowserShotId = z.infer<typeof BrowserShotId>;

export const NotificationId = brandedId(ID_PREFIX.notification, "NotificationId");
export type NotificationId = z.infer<typeof NotificationId>;

export const ActivityId = brandedId(ID_PREFIX.activity, "ActivityId");
export type ActivityId = z.infer<typeof ActivityId>;

export const EventId = brandedId(ID_PREFIX.event, "EventId");
export type EventId = z.infer<typeof EventId>;

/* ------------------------------------------------------------------ *
   Generation
 * ------------------------------------------------------------------ */

/** Lowercase base36 random suffix; collision-resistant enough for ids. */
function randomSuffix(size = 20): string {
  // crypto is available in Node 19+ and all modern browsers.
  const bytes =
    typeof crypto !== "undefined" && "getRandomValues" in crypto
      ? crypto.getRandomValues(new Uint8Array(size))
      : Array.from({ length: size }, () => Math.floor(Math.random() * 256));
  let out = "";
  for (const b of bytes) out += (b % 36).toString(36);
  return out;
}

/**
 * Mint a new id for an entity kind. Bypasses validation (the value is
 * constructed correctly by definition) and returns the branded type.
 */
export function newId<K extends EntityKind>(kind: K): string {
  return `${ID_PREFIX[kind]}_${randomSuffix()}`;
}

/** Strongly-typed minters for ergonomics at call sites. */
export const ids = {
  workspace: () => newId("workspace") as WorkspaceId,
  member: () => newId("member") as MemberId,
  apiKey: () => newId("apiKey") as ApiKeyId,
  workflow: () => newId("workflow") as WorkflowId,
  workflowVersion: () => newId("workflowVersion") as WorkflowVersionId,
  agentDefinition: () => newId("agentDefinition") as AgentDefinitionId,
  run: () => newId("run") as RunId,
  runAgent: () => newId("runAgent") as RunAgentId,
  agentMessage: () => newId("agentMessage") as AgentMessageId,
  log: () => newId("log") as LogId,
  artifact: () => newId("artifact") as ArtifactId,
  browserSession: () => newId("browserSession") as BrowserSessionId,
  browserAction: () => newId("browserAction") as BrowserActionId,
  browserShot: () => newId("browserShot") as BrowserShotId,
  notification: () => newId("notification") as NotificationId,
  activity: () => newId("activity") as ActivityId,
  event: () => newId("event") as EventId,
} as const;
