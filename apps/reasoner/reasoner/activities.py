"""Temporal activities for the per-node dynamic orchestrator.

These are the side-effecting units the `DynamicWorkflow` (reasoner.workflow) drives.
Each is a plain `@activity.defn` — registered on the Worker alongside the LangGraph
plugin's activities. Splitting the composed DAG into one `run_node` activity per
user node is what gives the dynamic path *per-node* Temporal durability: each node
lands its own event-history checkpoint, so a crash resumes at the in-flight node and
already-succeeded nodes are served from history, never re-run.

All DB / LLM / browser IO lives here (never in the workflow), so the workflow stays
deterministic. The engine helpers (`reasoner.engine`) hold the shared, pure logic
so this module and the single-activity fallback (`engine.execute_graph`) behave
identically.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from temporalio import activity

from . import db, engine, obs


# ─────────────────────────── activity payloads ───────────────────────────
# Dataclasses (not bare dicts) so Temporal's converter round-trips them cleanly
# and the workflow<->activity contract is explicit.

@dataclass
class LoadGraphInput:
    run_id: str


@dataclass
class LoadGraphResult:
    """The composed graph + its deterministic plan, resolved OUTSIDE the workflow.

    The workflow must not touch the DB, so this activity fetches the GraphNode list
    once and hands the ordered plan to the workflow as input. `dynamic` is False
    when the run has no composed graph (the workflow then no-ops and the static
    LangGraph path handles it).
    """
    dynamic: bool
    graph: list[dict[str, Any]] = field(default_factory=list)
    ordered_keys: list[str] = field(default_factory=list)
    agent_ids: dict[str, str] = field(default_factory=dict)   # node key → agent id
    prior_status: dict[str, str] = field(default_factory=dict)  # agent id → status


@dataclass
class RunNodeInput:
    run_id: str
    node: dict[str, Any]
    agent_id: str
    upstream: dict[str, dict]
    run_input: dict[str, Any]
    # Loop fan-out template roots ({"item": …, "loop": {…}}); empty otherwise.
    extra: dict[str, Any] = field(default_factory=dict)


@dataclass
class RunNodeResult:
    output: dict[str, Any] = field(default_factory=dict)
    # Whether this (gate) node's condition passed. True for non-gate nodes.
    gate_open: bool = True
    failed: bool = False
    error: str = ""


@dataclass
class SkipNodeInput:
    run_id: str
    agent_id: str
    key: str


@dataclass
class EdgeInput:
    run_id: str
    from_agent_id: str
    to_agent_id: str
    label: str


@dataclass
class ProgressInput:
    run_id: str
    done: int
    total: int
    step: str


@dataclass
class FinishInput:
    run_id: str
    status: str
    step: str
    error: str = ""


# ─────────────────────────── activities ───────────────────────────

@activity.defn
async def load_graph(inp: LoadGraphInput) -> LoadGraphResult:
    """Fetch the composed graph, seed run_agents, and return the ordered plan.

    Runs once at the top of the workflow. Seeding here (idempotent) means the whole
    graph renders immediately and the workflow already knows every agent id via the
    deterministic `_stable_agent_id` scheme — so it never needs a DB read to map
    keys to agent ids. `prior_status` lets the workflow trust rows already
    `succeeded` on resume, matching the fallback's resume behaviour.
    """
    graph = await db.fetch_graph_nodes(inp.run_id)
    if not graph:
        return LoadGraphResult(dynamic=False)
    plan = engine.build_plan(graph)
    agent_ids = await engine.seed_agents(inp.run_id, plan)
    prior = await db.fetch_agent_statuses(inp.run_id)
    return LoadGraphResult(
        dynamic=True,
        graph=[dict(n) for n in plan.ordered],  # already topo-ordered
        ordered_keys=[n["key"] for n in plan.ordered],
        agent_ids=agent_ids,
        prior_status=prior,
    )


@activity.defn
async def run_node(inp: RunNodeInput) -> RunNodeResult:
    """Execute ONE node's handler + write its status/usage. The durable unit.

    Returns the node output and whether a gate is open (never raises for a node
    failure — a failure is returned as `failed=True` so the workflow can finish the
    run cleanly and deterministically rather than relying on activity-failure
    semantics for control flow).
    """
    try:
        result = await engine.execute_one_node(
            inp.run_id, inp.node, inp.agent_id, inp.upstream, inp.run_input, extra=inp.extra
        )
    except Exception as exc:  # noqa: BLE001 — surface as data, not an activity crash
        await db.set_agent_status(inp.agent_id, "failed", summary=str(exc)[:280])
        await db.append_log(inp.run_id, "error", "system", inp.node["key"], f"Node failed: {exc}")
        return RunNodeResult(failed=True, error=str(exc))

    output = result.output or {}
    gate_open = engine.is_gate_open(inp.node, output)
    return RunNodeResult(output=output, gate_open=gate_open)


@activity.defn
async def skip_node(inp: SkipNodeInput) -> None:
    """Mark a node skipped (pruned by an upstream gate / all-deps-skipped)."""
    await engine.mark_skipped(inp.run_id, inp.agent_id, inp.key)


@activity.defn
async def add_edge(inp: EdgeInput) -> None:
    await db.add_message(inp.run_id, inp.from_agent_id, inp.to_agent_id, inp.label)


@activity.defn
async def set_progress(inp: ProgressInput) -> None:
    await db.set_run_progress(inp.run_id, inp.done, inp.total, inp.step)


@activity.defn
async def finish_run(inp: FinishInput) -> None:
    await db.finish_run(inp.run_id, inp.status, inp.step, error=(inp.error or None))
    obs.flush()


# Exported so the worker can register every activity in one place.
ALL_ACTIVITIES = [load_graph, run_node, skip_node, add_edge, set_progress, finish_run]
