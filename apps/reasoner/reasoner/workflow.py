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

import asyncio
from datetime import datetime, timedelta
from typing import Any

from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.contrib.langgraph import graph
from temporalio.exceptions import ActivityError, TimeoutError as TemporalTimeoutError, TimeoutType

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
    import reasoner.approval as approval
    import reasoner.plan as plan
    import reasoner.waits as waits

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

# human.approval: opened without blocking, waited on (Temporal signal or
# timeout) only when a node that depends on it is reached — see _resolve.
_APPROVAL_TYPE = "human.approval"
# logic.wait: a durable timer in workflow code (_wait).
_WAIT_TYPE = "logic.wait"

# Runs started after this patch execute independent nodes concurrently; runs
# already in flight when it shipped replay the original one-at-a-time loop.
_PARALLEL_PATCH = "parallel-nodes-v1"
# Upper bound on nodes executing at once. The dispatcher passes the configured
# value as workflow input (RUN_MAX_PARALLEL) so a replay always sees the same one.
_DEFAULT_MAX_PARALLEL = 8

# Runs started after this patch (a) run publishing steps exactly once, with no
# Temporal or node-level retry, since a retry after a timeout can post twice;
# and (b) in runs with approvals, let a failed step skip only what depends on
# it, so the run's other drafts still reach a decision and publish. Runs in
# flight when it shipped replay the old behaviour.
_PUBLISH_SAFETY_PATCH = "publish-safety-v1"


