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
from temporalio.exceptions import ActivityError

# Pure logic (reasoner.plan) and the activity definitions are imported under the
# sandbox pass-through: they must not be re-validated/instrumented by the workflow
# sandbox. `plan` is IO-free; `activities` only supplies the activity references and
# their (dataclass) payload types — the workflow never calls their bodies directly.
with workflow.unsafe.imports_passed_through():
    # NB: use absolute `import reasoner.X`, NOT `from . import X`. The relative
    # form resolves to importing the parent package `reasoner` (already in the
    # sandbox's sys.modules), so the sandbox importer skips its passthrough check
    # and instead *executes* the whole node stack (engine→nodes→catalog) inside
    # the workflow sandbox — where catalog's import-time Path.resolve()/open()
    # calls are restricted. The absolute form resolves to `reasoner.activities`
    # (not yet in sys.modules), which correctly routes through passthrough.
    import reasoner.activities as activities
    import reasoner.plan as plan

# Hardcoded (not imported from .graph) so the workflow sandbox never pulls in the
# node modules' non-deterministic deps (psycopg, httpx, playwright). Must match
# reasoner.graph.GRAPH_NAME.
GRAPH_NAME = "agently-reason"

# Per-node timeout: generous enough for a slow browse, retried a few times. Each
# node is its own durable unit, so a retry re-runs only that node.
_NODE_TIMEOUT = timedelta(seconds=600)
_NODE_RETRY = RetryPolicy(maximum_attempts=3)
_IO_TIMEOUT = timedelta(seconds=60)

