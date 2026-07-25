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

import json
import os
from dataclasses import dataclass, field
from typing import Any

from temporalio import activity

from . import db, engine, obs, pieces
from .nodes import NodeContext, render_tpl, split_credential


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


@dataclass
class PreparePieceInput:
    """Same shape as RunNodeInput — a piece node is just another node; only the
    execution backend (the Node pieces worker) differs."""
    run_id: str
    node: dict[str, Any]
    agent_id: str
    upstream: dict[str, dict]
    run_input: dict[str, Any]
    extra: dict[str, Any] = field(default_factory=dict)


@dataclass
class PreparePieceResult:
    """mode='execute' → payload is the execute_piece argument (contract §4).
    mode='record' → result is the record-intent output; no cross-queue call."""
    mode: str
    payload: dict[str, Any] = field(default_factory=dict)
    result: dict[str, Any] = field(default_factory=dict)


@dataclass
class RecordPieceInput:
    run_id: str
    node: dict[str, Any]
    agent_id: str
    result: dict[str, Any]
    mode: str  # "execute" (result is the execute_piece return) | "record"


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


# ─────────────────────── Activepieces piece nodes ───────────────────────
# A `pieces.<slug>.<action>` node executes on the Node pieces worker (its own
# task queue) via the `execute_piece` activity — docs/pieces-runtime-contract.md.
# The Python side brackets that call with two activities: `prepare_piece_node`
# (render props, check credentials, mark the agent running) and
# `record_piece_result` (persist the outcome exactly like run_node does).

def _clip_summary(s: str, n: int = 280) -> str:
    s = (s or "").strip().replace("\n", " ")
    return s if len(s) <= n else s[: n - 1] + "…"


@activity.defn
async def prepare_piece_node(inp: PreparePieceInput) -> PreparePieceResult:
    """Resolve a piece node into an execute_piece payload — or a record-intent.

    Mirrors the head of engine.execute_one_node (status → running, log line) so
    the UI shows the node live while the Node worker runs it. Prop values are
    rendered with the same template engine `_integration` uses; the credential
    env var is checked for PRESENCE only — the secret itself stays out of
    Temporal payloads (the Node worker resolves it by name, contract §4-5).
    """
    node = inp.node
    key = node["key"]
    node_type = str(node.get("type", ""))
    await db.set_agent_status(inp.agent_id, "running")
    await db.append_log(inp.run_id, "info", "agent", key, f"Running {node_type}")

    spec = pieces.piece_spec_for(node_type)
    if spec is None:
        await db.append_log(
            inp.run_id, "warn", "system", key,
            f"{node_type} recorded — not in the pieces index (regenerate with apps/pieces-worker gen:index)",
        )
        return PreparePieceResult(
            mode="record",
            result={"recorded": True, "executed": False, "reason": "unknown-piece"},
        )

    # Piece TRIGGER nodes: the event was already produced BEFORE this run
    # existed (webhook ingress / polling tick called the trigger's run() on the
    # pieces worker — apps/api hooks.go + piecespoller.go) and rides in as
    # run.input.__trigger_event. The node simply surfaces it as its output so
    # downstream {{outputs.<key>.…}} templating works; no cross-queue call.
    if str(spec.get("kind", "action")) == "trigger":
        event = (inp.run_input or {}).get("__trigger_event")
        if event is None:
            result: dict[str, Any] = {"recorded": True, "executed": False, "reason": "no-trigger-event"}
            await db.append_log(
                inp.run_id, "info", "agent", key,
                f"{node_type} recorded — run was not started by its trigger (no event payload)",
            )
        else:
            result = dict(event) if isinstance(event, dict) else {"event": event}
            result["executed"] = True
            await db.append_log(inp.run_id, "info", "agent", key, f"{node_type} trigger event received")
        return PreparePieceResult(mode="record", result=result)

    # The node may name a DB-backed credential via the reserved __credentialId
    # config key (docs/credentials-contract.md §§6-7). It is stripped from the
    # config BEFORE prop/template rendering and travels to the Node worker as
    # `credentialId` — the id only, never the secret values.
    cfg, cred_id = split_credential(node.get("config", {}) or {})

    cred_key = pieces.credential_key_for(spec)
    # Presence gate: a selected DB credential counts as present (the Node worker
    # resolves it by id); otherwise the env var must exist, as before.
    if pieces.auth_required(spec) and not cred_id and not os.getenv(cred_key or "", ""):
        await db.append_log(
            inp.run_id, "warn", "system", key,
            f"{node_type} recorded — missing credential env var: {cred_key}",
        )
        return PreparePieceResult(
            mode="record",
            result={"recorded": True, "executed": False, "missingCredentials": [cred_key]},
        )

    # Render each configured prop with the shared template engine (same roots the
    # catalog executor uses: input/outputs/config/credentials + loop extras).
    ctx = NodeContext(
        run_id=inp.run_id, agent_id=inp.agent_id, node=node,
        upstream=inp.upstream, run_input=inp.run_input, extra=inp.extra,
    )
    roots = {"config": cfg, "credentials": {}}
    known_props = {p.get("key") for p in spec.get("props") or []}
    props: dict[str, Any] = {}
    for prop_key, raw in cfg.items():
        if known_props and prop_key not in known_props:
            continue  # config keys the action doesn't declare (e.g. "model") stay out
        if isinstance(raw, str):
            rendered = render_tpl(raw, ctx, roots)
            # When templating actually resolved something ({{outputs.x.rows}} →
            # a JSON list/number), recover the structure so the piece receives
            # typed props. A literal the user typed ("123", "#general") is kept
            # verbatim — only template-produced values are decoded.
            if rendered != raw:
                try:
                    props[prop_key] = json.loads(rendered)
                except (ValueError, TypeError):
                    props[prop_key] = rendered
            else:
                props[prop_key] = rendered
        else:
            props[prop_key] = raw  # already-typed values (numbers, lists, objects)

    return PreparePieceResult(
        mode="execute",
        payload={
            "piece": spec["piece"],
            "pieceVersion": spec.get("pieceVersion", ""),
            "action": spec["action"],
            "props": props,
            "authEnvKey": cred_key,
            # The Node worker resolves this id against Postgres (contract §4);
            # falls back to process.env[authEnvKey] when null/dangling.
            "credentialId": cred_id or None,
        },
    )


