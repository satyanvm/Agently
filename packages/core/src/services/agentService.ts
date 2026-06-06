import type { SupabaseClient } from "../db/index.js";
import type { Agent } from "../types.js";

/**
 * Framework-agnostic business logic for agents. No HTTP, no Next.js imports —
 * so the same functions back both Next.js route handlers (MVP) and a future
 * standalone Express/Fastify server, unchanged.
 *
 * Signatures are defined now; bodies are wired to Supabase in Phase 2.
 */

export interface CreateAgentInput {
  name: string;
  type: Agent["type"];
  config: Record<string, unknown>;
}

export async function createAgent(
  _client: SupabaseClient,
  _userId: string,
  _input: CreateAgentInput,
): Promise<Agent> {
  throw new Error("createAgent: implemented in Phase 2");
}

export async function listAgents(
  _client: SupabaseClient,
  _userId: string,
): Promise<Agent[]> {
  throw new Error("listAgents: implemented in Phase 2");
}

export async function getAgent(
  _client: SupabaseClient,
  _userId: string,
  _agentId: string,
): Promise<Agent | null> {
  throw new Error("getAgent: implemented in Phase 2");
}