# Cross-queue `execute_piece` call (docs/pieces-runtime-contract.md §3): a tight
# schedule_to_start catches "no pieces worker is polling" quickly so the run can
# fail with an actionable reason instead of hanging for the full node timeout.
_PIECE_PREFIX = "pieces."
_PIECE_SCHEDULE_TO_START = timedelta(seconds=30)
_PIECE_TIMEOUT = timedelta(seconds=180)
_PIECE_RETRY = RetryPolicy(maximum_attempts=3)
_DEFAULT_PIECES_QUEUE = "agently-pieces"


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
        # Which task queue the Node pieces worker polls (contract §3). Part of the
        # workflow INPUT (threaded by the dispatcher from config) — never an env
        # read here, so replays stay deterministic.
        self._pieces_queue: str = params.get("pieces_task_queue") or _DEFAULT_PIECES_QUEUE

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
        fanned_out: set[str] = set()  # loop-body nodes run by the fan-out below
        done = 0
        for node in ordered:
            key = node["key"]
            agent_id = agent_ids[key]
            deps = list(node.get("dependsOn") or [])

            if key in fanned_out:
                done += 1
                await self._progress(run_id, done, total, f"Ran {key} (loop body)")
                continue

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
            result = await self._run_one(
                run_id, node, agent_id, upstream, run_input, extra=None,
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

            # logic.loop: fan its dominated body out once per item, each body-node
            # execution its own durable activity. The items list is part of this
            # loop node's activity result, so a replay reconstructs the identical
            # fan-out from history — the workflow stays deterministic.
            if str(node.get("type", "")) == "logic.loop":
                body_keys = plan.loop_body(key, ordered, dependents)
                fanned_out.update(body_keys)
                by_key = {n["key"]: n for n in ordered}
                items = list(outputs[key].get("items") or [])
                results: list[Any] = []
                loop_failed = False
                for idx, item in enumerate(items):
                    extra = {"item": item, "loop": {key: {"index": idx}}}
                    iter_outputs: dict[str, dict[str, Any]] = {}
                    iter_skipped: set[str] = set()
                    for bkey in body_keys:
                        if bkey in iter_skipped:
                            continue
                        bnode = by_key[bkey]
                        bdeps = list(bnode.get("dependsOn") or [])
                        bupstream = {
                            dep: (iter_outputs.get(dep) if dep in iter_outputs else outputs.get(dep, {}))
                            for dep in bdeps
                        }
                        bresult = await self._run_one(
                            run_id, bnode, agent_ids[bkey], bupstream, run_input, extra=extra,
                        )
                        if bresult.failed:
                            await workflow.execute_activity(
                                activities.finish_run,
                                activities.FinishInput(run_id=run_id, status="failed", step="Failed", error=bresult.error),
                                start_to_close_timeout=_IO_TIMEOUT,
                                retry_policy=_NODE_RETRY,
                            )
                            loop_failed = True
                            break
                        iter_outputs[bkey] = bresult.output or {}
                        if not bresult.gate_open:
                            for pruned in plan.descendants(bkey, dependents):
                                if pruned in body_keys:
                                    iter_skipped.add(pruned)
                    if loop_failed:
                        return {"run_id": run_id, "done": False, "failed_node": key}
                    outputs.update(iter_outputs)
                    if len(iter_outputs) == 1:
                        results.append(next(iter(iter_outputs.values())))
                    else:
                        results.append(iter_outputs)
                outputs[key]["results"] = results
                if not items:
                    for bkey in body_keys:
                        await workflow.execute_activity(
                            activities.skip_node,
                            activities.SkipNodeInput(run_id=run_id, agent_id=agent_ids[bkey], key=bkey),
                            start_to_close_timeout=_IO_TIMEOUT,
                            retry_policy=_NODE_RETRY,
                        )
                        outputs[bkey] = {"skipped": True}

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

    async def _run_one(
        self,
        run_id: str,
        node: dict[str, Any],
        agent_id: str,
        upstream: dict[str, dict[str, Any]],
        run_input: dict[str, Any],
        extra: dict[str, Any] | None,
    ):
        """Execute one node as its durable activity unit.

        Two backends, one orchestrator: `pieces.*` types run on the Node pieces
        worker via the cross-queue `execute_piece` activity (bracketed by the
        prepare/record activities on our own queue); everything else runs the
        local `run_node` activity. Both return the same RunNodeResult shape, so
        the loop/gate/skip logic above is backend-agnostic.
        """
        if str(node.get("type", "")).startswith(_PIECE_PREFIX):
            return await self._run_piece_node(run_id, node, agent_id, upstream, run_input, extra)
        return await workflow.execute_activity(
            activities.run_node,
            activities.RunNodeInput(
                run_id=run_id, node=node, agent_id=agent_id,
                upstream=upstream, run_input=run_input, extra=extra or {},
            ),
            start_to_close_timeout=_NODE_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )

    async def _run_piece_node(
        self,
        run_id: str,
        node: dict[str, Any],
        agent_id: str,
        upstream: dict[str, dict[str, Any]],
        run_input: dict[str, Any],
        extra: dict[str, Any] | None,
    ):
        """prepare (our queue) → execute_piece (pieces queue) → record (our queue).

        A missing pieces worker is a run failure. The activity error is converted
        into the same failed-node result shape as local execution so the run row
        records the queue/provider reason before the workflow returns.
        """
        try:
            prep = await workflow.execute_activity(
                activities.prepare_piece_node,
                activities.PreparePieceInput(
                    run_id=run_id, node=node, agent_id=agent_id,
                    upstream=upstream, run_input=run_input, extra=extra or {},
                ),
                start_to_close_timeout=_IO_TIMEOUT,
                retry_policy=_NODE_RETRY,
            )
        except ActivityError as exc:
            return activities.RunNodeResult(
                failed=True, error=f"{node.get('type')}: prepare failed: {exc}"
            )

        if prep.mode == "execute":
            try:
                result = await workflow.execute_activity(
                    "execute_piece",           # by name: implemented by the Node worker
                    prep.payload,
                    task_queue=self._pieces_queue,
                    schedule_to_start_timeout=_PIECE_SCHEDULE_TO_START,
                    start_to_close_timeout=_PIECE_TIMEOUT,
                    retry_policy=_PIECE_RETRY,
                )
                mode = "execute"
            except ActivityError as exc:
                result = {
                    "ok": False,
                    "errorType": "PiecesWorkerUnavailable",
                    "error": f"execute_piece failed: {exc}",
                }
                mode = "execute"
        else:
            result, mode = prep.result, "trigger"

        return await workflow.execute_activity(
            activities.record_piece_result,
            activities.RecordPieceInput(
                run_id=run_id, node=node, agent_id=agent_id, result=result, mode=mode,
            ),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )

    async def _progress(self, run_id: str, done: int, total: int, step: str) -> None:
        await workflow.execute_activity(
            activities.set_progress,
            activities.ProgressInput(run_id=run_id, done=done, total=total, step=step),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
