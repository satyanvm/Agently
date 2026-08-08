"""Dynamic execution engine: run a user-composed GraphNode DAG.

Given the list of GraphNodes saved by the visual builder (key / role / type /
config / dependsOn), this:

  1. validates + topologically orders the nodes by their `dependsOn` edges,
  2. creates the run_agent rows so the existing UI renders the graph,
  3. executes each node via its registered handler (reasoner.nodes), passing the
     resolved outputs of upstream nodes downstream,
  4. writes run progress / logs / usage / artifacts into the shared Postgres,
     exactly like the static graph does.

Two orchestrators share the pieces in this module:

  • `execute_graph` — the single-activity fallback (graph.py `dynamic_node`): runs
    the whole DAG in one Temporal activity. Durable/replayable as one unit.
  • `reasoner.workflow.DynamicWorkflow` — the per-node orchestrator: it drives the
    deterministic topo loop in *workflow* code and invokes `execute_node` (below)
    as ONE Temporal activity per node, so each user node is independently
    checkpointed in Temporal's event history. A crash resumes at the in-flight
    node — already-succeeded nodes are served from history and never re-run.

To avoid duplicating logic, both paths call the same helpers:
  - `topo_order` — Kahn sort + cycle/missing-dep detection.
  - `build_plan` — pure, deterministic: ordered node list, dependents adjacency,
    and (col,row) grid. Safe to compute in workflow context (no IO).
  - `should_skip` / `descendants` / `is_gate_open` — pure skip-propagation logic
    shared by the loop in both orchestrators.
  - `seed_agents` / `execute_one_node` / `mark_skipped` — the IO steps (DB writes,
    handler dispatch), run inside an activity in both paths.

LLM, browser, delivery, credential, and tool failures propagate to the run; this
module never manufactures mock output or record-intent success.
"""
from __future__ import annotations

from typing import Any

from . import db, nodes, obs
from .nodes import NodeContext
# Pure, IO-free graph logic lives in reasoner.plan so the Temporal workflow sandbox
# can import it without pulling in db/nodes/httpx. Re-exported here so existing
# callers (and tests) keep using engine.topo_order / engine.build_plan / etc.
from .plan import (  # noqa: F401 — re-exported for the engine's public surface
    _GATE_TYPES,
    _KIND_ROLE,
    _SKIPPED_STATUS,
    _grid,
    _role_for,
    _title,
    GraphError,
    GraphPlan,
    agent_model,
    agent_name,
    build_plan,
    dependents_of,
    descendants,
    is_gate_open,
    loop_body,
    should_skip,
    topo_order,
)


# ─────────────────────── the IO steps (activity bodies) ───────────────────────

async def seed_agents(run_id: str, plan: GraphPlan) -> dict[str, str]:
    """Create every run_agent up front (idempotent) so the whole graph renders.

    Returns key → stable agent id. Safe to call again on resume: `upsert_agent`
    is `on conflict do nothing`, so it never clobbers a node that already ran.
    """
    await db.set_run_running(run_id, "Starting")
    # Tracing is mandatory now, so the handle is always real — no guard needed.
    await db.set_langfuse_trace(run_id, obs.session_handle(run_id))
    agent_ids: dict[str, str] = {}
    for n in plan.ordered:
        key = n["key"]
        col, row = plan.grid[key]
        agent_ids[key] = await db.upsert_agent(
            run_id, key, agent_name(n, key), _role_for(n), agent_model(n),
            list(n.get("dependsOn") or []), col, row,
        )
    return agent_ids


async def mark_skipped(run_id: str, agent_id: str, key: str) -> None:
    """Record a control-flow skip on a node's agent row + log."""
    await db.set_agent_status(agent_id, _SKIPPED_STATUS, summary="Skipped (upstream condition)")
    await db.append_log(run_id, "info", "system", key, "Skipped — upstream condition was false")


