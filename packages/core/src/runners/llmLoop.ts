import type { Runner, RunnerContext, RunnerResult } from "./types.js";

/**
 * Default MVP runner. An "agent" is configuration: a system prompt, a model,
 * and (later) a set of tools. A run drives the model in a loop, streaming each
 * step as logs, until the model signals completion or a max-iteration cap.
 *
 * Phase 4 wires the real provider call (Anthropic/OpenAI) where marked. For now
 * this is a deterministic stub so the end-to-end pipeline — enqueue → claim →
 * run → logs → completion — works before any API key exists.
 */

interface LlmLoopConfig {
  model?: string;
  systemPrompt?: string;
  maxIterations?: number;
}

export const llmLoopRunner: Runner = {
  type: "llm-loop",

  async run(ctx: RunnerContext): Promise<RunnerResult> {
    const config = ctx.agent.config as LlmLoopConfig;
    const model = config.model ?? "claude-haiku-4-5-20251001";
    const maxIterations = config.maxIterations ?? 3;

    await ctx.log("info", `Starting llm-loop with model "${model}"`);

    for (let i = 1; i <= maxIterations; i++) {
      if (await ctx.isCanceled()) {
        await ctx.log("warn", "Run canceled — stopping loop.");
        return { result: { stopped: "canceled", iterations: i - 1 } };
      }

      await ctx.log("info", `Iteration ${i}/${maxIterations}`);

      // ── Phase 4: replace this block with a real provider call ──────────────
      // const reply = await callModel(model, config.systemPrompt, history);
      // await ctx.log("debug", reply.text);
      // if (reply.done) break;
      await new Promise((r) => setTimeout(r, 500));
      await ctx.log("debug", `(stub) model step ${i} produced no tool calls`);
      // ───────────────────────────────────────────────────────────────────────
    }

    await ctx.log("info", "Loop complete.");
    return { result: { iterations: maxIterations, model } };
  },
};
