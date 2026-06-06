import type { Runner } from "./types.js";

/**
 * 🔌 SEAM: predefined task types (scrape URL, summarize, etc.).
 * Not part of the MVP. Registered so the architecture is visibly pluggable;
 * dispatch on `config.task` to a small catalog of handlers when implemented.
 */
export const taskRunner: Runner = {
  type: "task",
  async run() {
    throw new Error(
      'The "task" runner is not implemented yet (predefined task types — post-MVP).',
    );
  },
};
