/**
 * Minimal structured logger. Emits JSON lines in production (parseable by any
 * log pipeline) and pretty lines in dev. The interface is what code depends on,
 * so swapping in pino/winston/OTel later is a one-file change.
 */

export type LogFields = Record<string, unknown>;

export interface Logger {
  debug(msg: string, fields?: LogFields): void;
  info(msg: string, fields?: LogFields): void;
  warn(msg: string, fields?: LogFields): void;
  error(msg: string, fields?: LogFields): void;
  /** Returns a logger that merges `fields` into every line. */
  child(fields: LogFields): Logger;
}

type Level = "debug" | "info" | "warn" | "error";
const ORDER: Record<Level, number> = { debug: 0, info: 1, warn: 2, error: 3 };

export interface LoggerOptions {
  level?: Level;
  pretty?: boolean;
  base?: LogFields;
}

export function createLogger(opts: LoggerOptions = {}): Logger {
  const level = opts.level ?? (process.env.NODE_ENV === "production" ? "info" : "debug");
  const pretty = opts.pretty ?? process.env.NODE_ENV !== "production";
  const base = opts.base ?? {};

  function emit(lvl: Level, msg: string, fields?: LogFields): void {
    if (ORDER[lvl] < ORDER[level]) return;
    const record = { level: lvl, time: new Date().toISOString(), msg, ...base, ...fields };
    if (pretty) {
      const extra = { ...base, ...fields };
      const tail = Object.keys(extra).length ? " " + JSON.stringify(extra) : "";
      // eslint-disable-next-line no-console
      console[lvl === "debug" ? "log" : lvl](`[${lvl}] ${msg}${tail}`);
    } else {
      // eslint-disable-next-line no-console
      console[lvl === "debug" ? "log" : lvl](JSON.stringify(record));
    }
  }

  return {
    debug: (m, f) => emit("debug", m, f),
    info: (m, f) => emit("info", m, f),
    warn: (m, f) => emit("warn", m, f),
    error: (m, f) => emit("error", m, f),
    child: (fields) => createLogger({ level, pretty, base: { ...base, ...fields } }),
  };
}
