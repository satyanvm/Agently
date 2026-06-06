import type {
  LogEntry,
  LogLevel,
  LogChannel,
  RunId,
  Page,
  ListLogsQuery,
} from "@agently/contracts";
import { ids } from "@agently/contracts";
import type { PlatformDeps, Emitter } from "./context.js";
import { paginate } from "../util.js";

export interface AppendLogInput {
  level: LogLevel;
  channel: LogChannel;
  source: string;
  message: string;
  detail?: string | null;
  reasoning?: boolean;
}

export interface LogService {
  /** Append a log line: assigns seq + offset, persists, emits an event. This is
   *  the durable write path a real runner will call. */
  append(runId: RunId, input: AppendLogInput): LogEntry;
  /** Filtered, paginated read used by the log viewer + API. */
  list(runId: RunId, query: ListLogsQuery): Page<LogEntry>;
}

const SEVERITY_RANK: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  success: 1,
  warn: 2,
  error: 3,
};

export function createLogService(deps: PlatformDeps, emit: Emitter): LogService {
  const { repos, clock } = deps;

  return {
    append(runId, input) {
      const run = repos.runs.getById(runId);
      const startMs = run?.startedAt ? Date.parse(run.startedAt) : clock.now();
      const seq = repos.logs.maxSeq(runId) + 1;
      const entry: LogEntry = {
        id: ids.log(),
        runId,
        seq,
        ts: clock.iso(),
        offsetMs: Math.max(0, clock.now() - startMs),
        level: input.level,
        channel: input.channel,
        source: input.source,
        message: input.message,
        detail: input.detail ?? null,
        reasoning: input.reasoning ?? input.channel === "model",
      };
      repos.logs.insert(entry);
      emit({ type: "run.log.appended", runId, log: entry });
      return entry;
    },

    list(runId, query) {
      let items = repos.logs.listByRun(runId);
      const after = query.afterSeq;
      if (after != null) items = items.filter((l) => l.seq > after);
      if (query.severity === "warn") items = items.filter((l) => SEVERITY_RANK[l.level] >= 2);
      if (query.severity === "error") items = items.filter((l) => l.level === "error");
      if (query.channel) items = items.filter((l) => l.channel === query.channel);
      if (query.source) items = items.filter((l) => l.source === query.source);
      if (query.q) {
        const needle = query.q.toLowerCase();
        items = items.filter(
          (l) =>
            l.message.toLowerCase().includes(needle) ||
            l.source.toLowerCase().includes(needle),
        );
      }
      return paginate(items, query);
    },
  };
}
