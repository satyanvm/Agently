"""Dynamic execution engine: run a user-composed GraphNode DAG.

Given the list of GraphNodes saved by the visual builder (key / role / type /
config / dependsOn), this:

  1. validates + topologically orders the nodes by their `dependsOn` edges,
  2. creates the run_agent rows so the existing UI renders the graph,
  3. executes each node via its registered handler (reasoner.nodes), passing the
     resolved outputs of upstream nodes downstream,
  4. writes run progress / logs / usage / artifacts into the shared Postgres,
     exactly like the static graph does.

It runs inside a single Temporal activity (see graph.py `dynamic_node`). That
keeps the whole composed DAG durable as one replayable unit; per-node retries of
the static graph are traded for simpler, deterministic ordering of an
arbitrarily-shaped user graph. LLM/browser calls still degrade to mocks when keys
are absent, so a dynamic run works end-to-end with no external services.
"""
from __future__ import annotations

from typing import Any

from . import db, nodes, obs
from .nodes import NodeContext


class GraphError(ValueError):
    """Raised when the composed graph is structurally invalid (cycle, bad dep)."""


# Map a builder node kind (prefix of the type) to the run_agent role the UI uses
# to colour/lay out the node. Mirrors KIND_ROLE in apps/web/lib/builder-graph.ts.
_KIND_ROLE = {
    "trigger": "orchestrator",
    "agent": "orchestrator",
    "tool": "browser",
    "logic": "analyst",
    "output": "writer",
}


def _role_for(node: dict[str, Any]) -> str:
    role = node.get("role")
    if isinstance(role, str) and role:
        return role
    kind = str(node.get("type", "")).split(".", 1)[0]
    return _KIND_ROLE.get(kind, "orchestrator")


def topo_order(graph: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Return nodes in dependency order (Kahn's algorithm).

    Raises GraphError on a missing dependency or a cycle. Ties are broken by the
    nodes' original order so layout stays stable across runs.
    """
    by_key: dict[str, dict[str, Any]] = {}
    for n in graph:
        key = n.get("key")
        if not key:
            raise GraphError("node without a key")
        if key in by_key:
            raise GraphError(f"duplicate node key: {key}")
        by_key[key] = n

    # indegree + adjacency over dependsOn (dep → node).
    indeg: dict[str, int] = {k: 0 for k in by_key}
    dependents: dict[str, list[str]] = {k: [] for k in by_key}
    for key, n in by_key.items():
        for dep in n.get("dependsOn") or []:
            if dep not in by_key:
                raise GraphError(f"node '{key}' depends on unknown node '{dep}'")
            indeg[key] += 1
            dependents[dep].append(key)

    order_index = {n["key"]: i for i, n in enumerate(graph)}
    ready = sorted((k for k, d in indeg.items() if d == 0), key=lambda k: order_index[k])
    ordered: list[dict[str, Any]] = []
    while ready:
        key = ready.pop(0)
        ordered.append(by_key[key])
        for child in dependents[key]:
            indeg[child] -= 1
            if indeg[child] == 0:
                # insert keeping original-order tie-break
                ready.append(child)
                ready.sort(key=lambda k: order_index[k])

    if len(ordered) != len(by_key):
        stuck = [k for k, d in indeg.items() if d > 0]
        raise GraphError(f"cycle detected among nodes: {', '.join(sorted(stuck))}")
    return ordered


def _grid(graph: list[dict[str, Any]]) -> dict[str, tuple[int, int]]:
    """Assign (col, row) per node for UI layout: col = longest-path depth."""
    by_key = {n["key"]: n for n in graph}
    depth: dict[str, int] = {}

    def col_of(key: str, seen: frozenset[str]) -> int:
        if key in depth:
            return depth[key]
        deps = [d for d in (by_key[key].get("dependsOn") or []) if d in by_key and d not in seen]
        d = 0 if not deps else 1 + max(col_of(dp, seen | {key}) for dp in deps)
        depth[key] = d
        return d

    cols: dict[str, tuple[int, int]] = {}
    row_per_col: dict[int, int] = {}
    for n in graph:
        c = col_of(n["key"], frozenset())
        r = row_per_col.get(c, 0)
        row_per_col[c] = r + 1
        cols[n["key"]] = (c, r)
    return cols


async def execute_graph(run_id: str, graph: list[dict[str, Any]], run_input: dict[str, Any]) -> dict[str, Any]:
    """Execute a composed GraphNode DAG. Returns {"done": bool, "outputs": {...}}."""
    ordered = topo_order(graph)
    total = len(ordered)
    grid = _grid(ordered)

    await db.set_run_running(run_id, "Starting")
    if obs.enabled():
        await db.set_langfuse_trace(run_id, obs.session_handle(run_id))

    # Create every run_agent up front so the whole graph renders immediately.
    agent_ids: dict[str, str] = {}
    for n in ordered:
        key = n["key"]
        col, row = grid[key]
        name = n.get("config", {}).get("label") or _title(key, n)
        model = n.get("config", {}).get("model") or n.get("model") or "claude-sonnet-4-6"
        agent_ids[key] = await db.upsert_agent(
            run_id, key, name, _role_for(n), model, list(n.get("dependsOn") or []), col, row
        )

    outputs: dict[str, dict[str, Any]] = {}
    done = 0
    for n in ordered:
        key = n["key"]
        agent_id = agent_ids[key]
        await db.set_agent_status(agent_id, "running")
        await db.append_log(run_id, "info", "agent", key, f"Running {n.get('type')}")

        upstream = {dep: outputs.get(dep, {}) for dep in (n.get("dependsOn") or [])}
        ctx = NodeContext(
            run_id=run_id,
            agent_id=agent_id,
            node=n,
            upstream=upstream,
            run_input=run_input,
            label=n.get("config", {}).get("label") or _title(key, n),
        )
        handler = nodes.handler_for(str(n.get("type", "")))
        try:
            result = await handler(ctx)
        except Exception as exc:  # noqa: BLE001 — one node's failure fails the run cleanly
            await db.set_agent_status(agent_id, "failed", summary=str(exc)[:280])
            await db.append_log(run_id, "error", "system", key, f"Node failed: {exc}")
            await db.finish_run(run_id, "failed", "Failed", error=str(exc))
            obs.flush()
            return {"done": False, "error": str(exc), "failed_node": key}

        outputs[key] = result.output or {}
        if result.tokens_in or result.tokens_out or result.cost_usd:
            await db.add_usage(run_id, result.tokens_in, result.tokens_out, result.cost_usd)
        await db.set_agent_status(agent_id, "succeeded", summary=result.summary)
        # Draw the edges into downstream agents for the live graph view.
        for child in ordered:
            if key in (child.get("dependsOn") or []):
                await db.add_message(run_id, agent_id, agent_ids[child["key"]], f"{key} → {child['key']}")

        done += 1
        await db.set_run_progress(run_id, done, total, f"Ran {key}")

    await db.finish_run(run_id, "succeeded", "Completed")
    obs.flush()
    return {"done": True, "outputs": outputs}


def _title(key: str, node: dict[str, Any]) -> str:
    """A human label for a node when the builder didn't store one."""
    t = str(node.get("type", key))
    return t.split(".", 1)[-1].replace("_", " ").title() or key