@activity.defn
async def record_piece_result(inp: RecordPieceInput) -> RunNodeResult:
    """Persist a piece node's outcome the way run_node persists a handler's.

    Never fails the run: piece-level errors ({ok:false}) and record-intents both
    land as node output with a succeeded agent row — matching how record-intent
    catalog nodes behave — so one unavailable integration can't kill the graph.
    """
    key = inp.node["key"]
    res = inp.result or {}

    if inp.mode == "record":
        output = dict(res)
        if res.get("missingCredentials"):
            summary = "Recorded (needs " + ", ".join(res.get("missingCredentials", [])) + ")"
        elif res.get("executed"):
            summary = "Trigger event received"
        else:
            summary = f"Recorded ({res.get('reason', 'intent')})"
    elif res.get("ok"):
        raw = res.get("output")
        output = raw if isinstance(raw, dict) else {"value": raw}
        output.setdefault("executed", True)
        summary = _clip_summary(json.dumps(raw)) if raw is not None else "Ran"
    else:
        output = {
            "error": res.get("error", "piece execution failed"),
            "errorType": res.get("errorType", "PieceExecutionError"),
            "executed": True,
        }
        summary = f"Failed: {_clip_summary(str(res.get('error', '')), 120)}"
        await db.append_log(
            inp.run_id, "error", "tool", key,
            f"{inp.node.get('type')} failed: {res.get('error', '')}",
        )

    await db.set_agent_status(inp.agent_id, "succeeded", summary=summary)
    return RunNodeResult(output=output, gate_open=True)


# Exported so the worker can register every activity in one place.
ALL_ACTIVITIES = [
    load_graph, run_node, skip_node, add_edge, set_progress, finish_run,
    prepare_piece_node, record_piece_result,
]
