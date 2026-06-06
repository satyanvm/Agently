import type {
  AgentDefinition,
  Page,
  PageQuery,
  CreateAgentInput,
} from "@agently/contracts";
import { ids } from "@agently/contracts";
import type { PlatformDeps } from "./context.js";
import { paginate } from "../util.js";

export interface AgentService {
  list(query: PageQuery): Page<AgentDefinition>;
  create(input: CreateAgentInput): AgentDefinition;
}

export function createAgentService(deps: PlatformDeps): AgentService {
  const { repos, clock } = deps;
  return {
    list(query) {
      return paginate(repos.agents.all(), query);
    },
    create(input) {
      const now = clock.iso();
      const agent: AgentDefinition = {
        id: ids.agentDefinition(),
        workspaceId: repos.workspace.get().id,
        name: input.name,
        role: input.role,
        model: input.model,
        description: input.description,
        tools: input.tools,
        config: input.config,
        createdAt: now,
        updatedAt: now,
      };
      return repos.agents.insert(agent);
    },
  };
}
