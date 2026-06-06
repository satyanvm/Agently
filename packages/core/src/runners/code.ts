import type { Runner } from "./types.js";

/**
 * 🔌 SEAM: arbitrary user-supplied code.
 * Not part of the MVP and intentionally inert. Real implementation REQUIRES a
 * sandbox (isolated-vm, Firecracker, or a container per run) — never run
 * untrusted code in the worker process. Left as a stub to mark the boundary.
 */
export const codeRunner: Runner = {
  type: "code",
  async run() {
    throw new Error(
      'The "code" runner is not implemented yet (needs sandboxing — post-MVP).',
    );
  },
};