async def execute_one_node(
    run_id: str,
    node: dict[str, Any],
    agent_id: str,
    upstream: dict[str, dict[str, Any]],
    run_input: dict[str, Any],
    extra: dict[str, Any] | None = None,
) -> nodes.NodeResult:
    """Run ONE node's handler and write its status/usage. Raises on handler error.

    This is the unit the per-node orchestrator wraps in a single Temporal activity,
    and the unit `execute_graph` calls inline. It does NOT decide skips or draw
    hand-off edges — the orchestrator owns ordering/skip so that logic stays pure
    and identical across both paths. `extra` carries loop fan-out template roots
    ({"item": …, "loop": {…}}) for body-node executions.
    """
    key = node["key"]
    await db.set_agent_status(agent_id, "running")
    await db.append_log(run_id, "info", "agent", key, f"Running {node.get('type')}")
    ctx = NodeContext(
        run_id=run_id,
        agent_id=agent_id,
        node=node,
        upstream=upstream,
        run_input=run_input,
        label=agent_name(node, key),
        extra=extra or {},
    )
    handler = nodes.handler_for(str(node.get("type", "")))
    result = await handler(ctx)
    if result.tokens_in or result.tokens_out or result.cost_usd:
        await db.add_usage(run_id, result.tokens_in, result.tokens_out, result.cost_usd)
    await db.set_agent_status(agent_id, "succeeded", summary=result.summary)
    return result


async def _fan_out_loop(
    run_id: str,
    loop_key: str,
    items: list[Any],
    body_keys: list[str],
    plan: GraphPlan,
    agent_ids: dict[str, str],
    outputs: dict[str, dict[str, Any]],
    run_input: dict[str, Any],
) -> list[Any]:
    """Run the loop's body once per item; return the collected per-item results.

    Each iteration executes the body nodes in topological order with the template
    roots {"item": <element>, "loop": {<loop_key>: {"index": i}}} injected. A gate
    inside the body prunes the REST OF THAT ITERATION only. The per-item result is
    the last body node's output (the natural "value" of the iteration), or a dict
    of every body node's output when the body has parallel leaves.

    Side effect: outputs[<body_key>] is left holding the LAST iteration's output so
    a downstream join can still reference a body node directly; the collected list
    lives at outputs[<loop_key>]["results"].
    """
    by_key = {n["key"]: n for n in plan.ordered}
    body_nodes = [by_key[k] for k in body_keys]
    results: list[Any] = []
    for idx, item in enumerate(items):
        extra = {"item": item, "loop": {loop_key: {"index": idx}}}
        iter_outputs: dict[str, dict[str, Any]] = {}
        iter_skipped: set[str] = set()
        for bn in body_nodes:
            bkey = bn["key"]
            if bkey in iter_skipped:
                continue
            deps = list(bn.get("dependsOn") or [])
            upstream = {
                dep: (iter_outputs.get(dep) if dep in iter_outputs else outputs.get(dep, {}))
                for dep in deps
            }
            result = await execute_one_node(
                run_id, bn, agent_ids[bkey], upstream, run_input, extra=extra,
            )
            iter_outputs[bkey] = result.output or {}
            if not is_gate_open(bn, iter_outputs[bkey]):
                for pruned in descendants(bkey, plan.dependents):
                    if pruned in body_keys:
                        iter_skipped.add(pruned)
        outputs.update(iter_outputs)
        if len(iter_outputs) == 1:
            results.append(next(iter(iter_outputs.values())))
        else:
            results.append(iter_outputs)
    if items:
        await db.append_log(
            run_id, "info", "system", loop_key,
            f"Loop fanned out {len(body_keys)} body node(s) × {len(items)} item(s)",
        )
    return results


