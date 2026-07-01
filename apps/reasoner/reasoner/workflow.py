"""The Temporal workflows that drive the reasoning graphs.

Two workflows, one per execution shape:

  • `ReasoningWorkflow` — the STATIC path. Compiles the registered LangGraph graph
    and invokes it; the LangGraphPlugin runs each static node as its own activity.
    The `route` node inside that graph decides static-vs-dynamic; a composed run is
    steered to the dynamic workflow below (started by the dispatcher).

  • `DynamicWorkflow` — the DYNAMIC path with **per-node durability**. It drives the
    topological loop in deterministic *workflow* code and invokes ONE activity per
    user node (`activities.run_node`). Each node therefore lands its own entry in
    Temporal's event history: kill the worker mid-run and Temporal replays this loop,
    serving already-completed node activities from history (NOT re-running them) and
    resuming at the in-flight node. That is real per-node checkpointing — it replaces
    the old single-activity `dynamic_node`, which re-ran the whole composed DAG on a
    crash.

The workflow bodies are deterministic: no DB, no httpx, no wall-clock, no random.
Ordering/skip logic comes from `reasoner.plan` (pure, IO-free); the composed graph
reaches the workflow via the `load_graph` activity (not a DB call in workflow
context). Every side effect happens inside an activity.
"""
from __future__ import annotations

from datetime import timedelta
from typing import Any

from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.contrib.langgraph import graph

# Pure logic (reasoner.plan) and the activity definitions are imported under the
# sandbox pass-through: they must not be re-validated/instrumented by the workflow
# sandbox. `plan` is IO-free; `activities` only supplies the activity references and
# their (dataclass) payload types — the workflow never calls their bodies directly.
with workflow.unsafe.imports_passed_through():
    from . import activities, plan

# Hardcoded (not imported from .graph) so the workflow sandbox never pulls in the
# node modules' non-deterministic deps (psycopg, httpx, playwright). Must match
# reasoner.graph.GRAPH_NAME.
GRAPH_NAME = "agently-reason"

# Per-node timeout: generous enough for a slow browse, retried a few times. Each
# node is its own durable unit, so a retry re-runs only that node.
_NODE_TIMEOUT = timedelta(seconds=600)
_NODE_RETRY = RetryPolicy(maximum_attempts=3)
_IO_TIMEOUT = timedelta(seconds=60)


@workflow.defn
class ReasoningWorkflow:
    @workflow.run
    async def run(self, params: dict[str, Any]) -> dict[str, Any]:
        run_id = params["run_id"]
        initial = {"run_id": run_id, "input": params.get("input", {})}
        result = await graph(GRAPH_NAME).compile().ainvoke(initial)
        return {"run_id": run_id, "done": bool(result.get("done", False))}


@workflow.defn
class DynamicWorkflow:
    """Per-node orchestrator for user-composed DAGs (see module docstring)."""

    @workflow.run
    async def run(self, params: dict[str, Any]) -> dict[str, Any]:
        run_id = params["run_id"]
        run_input = params.get("input", {})

        # 1) Resolve the graph + seed agents OUTSIDE the workflow (one activity).
        loaded = await workflow.execute_activity(
            activities.load_graph,
            activities.LoadGraphInput(run_id=run_id),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        if not loaded.dynamic:
            # No composed graph — nothing for this workflow to do. (The dispatcher
            # only routes composed runs here, so this is a defensive no-op.)
            return {"run_id": run_id, "done": False, "dynamic": False}

        ordered = loaded.graph               # already topologically ordered
        agent_ids = loaded.agent_ids         # node key → agent id
        prior = loaded.prior_status          # agent id → status (for resume)
        dependents = plan.dependents_of(ordered)
        total = len(ordered)

        # 2) Drive the deterministic loop; each node is its own activity.
        outputs: dict[str, dict[str, Any]] = {}
        skipped: set[str] = set()
        done = 0
        for node in ordered:
            key = node["key"]
            agent_id = agent_ids[key]
            deps = list(node.get("dependsOn") or [])

            # Pruned by an upstream gate (directly, or all deps skipped).
            if plan.should_skip(node, skipped):
                skipped.add(key)
                skipped.update(plan.descendants(key, dependents))
                await workflow.execute_activity(
                    activities.skip_node,
                    activities.SkipNodeInput(run_id=run_id, agent_id=agent_id, key=key),
                    start_to_close_timeout=_IO_TIMEOUT,
                    retry_policy=_NODE_RETRY,
                )
                outputs[key] = {"skipped": True}
                done += 1
                await self._progress(run_id, done, total, f"Skipped {key}")
                continue

            # Resume: a node already marked succeeded in a prior attempt is served
            # from Temporal history for the activity below, but if the WHOLE workflow
            # was restarted fresh (not a replay) the prior DB status still lets us
            # avoid redoing its side effects.
            if prior.get(agent_id) == "succeeded":
                outputs[key] = {}
                done += 1
                await self._progress(run_id, done, total, f"Resumed {key}")
                continue

            upstream = {dep: outputs.get(dep, {}) for dep in deps}
            result = await workflow.execute_activity(
                activities.run_node,
                activities.RunNodeInput(
                    run_id=run_id, node=node, agent_id=agent_id,
                    upstream=upstream, run_input=run_input,
                ),
                start_to_close_timeout=_NODE_TIMEOUT,
                retry_policy=_NODE_RETRY,
            )

            if result.failed:
                await workflow.execute_activity(
                    activities.finish_run,
                    activities.FinishInput(run_id=run_id, status="failed", step="Failed", error=result.error),
                    start_to_close_timeout=_IO_TIMEOUT,
                    retry_policy=_NODE_RETRY,
                )
                return {"run_id": run_id, "done": False, "failed_node": key}

            outputs[key] = result.output or {}

            # Gate whose condition was false → prune its downstream subgraph.
            if not result.gate_open:
                skipped.update(plan.descendants(key, dependents))

            # Hand-off edges for the live graph view (one activity per edge keeps
            # each write independently durable and idempotent).
            for child_key in dependents[key]:
                await workflow.execute_activity(
                    activities.add_edge,
                    activities.EdgeInput(
                        run_id=run_id, from_agent_id=agent_id,
                        to_agent_id=agent_ids[child_key], label=f"{key} → {child_key}",
                    ),
                    start_to_close_timeout=_IO_TIMEOUT,
                    retry_policy=_NODE_RETRY,
                )

            done += 1
            await self._progress(run_id, done, total, f"Ran {key}")

        # 3) Finish the run.
        await workflow.execute_activity(
            activities.finish_run,
            activities.FinishInput(run_id=run_id, status="succeeded", step="Completed"),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        return {"run_id": run_id, "done": True, "skipped": sorted(skipped)}

    async def _progress(self, run_id: str, done: int, total: int, step: str) -> None:
        await workflow.execute_activity(
            activities.set_progress,
            activities.ProgressInput(run_id=run_id, done=done, total=total, step=step),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