def _activity_reason(exc: ActivityError) -> str:
    """The underlying reason of a failed activity. ActivityError's own message
    is just "Activity task failed"; the activity's error (or the timeout) is
    its cause."""
    cause = exc.cause
    if cause is None:
        return str(exc)
    return getattr(cause, "message", "") or str(cause) or type(cause).__name__


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

    def __init__(self) -> None:
        # approval id → woken. Filled by the approval_decided signal (or at open
        # when the approval was already decided before a restart).
        self._decisions: set[str] = set()
        # node key → (approval id, deadline, agent id, onTimeout, title) for
        # approvals opened but not yet resolved.
        self._pending: dict[str, tuple[str, datetime, str, str, str]] = {}
        # approval node key → id of its decided approval (the latest item's,
        # inside a loop), and each node's ancestors: what runs after a
        # decision is recorded against it (approval_outcomes).
        self._resolved: dict[str, str] = {}
        self._ancestors: dict[str, set[str]] = {}
        # Set in run() under _PUBLISH_SAFETY_PATCH.
        self._publishing: set[str] = set()
        self._keep_going = False

    @workflow.signal
    def approval_decided(self, payload: dict[str, Any]) -> None:
        """Sent by the dispatcher's relay once a person decides in the Inbox.
        Only wakes the run: the decision itself is read back from Postgres."""
        approval_id = str((payload or {}).get("approvalId") or "")
        if approval_id:
            self._decisions.add(approval_id)

    @workflow.run
    async def run(self, params: dict[str, Any]) -> dict[str, Any]:
        run_id = params["run_id"]
        run_input = params.get("input", {})
        # Which task queue the Node pieces worker polls (contract §3). Part of the
        # workflow INPUT (threaded by the dispatcher from config) — never an env
        # read here, so replays stay deterministic.
        self._pieces_queue: str = params.get("pieces_task_queue") or _DEFAULT_PIECES_QUEUE
        # Builder test-run options (apps/api node_data.go → dispatcher):
        # targetNode = run only it and its ancestors, reusing `seed` outputs for
        # ancestors; usePins = pinned nodes return their pinned data.
        exec_opts: dict[str, Any] = params.get("exec") or {}
        self._use_pins = bool(exec_opts.get("usePins"))
        self._seed: dict[str, Any] = exec_opts.get("seed") or {}
        target: str = exec_opts.get("targetNode") or ""

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
        if loaded.error:
            return await self._fail_run(run_id, "", loaded.error)

        self._target = target
        has_approval = any(n.get("type") == _APPROVAL_TYPE for n in loaded.graph)
        safety = workflow.patched(_PUBLISH_SAFETY_PATCH)
        self._publishing = set(loaded.publishing) if safety else set()
        # Builder test runs keep failing fast: they're for seeing one step work.
        self._keep_going = safety and has_approval and not (target or self._use_pins)
        # Approvals and builder test runs keep the one-pass-at-a-time loop: it
        # opens every reachable approval before blocking on any, and step runs
        # / pins are served there. Everything else runs in parallel. The choice
        # depends only on the workflow input and load_graph's result (both in
        # history), so a replay takes the same path.
        if workflow.patched(_PARALLEL_PATCH):
            if not (has_approval or target or self._use_pins):
                max_parallel = max(1, int(params.get("max_parallel") or _DEFAULT_MAX_PARALLEL))
                return await self._run_parallel(run_id, run_input, loaded, max_parallel)
        return await self._run_sequential(run_id, run_input, loaded)

    async def _run_sequential(
        self, run_id: str, run_input: dict[str, Any], loaded: Any,
    ) -> dict[str, Any]:
        """One node at a time, setting aside nodes that wait on an undecided
        approval. Also replays runs started before _PARALLEL_PATCH."""
        target = self._target
        ordered = loaded.graph               # already topologically ordered
        agent_ids = loaded.agent_ids         # node key → agent id
        prior = loaded.prior_status          # agent id → status (for resume)
        dependents = plan.dependents_of(ordered)
        total = len(ordered)
        self._ancestors = {n["key"]: plan.ancestors(n["key"], ordered) for n in ordered}
        include = (plan.ancestors(target, ordered) | {target}) if target else None

        # 2) Drive the deterministic loop; each node is its own activity.
        outputs: dict[str, dict[str, Any]] = {}
        skipped: set[str] = set()
        fanned_out: set[str] = set()  # loop-body nodes run by the fan-out below
        failures: list[tuple[str, str]] = []  # (key, error) when _keep_going
        done = 0
        # Nodes waiting on an undecided approval are set aside and the pass
        # goes on, so every other branch (and every other approval request)
        # moves first. Only when nothing else can run does the workflow block,
        # on the earliest-opened approval that is holding something up.
        remaining = list(ordered)
        while remaining:
            blocked: set[str] = set()
            deferred: list[dict[str, Any]] = []
            for node in remaining:
                key = node["key"]
                agent_id = agent_ids[key]
                deps = list(node.get("dependsOn") or [])

                if key not in fanned_out and any(d in self._pending or d in blocked for d in deps):
                    blocked.add(key)
                    deferred.append(node)
                    continue

                if key in fanned_out:
                    done += 1
                    await self._progress(run_id, done, total, f"Ran {key} (loop body)")
                    continue

                # Step run: nodes the target doesn't depend on don't run.
                if include is not None and key not in include:
                    skipped.add(key)
                    await workflow.execute_activity(
                        activities.skip_node,
                        activities.SkipNodeInput(run_id=run_id, agent_id=agent_id, key=key,
                                                 reason="not part of this step run"),
                        start_to_close_timeout=_IO_TIMEOUT,
                        retry_policy=_NODE_RETRY,
                    )
                    outputs[key] = {"skipped": True}
                    done += 1
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
                    await self._outcome(run_id, node, agent_id, "skipped")
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

                if node.get("type") == _APPROVAL_TYPE and not self._has_shortcut(node):
                    opened = await self._open_approval(run_id, node, agent_id, upstream, run_input, None, 0)
                    if opened.failed:
                        return await self._fail_run(run_id, key, opened.error)
                    outputs[key] = {"approvalId": opened.approval_id, "pending": True}
                    await self._edges(run_id, key, agent_id, dependents, agent_ids)
                    done += 1
                    await self._progress(run_id, done, total, f"Opened approval {key}")
                    continue

                result = await self._execute(
                    run_id, node, agent_id, upstream, run_input, extra=None, shortcuts=True,
                )

                if result.failed:
                    await self._outcome(run_id, node, agent_id, "failed")
                    if self._keep_going:
                        # Only what depends on this step is lost; every other
                        # draft still waits for its decision and publishes.
                        failures.append((key, result.error))
                        skipped.update(plan.descendants(key, dependents))
                        done += 1
                        await self._progress(run_id, done, total, f"Failed {key}; carrying on with the rest")
                        continue
                    await workflow.execute_activity(
                        activities.finish_run,
                        activities.FinishInput(run_id=run_id, status="failed", step="Failed", error=result.error),
                        start_to_close_timeout=_IO_TIMEOUT,
                        retry_policy=_NODE_RETRY,
                    )
                    return {"run_id": run_id, "done": False, "failed_node": key}

                outputs[key] = result.output or {}
                await self._outcome(run_id, node, agent_id, _status_of(result))

                # logic.loop: fan its dominated body out once per item, each body-node
                # execution its own durable activity. The items list is part of this
                # loop node's activity result, so a replay reconstructs the identical
                # fan-out from history — the workflow stays deterministic (_fan_out).
                if str(node.get("type", "")) == "logic.loop":
                    body_keys = plan.loop_body(key, ordered, dependents)
                    fanned_out.update(body_keys)
                    error = await self._fan_out(run_id, key, ordered, dependents, agent_ids, body_keys,
                                                outputs, run_input)
                    if error is not None:
                        return await self._fail_run(run_id, key, error)

                # Gate whose condition was false → prune its downstream subgraph.
                if not result.gate_open:
                    skipped.update(plan.descendants(key, dependents))

                await self._edges(run_id, key, agent_id, dependents, agent_ids)

                done += 1
                await self._progress(run_id, done, total, f"Ran {key}")


            remaining = deferred
            if remaining:
                waiting_on = {n["key"] for n in remaining}
                key = next((k for k in self._pending if plan.descendants(k, dependents) & waiting_on),
                           next(iter(self._pending)))
                decided = await self._resolve(run_id, key)
                if decided.failed:
                    return await self._fail_run(run_id, key, decided.error)
                outputs[key] = decided.output or {}
                if not decided.gate_open:
                    skipped.update(plan.descendants(key, dependents))

        # Approvals nothing downstream waited on still get their decision (or
        # timeout) recorded before the run finishes.
        for key in list(self._pending):
            decided = await self._resolve(run_id, key)
            if decided.failed:
                return await self._fail_run(run_id, key, decided.error)

        if failures:
            first_key, first_error = failures[0]
            more = f" ({len(failures)} steps failed; see the run)" if len(failures) > 1 else ""
            return await self._fail_run(run_id, first_key, first_error + more)

        # 3) Finish the run.
        await workflow.execute_activity(
            activities.finish_run,
            activities.FinishInput(run_id=run_id, status="succeeded",
                                   step=f"Step run of {target} finished" if target else "Completed"),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        return {"run_id": run_id, "done": True, "skipped": sorted(skipped)}

    async def _run_parallel(
        self, run_id: str, run_input: dict[str, Any], loaded: Any, max_parallel: int,
    ) -> dict[str, Any]:
        """Run every node as soon as its dependencies finish, up to max_parallel
        at once. Only for graphs without approvals, outside builder test runs
        (see run).

        Same semantics as _run_sequential (skips, gates, resume, node policies,
        waits, loop fan-out, fail-fast), but independent branches overlap
        instead of queueing. Each node is a workflow task that waits on its
        dependencies' events; Temporal replays activity completions in history
        order, so the schedule is deterministic. After the first failure no new
        node starts; nodes already executing finish, then the run is marked
        failed.
        """
        ordered = loaded.graph
        agent_ids = loaded.agent_ids
        prior = loaded.prior_status
        dependents = plan.dependents_of(ordered)
        total = len(ordered)

        outputs: dict[str, dict[str, Any]] = {}
        skipped: set[str] = set()
        fanned_out: set[str] = set()
        finished = {n["key"]: asyncio.Event() for n in ordered}
        slots = asyncio.Semaphore(max_parallel)
        failures: list[tuple[str, str]] = []
        done = 0

        async def visit(node: dict[str, Any]) -> None:
            nonlocal done
            key = node["key"]
            agent_id = agent_ids[key]
            deps = list(node.get("dependsOn") or [])
            try:
                for dep in deps:
                    await finished[dep].wait()
                if failures:
                    return
                if key in fanned_out:
                    done += 1
                    await self._progress(run_id, done, total, f"Ran {key} (loop body)")
                    return
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
                    return
                if prior.get(agent_id) == "succeeded":
                    outputs[key] = {}
                    done += 1
                    await self._progress(run_id, done, total, f"Resumed {key}")
                    return

                upstream = {dep: outputs.get(dep, {}) for dep in deps}
                async with slots:
                    if failures:
                        return
                    try:
                        result = await self._execute(run_id, node, agent_id, upstream, run_input,
                                                     extra=None, shortcuts=False)
                    except ActivityError as exc:
                        # An activity that exhausted its retries fails the node,
                        # not the whole workflow, so the run ends with a reason.
                        result = activities.RunNodeResult(
                            failed=True, error=f"{node.get('type')}: {_activity_reason(exc)}")
                if result.failed:
                    failures.append((key, result.error))
                    return
                outputs[key] = result.output or {}

                if str(node.get("type", "")) == "logic.loop":
                    body_keys = plan.loop_body(key, ordered, dependents)
                    fanned_out.update(body_keys)
                    error = await self._fan_out(run_id, key, ordered, dependents, agent_ids, body_keys,
                                                outputs, run_input, slots=slots)
                    if error is not None:
                        failures.append((key, error))
                        return

                if not result.gate_open:
                    skipped.update(plan.descendants(key, dependents))

                await self._edges(run_id, key, agent_id, dependents, agent_ids)
                done += 1
                await self._progress(run_id, done, total, f"Ran {key}")
            finally:
                # Always release dependents, so a failure or skip never strands them.
                finished[key].set()

        await asyncio.gather(*(visit(n) for n in ordered))

        if failures:
            failed_key, error = failures[0]
            return await self._fail_run(run_id, failed_key, error)

        await workflow.execute_activity(
            activities.finish_run,
            activities.FinishInput(run_id=run_id, status="succeeded", step="Completed"),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        return {"run_id": run_id, "done": True, "skipped": sorted(skipped)}

    async def _edges(self, run_id: str, key: str, agent_id: str,
                     dependents: dict[str, list[str]], agent_ids: dict[str, str]) -> None:
        """Hand-off edges for the live graph view (one activity per edge keeps
        each write independently durable and idempotent)."""
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

    async def _fail_run(self, run_id: str, key: str, error: str) -> dict[str, Any]:
        await workflow.execute_activity(
            activities.finish_run,
            activities.FinishInput(run_id=run_id, status="failed", step="Failed", error=error),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        return {"run_id": run_id, "done": False, "failed_node": key}

    def _has_shortcut(self, node: dict[str, Any]) -> bool:
        """Test runs serve pinned / reused outputs instead of executing — an
        approval included (no Inbox row for a builder test with pinned data)."""
        key = node["key"]
        if self._use_pins and isinstance(node.get("pinned"), dict):
            return True
        return bool(self._target and key != self._target and isinstance(self._seed.get(key), dict))

    async def _open_approval(self, run_id: str, node: dict[str, Any], agent_id: str,
                             upstream: dict[str, Any], run_input: dict[str, Any],
                             extra: dict[str, Any] | None, iteration: int) -> approval.OpenApprovalResult:
        opened = await workflow.execute_activity(
            approval.open_approval,
            approval.OpenApprovalInput(run_id=run_id, node=node, agent_id=agent_id, upstream=upstream,
                                       run_input=run_input, extra=extra or {}, iteration=iteration),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        if not opened.failed:
            if opened.status != "pending":
                # Decided before this workflow (re)started: no need to wait.
                self._decisions.add(opened.approval_id)
            on_timeout = str((node.get("config") or {}).get("onTimeout") or "skip")
            deadline = workflow.now() + timedelta(seconds=opened.timeout_s)
            self._pending[node["key"]] = (opened.approval_id, deadline, agent_id, on_timeout, opened.title)
        return opened

    async def _resolve(self, run_id: str, key: str) -> activities.RunNodeResult:
        return await self._resolve_entry(run_id, key, self._pending.pop(key))

    async def _resolve_entry(self, run_id: str, key: str,
                             entry: tuple[str, datetime, str, str, str]) -> activities.RunNodeResult:
        """Block until the approval `entry` is decided or times out, then turn
        the decision into the node's output (a gate: approved opens it,
        rejected / timed out prunes everything downstream)."""
        approval_id, deadline, agent_id, on_timeout, title = entry
        self._resolved[key] = approval_id
        timed_out = False
        if approval_id not in self._decisions:
            await workflow.execute_activity(
                approval.set_waiting,
                approval.SetWaitingInput(run_id=run_id, waiting=True, step=f"Waiting for approval: {title}"),
                start_to_close_timeout=_IO_TIMEOUT, retry_policy=_NODE_RETRY,
            )
            remaining = (deadline - workflow.now()).total_seconds()
            try:
                if remaining <= 0:
                    raise asyncio.TimeoutError
                await workflow.wait_condition(lambda: approval_id in self._decisions,
                                              timeout=timedelta(seconds=remaining))
            except asyncio.TimeoutError:
                timed_out = True
            await workflow.execute_activity(
                approval.set_waiting,
                approval.SetWaitingInput(run_id=run_id, waiting=False, step=f"Approval received: {title}"),
                start_to_close_timeout=_IO_TIMEOUT, retry_policy=_NODE_RETRY,
            )
        return await workflow.execute_activity(
            approval.resolve_approval,
            approval.ResolveApprovalInput(run_id=run_id, agent_id=agent_id, key=key, approval_id=approval_id,
                                          on_timeout=on_timeout, timed_out=timed_out),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )

    async def _outcome(self, run_id: str, node: dict[str, Any], agent_id: str, status: str,
                       iteration: int = 0) -> None:
        """Record a node that ran (or was skipped) after one or more decisions
        against each of those approvals. Nodes with no decided approval
        upstream record nothing (and add nothing to history)."""
        if node.get("type") == _APPROVAL_TYPE:
            return
        ids = sorted({self._resolved[a] for a in self._ancestors.get(node["key"], set()) if a in self._resolved})
        if not ids:
            return
        await workflow.execute_activity(
            approval.record_outcome,
            approval.RecordOutcomeInput(
                run_id=run_id, agent_id=agent_id, key=node["key"], node_type=str(node.get("type") or ""),
                status=status, approval_ids=ids,
                # Minted in workflow code (deterministic on replay) so a retried
                # activity can't insert the same outcome twice.
                outcome_ids=[f"out_{workflow.uuid4().hex[:20]}" for _ in ids],
            ),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )

    async def _fan_out(self, run_id: str, loop_key: str, ordered: list[dict[str, Any]],
                       dependents: dict[str, list[str]], agent_ids: dict[str, str], body_keys: list[str],
                       outputs: dict[str, dict[str, Any]], run_input: dict[str, Any],
                       slots: asyncio.Semaphore | None = None) -> str | None:
        """Run a loop's body once per item. Returns the error that should fail
        the run, or None. With `slots`, each body execution takes a slot so the
        parallel path's concurrency cap still holds.

        Like the top-level pass, an item's nodes that wait on its undecided
        approval are set aside: every item first runs up to its approvals, so
        all of them reach the Inbox together. Then items are finished in order,
        each blocking only on its own approvals. Without this, item 2's draft
        wouldn't reach the Inbox until item 1 had been decided (and, with a
        logic.wait in the body, published)."""
        by_key = {n["key"]: n for n in ordered}
        items = list(outputs[loop_key].get("items") or [])
        iters: list[dict[str, Any]] = []
        for idx, item in enumerate(items):
            it: dict[str, Any] = {
                "idx": idx, "extra": {"item": item, "loop": {loop_key: {"index": idx}}},
                "outputs": {}, "skipped": set(), "pending": {}, "remaining": list(body_keys),
            }
            error = await self._advance(run_id, it, by_key, dependents, agent_ids, body_keys, outputs,
                                        run_input, slots)
            if error is not None:
                return error
            iters.append(it)
        for it in iters:
            while it["pending"]:
                bkey = next(iter(it["pending"]))
                decided = await self._resolve_entry(run_id, bkey, it["pending"].pop(bkey))
                if decided.failed:
                    return decided.error
                it["outputs"][bkey] = decided.output or {}
                if not decided.gate_open:
                    it["skipped"].update(k for k in plan.descendants(bkey, dependents) if k in body_keys)
                error = await self._advance(run_id, it, by_key, dependents, agent_ids, body_keys, outputs,
                                            run_input, slots)
                if error is not None:
                    return error
            if it["remaining"]:
                return f"loop item {it['idx']} could not finish: {it['remaining']}"

        results: list[Any] = []
        for it in iters:
            outputs.update(it["outputs"])
            vals = it["outputs"]
            results.append(next(iter(vals.values())) if len(vals) == 1 else vals)
        outputs[loop_key]["results"] = results
        if not items:
            for bkey in body_keys:
                await workflow.execute_activity(
                    activities.skip_node,
                    activities.SkipNodeInput(run_id=run_id, agent_id=agent_ids[bkey], key=bkey),
                    start_to_close_timeout=_IO_TIMEOUT,
                    retry_policy=_NODE_RETRY,
                )
                outputs[bkey] = {"skipped": True}
        return None

    async def _advance(self, run_id: str, it: dict[str, Any], by_key: dict[str, dict[str, Any]],
                       dependents: dict[str, list[str]], agent_ids: dict[str, str], body_keys: list[str],
                       outputs: dict[str, dict[str, Any]], run_input: dict[str, Any],
                       slots: asyncio.Semaphore | None = None) -> str | None:
        """Run one loop item's body nodes until the rest wait on its pending
        approvals. Returns the error that should fail the run, or None."""
        remaining: list[str] = []
        blocked: set[str] = set()
        for bkey in it["remaining"]:
            bnode = by_key[bkey]
            if bkey in it["skipped"]:
                await self._outcome(run_id, bnode, agent_ids[bkey], "skipped", it["idx"])
                continue
            bdeps = list(bnode.get("dependsOn") or [])
            if any(d in it["pending"] or d in blocked for d in bdeps):
                blocked.add(bkey)
                remaining.append(bkey)
                continue
            bupstream = {dep: (it["outputs"][dep] if dep in it["outputs"] else outputs.get(dep, {})) for dep in bdeps}
            if bnode.get("type") == _APPROVAL_TYPE:
                # Inside a loop each item's approval is its own row (iteration = item index).
                opened = await self._open_approval(run_id, bnode, agent_ids[bkey], bupstream, run_input,
                                                   it["extra"], it["idx"])
                if opened.failed:
                    return f"{_APPROVAL_TYPE}: {opened.error}"
                it["pending"][bkey] = self._pending.pop(bkey)
                it["outputs"][bkey] = {"approvalId": opened.approval_id, "pending": True}
                continue
            if slots is None:
                bresult = await self._execute(run_id, bnode, agent_ids[bkey], bupstream, run_input,
                                              extra=it["extra"], shortcuts=False)
            else:
                async with slots:
                    bresult = await self._execute(run_id, bnode, agent_ids[bkey], bupstream, run_input,
                                                  extra=it["extra"], shortcuts=False)
            await self._outcome(run_id, bnode, agent_ids[bkey],
                                "failed" if bresult.failed else _status_of(bresult), it["idx"])
            if bresult.failed:
                return bresult.error
            it["outputs"][bkey] = bresult.output or {}
            if not bresult.gate_open:
                it["skipped"].update(k for k in plan.descendants(bkey, dependents) if k in body_keys)
        it["remaining"] = remaining
        return None

    async def _wait(self, run_id: str, node: dict[str, Any], agent_id: str, upstream: dict[str, Any],
                    run_input: dict[str, Any], extra: dict[str, Any] | None) -> activities.RunNodeResult:
        """logic.wait: sleep on a durable Temporal timer (no worker is held)."""
        prep = await workflow.execute_activity(
            waits.prepare_wait,
            waits.PrepareWaitInput(run_id=run_id, node=node, agent_id=agent_id, upstream=upstream,
                                   run_input=run_input, extra=extra or {}),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )
        if prep.failed:
            return activities.RunNodeResult(failed=True, error=f"{_WAIT_TYPE}: {prep.error}")
        base = workflow.info().start_time if prep.from_run_start else workflow.now()
        target = base + timedelta(hours=prep.hours)
        remaining = (target - workflow.now()).total_seconds()
        if remaining > 0:
            await workflow.execute_activity(
                approval.set_waiting,
                approval.SetWaitingInput(run_id=run_id, waiting=True,
                                         step=f"Waiting until {target.isoformat(timespec='minutes')}"),
                start_to_close_timeout=_IO_TIMEOUT, retry_policy=_NODE_RETRY,
            )
            await workflow.sleep(timedelta(seconds=remaining))
            await workflow.execute_activity(
                approval.set_waiting,
                approval.SetWaitingInput(run_id=run_id, waiting=False, step=f"Resumed after waiting ({node['key']})"),
                start_to_close_timeout=_IO_TIMEOUT, retry_policy=_NODE_RETRY,
            )
        return await workflow.execute_activity(
            waits.finish_wait,
            waits.FinishWaitInput(run_id=run_id, agent_id=agent_id, key=node["key"], hours=prep.hours,
                                  resumed_at=workflow.now().isoformat(timespec="seconds"),
                                  from_run_start=prep.from_run_start),
            start_to_close_timeout=_IO_TIMEOUT,
            retry_policy=_NODE_RETRY,
        )

    async def _execute(
        self,
        run_id: str,
        node: dict[str, Any],
        agent_id: str,
        upstream: dict[str, dict[str, Any]],
        run_input: dict[str, Any],
        extra: dict[str, Any] | None,
        shortcuts: bool,
    ):
        """Run one node under its builder settings (n8n-style node settings).

        Shortcuts first (top-level nodes only, never loop-body iterations):
        pinned data in a test run, or a step run's reused upstream output. Then
        up to `retries` extra attempts with a durable `workflow.sleep` between
        them, each bounded by `timeoutSec`. If every attempt fails and
        `continueOnFail` is set, the node outputs {"error": …} and the run goes
        on; otherwise the failure stops the run as before.
        """
        key = node["key"]
        if shortcuts:
            source, known = None, None
            if self._use_pins and isinstance(node.get("pinned"), dict):
                source, known = "pinned", node["pinned"]
            elif self._target and key != self._target and isinstance(self._seed.get(key), dict):
                source, known = "reused", self._seed[key]
            if source:
                return await workflow.execute_activity(
                    activities.use_output,
                    activities.UseOutputInput(run_id=run_id, agent_id=agent_id, key=key,
                                              output=known, source=source),
                    start_to_close_timeout=_IO_TIMEOUT,
                    retry_policy=_NODE_RETRY,
                )

        retries, wait_s, timeout_s, continue_on_fail = plan.node_policy(node)
        if key in self._publishing:
            retries = 0  # never post twice (_PUBLISH_SAFETY_PATCH)
        attempt = 0
        while True:
            attempt += 1
            result = await self._run_one(run_id, node, agent_id, upstream, run_input, extra,
                                         timeout_s=timeout_s, attempt=attempt)
            if not result.failed or attempt > retries:
                break
            await workflow.sleep(timedelta(seconds=wait_s))

        if result.failed and continue_on_fail:
            await workflow.execute_activity(
                activities.fail_node,
                activities.FailNodeInput(run_id=run_id, agent_id=agent_id, key=key,
                                         error=result.error, attempts=attempt, continued=True),
                start_to_close_timeout=_IO_TIMEOUT,
                retry_policy=_NODE_RETRY,
            )
            return activities.RunNodeResult(output={"error": result.error}, gate_open=True)
        return result

    async def _run_one(
        self,
        run_id: str,
        node: dict[str, Any],
        agent_id: str,
        upstream: dict[str, dict[str, Any]],
        run_input: dict[str, Any],
        extra: dict[str, Any] | None,
        timeout_s: float | None = None,
        attempt: int = 1,
    ):
        """Execute one node as its durable activity unit.

        Two backends, one orchestrator: `pieces.*` types run on the Node pieces
        worker via the cross-queue `execute_piece` activity (bracketed by the
        prepare/record activities on our own queue); everything else runs the
        local `run_node` activity. Both return the same RunNodeResult shape, so
        the loop/gate/skip logic above is backend-agnostic.
        """
        if node.get("type") == _WAIT_TYPE:
            return await self._wait(run_id, node, agent_id, upstream, run_input, extra)
        if str(node.get("type", "")).startswith(_PIECE_PREFIX):
            return await self._run_piece_node(run_id, node, agent_id, upstream, run_input, extra,
                                              timeout_s=timeout_s, attempt=attempt)
        try:
            return await workflow.execute_activity(
                activities.run_node,
                activities.RunNodeInput(
                    run_id=run_id, node=node, agent_id=agent_id,
                    upstream=upstream, run_input=run_input, extra=extra or {}, attempt=attempt,
                ),
                # A user timeout is a hard bound: one Temporal attempt, and the
                # node's own retries (_execute) decide whether to go again.
                start_to_close_timeout=timedelta(seconds=timeout_s) if timeout_s else _NODE_TIMEOUT,
                retry_policy=(RetryPolicy(maximum_attempts=1)
                              if timeout_s or node["key"] in self._publishing else _NODE_RETRY),
            )
        except ActivityError as exc:
            # Timed out, or the worker kept dying: the activity never recorded
            # the failure itself, so do it here and fail the node cleanly.
            error = (f"timed out after {timeout_s:g}s" if timeout_s and _is_timeout(exc)
                     else f"node activity failed: {_activity_reason(exc)}")
            if node["key"] in self._publishing:
                error += _MAY_HAVE_PUBLISHED
            await workflow.execute_activity(
                activities.fail_node,
                activities.FailNodeInput(run_id=run_id, agent_id=agent_id, key=node["key"],
                                         error=error, attempts=attempt),
                start_to_close_timeout=_IO_TIMEOUT,
                retry_policy=_NODE_RETRY,
            )
            return activities.RunNodeResult(failed=True, error=f"{node.get('type')}: {error}")

    async def _run_piece_node(
        self,
        run_id: str,
        node: dict[str, Any],
        agent_id: str,
        upstream: dict[str, dict[str, Any]],
        run_input: dict[str, Any],
        extra: dict[str, Any] | None,
        timeout_s: float | None = None,
        attempt: int = 1,
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
                failed=True, error=f"{node.get('type')}: {_activity_reason(exc)}"
            )

        if prep.mode == "execute":
            try:
                result = await workflow.execute_activity(
                    "execute_piece",           # by name: implemented by the Node worker
                    prep.payload,
                    task_queue=self._pieces_queue,
                    schedule_to_start_timeout=_PIECE_SCHEDULE_TO_START,
                    start_to_close_timeout=timedelta(seconds=timeout_s) if timeout_s else _PIECE_TIMEOUT,
                    retry_policy=(RetryPolicy(maximum_attempts=1)
                                  if timeout_s or node["key"] in self._publishing else _PIECE_RETRY),
                )
                mode = "execute"
            except ActivityError as exc:
                timed_out = bool(timeout_s) and _is_timeout(exc)
                result = {
                    "ok": False,
                    "errorType": "Timeout" if timed_out else "PiecesWorkerUnavailable",
                    "error": (f"timed out after {timeout_s:g}s" if timed_out
                              else f"execute_piece failed: {_activity_reason(exc)}"),
                }
                # Never reached before scheduling (no worker polling) means it
                # can't have run; otherwise it may have posted before failing.
                if node["key"] in self._publishing and not _never_started(exc):
                    result["error"] += _MAY_HAVE_PUBLISHED
                mode = "execute"
        else:
            result, mode = prep.result, "trigger"

        return await workflow.execute_activity(
            activities.record_piece_result,
            activities.RecordPieceInput(
                run_id=run_id, node=node, agent_id=agent_id, result=result, mode=mode,
                attempt=attempt,
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


def _status_of(result: Any) -> str:
    """succeeded, or failed for a continue-on-fail node (its output is {"error": …})."""
    out = getattr(result, "output", None) or {}
    return "failed" if set(out) == {"error"} else "succeeded"


def _is_timeout(exc: ActivityError) -> bool:
    return isinstance(exc.cause, TemporalTimeoutError)


def _never_started(exc: ActivityError) -> bool:
    """No worker picked the activity up (schedule-to-start timeout)."""
    cause = exc.cause
    return isinstance(cause, TemporalTimeoutError) and cause.type == TimeoutType.SCHEDULE_TO_START


_MAY_HAVE_PUBLISHED = ("; it may have published before failing, so check the account before running "
                       "it again (publishing steps are never retried automatically)")
