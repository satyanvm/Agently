import type {
  BrowserSessionDetail,
  BrowserAction,
  BrowserActionType,
  BrowserSessionId,
  RunId,
} from "@agently/contracts";
import { DomainError, ids } from "@agently/contracts";
import type { PlatformDeps, Emitter } from "./context.js";

export interface RecordActionInput {
  type: BrowserActionType;
  target: string;
  value?: string | null;
  status: BrowserAction["status"];
  durationMs: number;
}

export interface BrowserService {
  getById(id: BrowserSessionId): BrowserSessionDetail;
  getByRun(runId: RunId): BrowserSessionDetail | null;
  /** Durable write path for a captured browser action. */
  recordAction(sessionId: BrowserSessionId, input: RecordActionInput): BrowserAction;
}

export function createBrowserService(deps: PlatformDeps, emit: Emitter): BrowserService {
  const { repos, clock } = deps;

  function assemble(id: BrowserSessionId): BrowserSessionDetail {
    const session = repos.browser.getById(id);
    if (!session) throw DomainError.notFound("browser session");
    return {
      ...session,
      shots: repos.browser.listShots(id),
      actions: repos.browser.listActions(id),
      console: repos.browser.listConsole(id),
    };
  }

  return {
    getById(id) {
      return assemble(id);
    },
    getByRun(runId) {
      const session = repos.browser.getByRun(runId);
      return session ? assemble(session.id) : null;
    },
    recordAction(sessionId, input) {
      const session = repos.browser.getById(sessionId);
      if (!session) throw DomainError.notFound("browser session");
      const action: BrowserAction = {
        id: ids.browserAction(),
        sessionId,
        ts: clock.iso(),
        type: input.type,
        target: input.target,
        value: input.value ?? null,
        status: input.status,
        durationMs: input.durationMs,
      };
      repos.browser.insertAction(action);
      repos.browser.update(sessionId, { actionsCount: session.actionsCount + 1 });
      emit({ type: "run.browser.action", runId: session.runId, sessionId, action });
      return action;
    },
  };
}