async def execute_graph(run_id: str, graph: list[dict[str, Any]], run_input: dict[str, Any]) -> dict[str, Any]:
    """Execute a composed GraphNode DAG in ONE activity (single-activity fallback).

    Returns {"done": bool, "outputs": {...}}. This is the fallback path used when
    per-node orchestration is not driving the run (graph.py `dynamic_node`). It
    shares its ordering, skip-propagation and per-node execution with the per-node
    orchestrator via the helpers above, so behaviour is identical.

    Real control-flow: a `logic.branch`/`logic.filter` whose condition is false
    prunes its entire downstream subgraph. Pruning propagates naturally — a node
    all of whose dependencies were skipped is itself skipped — so a skipped node
    never blocks the run from completing. Skipped nodes are marked with the
    `_SKIPPED_STATUS` agent status and their handlers are never invoked.

    Resume-safe: a node whose run_agent row is already `succeeded` (an activity
    retry re-ran this fallback from the top) is not re-executed; its recorded
    status is trusted and it is treated as done. Skipped/failed rows are re-run.
    """
    plan = build_plan(graph)
    total = plan.total
    agent_ids = await seed_agents(run_id, plan)

    # On an activity retry, trust rows already marked succeeded so we don't redo
    # side-effecting work (LLM calls, artifacts) for nodes that finished.
    prior = await db.fetch_agent_statuses(run_id)

    outputs: dict[str, dict[str, Any]] = {}
    skipped: set[str] = set()
    fanned_out: set[str] = set()  # loop-body nodes executed by the fan-out below
    done = 0
    for n in plan.ordered:
        key = n["key"]
        agent_id = agent_ids[key]
        deps = list(n.get("dependsOn") or [])

        if key in fanned_out:
            done += 1
            await db.set_run_progress(run_id, done, total, f"Ran {key} (loop body)")
            continue

        if should_skip(n, skipped):
            skipped.add(key)
            skipped.update(descendants(key, plan.dependents))
            await mark_skipped(run_id, agent_id, key)
            outputs[key] = {"skipped": True}
            done += 1
            await db.set_run_progress(run_id, done, total, f"Skipped {key}")
            continue

        if prior.get(agent_id) == "succeeded":
            # Already ran in a prior attempt — don't redo its side effects.
            outputs[key] = {}
            await db.append_log(run_id, "info", "system", key, "Resuming — node already completed, skipping re-run")
            done += 1
            await db.set_run_progress(run_id, done, total, f"Resumed {key}")
            continue

        upstream = {dep: outputs.get(dep, {}) for dep in deps}
        try:
            result = await execute_one_node(run_id, n, agent_id, upstream, run_input)
        except Exception as exc:  # noqa: BLE001 — one node's failure fails the run cleanly
            await db.set_agent_status(agent_id, "failed", summary=str(exc)[:280])
            await db.append_log(run_id, "error", "system", key, f"Node failed: {exc}")
            await db.finish_run(run_id, "failed", "Failed", error=str(exc))
            obs.flush()
            return {"done": False, "error": str(exc), "failed_node": key}

        outputs[key] = result.output or {}

        # logic.loop: fan its dominated body out once per resolved item. Body
        # nodes are marked fanned_out so the main walk doesn't re-run them.
        if str(n.get("type", "")) == "logic.loop":
            body_keys = loop_body(key, plan.ordered, plan.dependents)
            fanned_out.update(body_keys)
            items = outputs[key].get("items") or []
            try:
                results = await _fan_out_loop(
                    run_id, key, items, body_keys, plan, agent_ids, outputs, run_input,
                )
            except Exception as exc:  # noqa: BLE001 — a body failure fails the run cleanly
                await db.append_log(run_id, "error", "system", key, f"Loop body failed: {exc}")
                await db.finish_run(run_id, "failed", "Failed", error=str(exc))
                obs.flush()
                return {"done": False, "error": str(exc), "failed_node": key}
            outputs[key]["results"] = results
            if not items:
                for bk in body_keys:
                    await mark_skipped(run_id, agent_ids[bk], bk)
                    outputs[bk] = {"skipped": True}

        # Gate node whose condition was false: prune its whole downstream subgraph.
        if not is_gate_open(n, outputs[key]):
            pruned = descendants(key, plan.dependents)
            skipped.update(pruned)
            if pruned:
                await db.append_log(
                    run_id, "info", "system", key,
                    f"Condition false — skipping {len(pruned)} downstream node(s)",
                )

        # Draw the edges into downstream agents for the live graph view.
        for child_key in plan.dependents[key]:
            await db.add_message(run_id, agent_id, agent_ids[child_key], f"{key} → {child_key}")

        done += 1
        await db.set_run_progress(run_id, done, total, f"Ran {key}")

    await db.finish_run(run_id, "succeeded", "Completed")
    obs.flush()
    return {"done": True, "outputs": outputs, "skipped": sorted(skipped)}
