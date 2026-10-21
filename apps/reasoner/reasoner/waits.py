"""logic.wait: a durable pause (a Temporal timer) inside a run.

Used to schedule approved posts: "publish this 24 hours after the run
started", one slot per post. The timer itself is workflow code
(`workflow.sleep`, see workflow.py _wait) — it survives worker restarts and
holds no worker while it runs. These activities only do the side effects:

  prepare_wait   render config.hours (templates allowed, e.g. {{item.delayHours}})
                 and mark the node waiting
  finish_wait    mark it succeeded with when it resumed

config.from = "now" (default) waits `hours` from when the step is reached;
"run_start" waits until `hours` after the run started, so several items of a
loop get absolute slots instead of piling their waits up one after another.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from temporalio import activity

from . import db, nodes
from .activities import RunNodeResult
from .nodes import NodeContext

MAX_WAIT_HOURS = 24.0 * 30


@dataclass
class PrepareWaitInput:
    run_id: str
    node: dict[str, Any]
    agent_id: str
    upstream: dict[str, Any] = field(default_factory=dict)
    run_input: dict[str, Any] = field(default_factory=dict)
    extra: dict[str, Any] = field(default_factory=dict)


@dataclass
class PrepareWaitResult:
    hours: float = 0.0
    from_run_start: bool = False
    failed: bool = False
    error: str = ""


@dataclass
class FinishWaitInput:
    run_id: str
    agent_id: str
    key: str
    hours: float
    resumed_at: str
    from_run_start: bool = False


def parse_hours(raw: Any) -> float:
    try:
        hours = float(str(raw).strip())
    except (TypeError, ValueError) as exc:
        raise ValueError(f"hours must be a number, not {raw!r}") from exc
    if hours < 0 or hours > MAX_WAIT_HOURS:
        raise ValueError(f"hours must be between 0 and {MAX_WAIT_HOURS:g}")
    return hours


@activity.defn
async def prepare_wait(inp: PrepareWaitInput) -> PrepareWaitResult:
    cfg = dict(inp.node.get("config") or {})
    ctx = NodeContext(run_id=inp.run_id, agent_id=inp.agent_id, node=inp.node, upstream=inp.upstream,
                      run_input=inp.run_input, extra=inp.extra)
    try:
        hours = parse_hours(nodes.render(cfg.get("hours", ""), ctx))
        start = str(cfg.get("from") or "now").strip().lower()
        if start not in ("now", "run_start"):
            raise ValueError("from must be now or run_start")
    except Exception as exc:  # noqa: BLE001 - a bad config fails the node with its reason
        await db.set_agent_status(inp.agent_id, "failed", summary=str(exc)[:280])
        await db.set_agent_result(inp.agent_id, error=str(exc), attempts=1)
        return PrepareWaitResult(failed=True, error=str(exc))
    what = f"{hours:g} h after the run started" if start == "run_start" else f"{hours:g} h"
    await db.set_agent_status(inp.agent_id, "waiting", summary=f"Waiting {what}")
    await db.append_log(inp.run_id, "info", "system", inp.node["key"], f"Waiting {what}")
    return PrepareWaitResult(hours=hours, from_run_start=start == "run_start")


@activity.defn
async def finish_wait(inp: FinishWaitInput) -> RunNodeResult:
    out = {"waitedHours": inp.hours, "resumedAt": inp.resumed_at, "fromRunStart": inp.from_run_start}
    await db.set_agent_status(inp.agent_id, "succeeded", summary=f"Resumed {inp.resumed_at}")
    await db.set_agent_result(inp.agent_id, output=out, attempts=1)
    return RunNodeResult(output=out, gate_open=True)


ACTIVITIES = [prepare_wait, finish_wait]
