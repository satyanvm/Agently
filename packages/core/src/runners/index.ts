import { registerRunner } from "./registry.js";
import { llmLoopRunner } from "./llmLoop.js";
import { taskRunner } from "./task.js";
import { codeRunner } from "./code.js";

/**
 * Register all known runners. Importing this module wires the registry.
 * The MVP only *implements* llm-loop; task/code are registered stubs that
 * throw if invoked, so the seams are real and discoverable.
 */
registerRunner(llmLoopRunner);
registerRunner(taskRunner);
registerRunner(codeRunner);

export * from "./types.js";
export * from "./registry.js";
export { llmLoopRunner, taskRunner, codeRunner };
