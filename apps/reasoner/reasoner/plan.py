"""Pure, deterministic graph logic — no IO, safe inside a Temporal workflow.

This module holds everything the per-node orchestrator (`reasoner.workflow.
DynamicWorkflow`) needs to drive ordering and skip-propagation WITHOUT touching the
DB, network, clock, or randomness. It deliberately imports nothing from `db`,
`nodes`, `httpx`, `psycopg`, or `obs`, so the workflow sandbox can import it and
recompute the identical plan on replay.

`reasoner.engine` re-exports these names so the single-activity fallback and the
per-node orchestrator share ONE implementation of the ordering/skip semantics.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any


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

# run_agents has no "skipped" status in the schema (see ValidAgentStatuses in
# apps/api/internal/domain/enums.go: idle/running/succeeded/failed/blocked/waiting).
# We use "blocked" to render a control-flow skip distinctly from a success/failure.
_SKIPPED_STATUS = "blocked"

# Node types that decide whether their downstream subgraph runs. Their handler
# returns output["passed"] = bool; when false the engine prunes their descendants.
_GATE_TYPES = {"logic.branch", "logic.filter"}


def _role_for(node: dict[str, Any]) -> str:
    role = node.get("role")
    if isinstance(role, str) and role:
        return role
    kind = str(node.get("type", "")).split(".", 1)[0]
    return _KIND_ROLE.get(kind, "orchestrator")


def _title(key: str, node: dict[str, Any]) -> str:
    """A human label for a node when the builder didn't store one."""
    t = str(node.get("type", key))
    return t.split(".", 1)[-1].replace("_", " ").title() or key


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


def descendants(start: str, dependents: dict[str, list[str]]) -> set[str]:
    """Transitive closure of nodes reachable from `start` via dependsOn edges.

    `start` itself is excluded — a gate node still runs; only what it guards is
    pruned. Pure (no IO); safe to call from workflow context.
    """
    seen: set[str] = set()
    stack = list(dependents.get(start, []))
    while stack:
        node = stack.pop()
        if node in seen:
            continue
        seen.add(node)
        stack.extend(dependents.get(node, []))
    return seen


@dataclass(frozen=True)
class GraphPlan:
    """A fully-resolved, deterministic view of a composed DAG.

    Built by `build_plan` from the (already-fetched) GraphNode list. Contains no
    IO — it is safe to construct inside Temporal *workflow* code, which is exactly
    what the per-node orchestrator does: the workflow holds the plan and drives the
    order, delegating every side effect to activities. Ordering is deterministic so
    a replay reconstructs the identical plan.
    """
    ordered: list[dict[str, Any]]          # nodes in topological order
    dependents: dict[str, list[str]]       # dep key → dependent keys
    grid: dict[str, tuple[int, int]]       # node key → (col, row) for UI layout

    @property
    def total(self) -> int:
        return len(self.ordered)


def build_plan(graph: list[dict[str, Any]]) -> GraphPlan:
    """Validate + order the graph into a deterministic, IO-free `GraphPlan`."""
    ordered = topo_order(graph)
    grid = _grid(ordered)
    dependents: dict[str, list[str]] = {n["key"]: [] for n in ordered}
    for n in ordered:
        for dep in n.get("dependsOn") or []:
            dependents[dep].append(n["key"])
    return GraphPlan(ordered=ordered, dependents=dependents, grid=grid)


def dependents_of(graph: list[dict[str, Any]]) -> dict[str, list[str]]:
    """dep key → dependent keys, over an already-ordered node list."""
    dependents: dict[str, list[str]] = {n["key"]: [] for n in graph}
    for n in graph:
        for dep in n.get("dependsOn") or []:
            if dep in dependents:
                dependents[dep].append(n["key"])
    return dependents


def loop_body(loop_key: str, ordered: list[dict[str, Any]], dependents: dict[str, list[str]]) -> list[str]:
    """Keys of the nodes a logic.loop fans out per item — its DOMINATED subgraph.

    A node belongs to the loop body iff every one of its dependency paths enters
    through the loop node. Walking in topological order makes that a one-pass
    check: a node joins the body when it has at least one dependency and ALL of
    its dependencies are the loop node or earlier body members. A join node that
    also depends on something outside the loop is NOT in the body — it runs once,
    after the loop, seeing the loop's collected `results`.

    Pure (no IO); safe inside Temporal workflow code, which is exactly where the
    per-node orchestrator computes it. `dependents` is accepted for symmetry with
    the other helpers but the computation only needs dependsOn edges.
    """
    _ = dependents  # kept for call-site symmetry
    body: set[str] = set()
    for n in ordered:
        key = n["key"]
        if key == loop_key:
            continue
        deps = list(n.get("dependsOn") or [])
        if deps and all(d == loop_key or d in body for d in deps):
            body.add(key)
    return [n["key"] for n in ordered if n["key"] in body]


def should_skip(node: dict[str, Any], skipped: set[str]) -> bool:
    """Pure: has this node been pruned (directly, or all deps skipped)?

    Topological order guarantees every dependency has already been visited, so a
    node whose dependencies were ALL skipped is itself skipped. A node with no
    dependencies is never skipped this way.
    """
    key = node["key"]
    deps = list(node.get("dependsOn") or [])
    return key in skipped or bool(deps and all(d in skipped for d in deps))


def is_gate_open(node: dict[str, Any], output: dict[str, Any]) -> bool:
    """Pure: for a gate node, did its condition pass (so descendants run)?

    Non-gate nodes are always 'open'. A gate is open unless its handler reported
    output["passed"] == False.
    """
    if str(node.get("type", "")) not in _GATE_TYPES:
        return True
    return bool(output.get("passed", True))


def agent_name(node: dict[str, Any], key: str) -> str:
    return node.get("config", {}).get("label") or _title(key, node)


def agent_model(node: dict[str, Any]) -> str:
    return node.get("config", {}).get("model") or node.get("model") or "claude-sonnet-4-6"
