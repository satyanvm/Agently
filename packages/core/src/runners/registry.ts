import type { Agent } from "../types.js";
import type { Runner } from "./types.js";

const registry = new Map<Agent["type"], Runner>();

export function registerRunner(runner: Runner): void {
  registry.set(runner.type, runner);
}

export function getRunner(type: Agent["type"]): Runner {
  const runner = registry.get(type);
  if (!runner) {
    throw new Error(
      `No runner registered for agent type "${type}". ` +
        `Registered: [${[...registry.keys()].join(", ") || "none"}]`,
    );
  }
  return runner;
}

export function registeredTypes(): Agent["type"][] {
  return [...registry.keys()];
}
