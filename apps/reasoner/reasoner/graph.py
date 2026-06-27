"""The reasoning graph: plan → browse → synthesize → deliver.

Every node is declared `execute_in="activity"` so it runs as a Temporal activity —
durable, independently retried, and (critically) NOT re-run on workflow replay.
That is the "reasoning runs inside durability" principle made concrete: a crash mid
browse resumes at the browse activity, not from the top.

Each node also writes its progress into the shared Agently Postgres (runs / run_logs
/ run_agents / artifacts / browser_*), so the existing UI shows the run live. All DB
writes are keyed by run id and idempotent enough to survive an activity retry.
"""
from __future__ import annotations

from datetime import timedelta
from typing import Any, TypedDict

from langgraph.graph import END, START, StateGraph
from temporalio.common import RetryPolicy

from . import browser, db, llm, obs
from .config import CONFIG

GRAPH_NAME = "agently-reason"

# The four nodes, as they appear in the Agents tab. (key, name, role, col)
_NODES = [
    ("plan", "Planner", "orchestrator", 0),
    ("browse", "Browser", "browser", 1),
    ("synthesize", "Synthesizer", "analyst", 2),
    ("deliver", "Composer", "writer", 3),
]
_DEPS = {"plan": [], "browse": ["plan"], "synthesize": ["browse"], "deliver": ["synthesize"]}
_TOTAL_STEPS = len(_NODES)


class ReasonState(TypedDict, total=False):
    run_id: str
    input: dict[str, Any]
    plan: str
    findings: str
    summary: str
    done: bool


# ───────────────────────────── nodes ─────────────────────────────

async def _ensure_agents(run_id: str) -> dict[str, str]:
    """Create the four run_agent rows (idempotent) and return key→agent_id."""
    ids: dict[str, str] = {}
    for key, name, role, col in _NODES:
        ids[key] = await db.upsert_agent(
            run_id, key, name, role, "claude-sonnet-4-6", _DEPS[key], col, 0
        )
    return ids


async def plan_node(state: ReasonState) -> dict[str, Any]:
    run_id = state["run_id"]
    inp = state.get("input", {})
    await db.set_run_running(run_id, "Planning")
    # Only stamp a Langfuse handle when tracing is actually configured, so the UI
    # never renders a dead "View trace" link.
    if obs.enabled():
        await db.set_langfuse_trace(run_id, obs.session_handle(run_id))
    ids = await _ensure_agents(run_id)
    await db.set_agent_status(ids["plan"], "running")
    await db.append_log(run_id, "info", "agent", "planner", "Decomposing the task")

    topic = str(inp.get("topic") or inp.get("prompt") or "the requested task")
    out = await llm.complete(
        run_id, "plan",
        system="You are a planning agent. Produce a short, concrete plan (3-5 bullets) "
               "for accomplishing the user's task using web browsing then summarization.",
        user=f"Task: {topic}\nKnown input: {inp}",
    )
    await db.add_usage(run_id, out.tokens_in, out.tokens_out, out.cost_usd)
    await db.append_log(run_id, "info", "model", "planner", "Plan ready", detail=out.text, reasoning=True)
    await db.set_agent_status(ids["plan"], "succeeded", summary=_clip(out.text))
    await db.add_message(run_id, ids["plan"], ids["browse"], "plan → browse")
    await db.set_run_progress(run_id, 1, _TOTAL_STEPS, "Browsing")
    return {"plan": out.text}


async def browse_node(state: ReasonState) -> dict[str, Any]:
    run_id = state["run_id"]
    inp = state.get("input", {})
    ids = await _ensure_agents(run_id)
    await db.set_agent_status(ids["browse"], "running")

    urls = _urls_from_input(inp)
    await db.append_log(run_id, "info", "browser", "browser", f"Visiting {len(urls)} page(s)")
    findings = await browser.run_browse(run_id, "Browser", urls)
    await db.set_agent_status(ids["browse"], "succeeded", summary=f"Visited {len(urls)} page(s)")
    await db.add_message(run_id, ids["browse"], ids["synthesize"], "browse → synthesize")
    await db.set_run_progress(run_id, 2, _TOTAL_STEPS, "Synthesizing")
    return {"findings": findings}


async def synthesize_node(state: ReasonState) -> dict[str, Any]:
    run_id = state["run_id"]
    inp = state.get("input", {})
    ids = await _ensure_agents(run_id)
    await db.set_agent_status(ids["synthesize"], "running")
    await db.append_log(run_id, "info", "agent", "synthesizer", "Summarizing findings")

    topic = str(inp.get("topic") or inp.get("prompt") or "the task")
    out = await llm.complete(
        run_id, "synthesize",
        system="You are a synthesis agent. Write a clear, well-structured markdown digest "
               "answering the task from the browsed material. Be concrete and cite page titles.",
        user=f"Task: {topic}\n\nPlan:\n{state.get('plan','')}\n\nBrowsed material:\n{state.get('findings','')[:8000]}",
        model=CONFIG.synthesis_model,
    )
    await db.add_usage(run_id, out.tokens_in, out.tokens_out, out.cost_usd)
    await db.append_log(run_id, "success", "model", "synthesizer", "Digest drafted", detail=out.text, reasoning=True)
    await db.set_agent_status(ids["synthesize"], "succeeded", summary=_clip(out.text))
    await db.add_message(run_id, ids["synthesize"], ids["deliver"], "synthesize → deliver")
    await db.set_run_progress(run_id, 3, _TOTAL_STEPS, "Delivering")
    return {"summary": out.text}


async def deliver_node(state: ReasonState) -> dict[str, Any]:
    run_id = state["run_id"]
    inp = state.get("input", {})
    ids = await _ensure_agents(run_id)
    await db.set_agent_status(ids["deliver"], "running")

    summary = state.get("summary", "")
    await db.add_artifact(run_id, "digest.md", "report", "Composer", summary)
    email = inp.get("email")
    if email:
        # Email delivery is reused from the Go notifier in a later phase; for the
        # slice we record the intent honestly rather than pretend to send.
        await db.append_log(run_id, "info", "system", "composer", f"Digest ready — would email to {email}")
    await db.set_agent_status(ids["deliver"], "succeeded", summary="Digest produced")
    await db.set_run_progress(run_id, _TOTAL_STEPS, _TOTAL_STEPS, "Completed")
    await db.finish_run(run_id, "succeeded", "Completed")
    obs.flush()
    return {"done": True}


# ──────────────────────────── helpers ────────────────────────────

def _clip(s: str, n: int = 280) -> str:
    s = s.strip().replace("\n", " ")
    return s if len(s) <= n else s[: n - 1] + "…"


def _urls_from_input(inp: dict[str, Any]) -> list[str]:
    urls = inp.get("urls")
    if isinstance(urls, list) and urls:
        return [str(u) for u in urls]
    if isinstance(inp.get("url"), str):
        return [inp["url"]]
    # Sensible default that exercises the acceptance prompt (Show HN launches).
    return ["https://news.ycombinator.com/show"]


# ─────────────────────────── graph build ──────────────────────────

def build_graph() -> StateGraph:
    """Return the (uncompiled) StateGraph; the plugin compiles it in-workflow."""
    g = StateGraph(ReasonState)

    common_retry = RetryPolicy(maximum_attempts=3)
    g.add_node("plan", plan_node, metadata={
        "execute_in": "activity",
        "start_to_close_timeout": timedelta(seconds=120),
        "retry_policy": common_retry,
    })
    g.add_node("browse", browse_node, metadata={
        "execute_in": "activity",
        "start_to_close_timeout": timedelta(seconds=300),  # browsing is slow
        "retry_policy": RetryPolicy(maximum_attempts=2),
    })
    g.add_node("synthesize", synthesize_node, metadata={
        "execute_in": "activity",
        "start_to_close_timeout": timedelta(seconds=180),
        "retry_policy": common_retry,
    })
    g.add_node("deliver", deliver_node, metadata={
        "execute_in": "activity",
        "start_to_close_timeout": timedelta(seconds=60),
        "retry_policy": common_retry,
    })

    g.add_edge(START, "plan")
    g.add_edge("plan", "browse")
    g.add_edge("browse", "synthesize")
    g.add_edge("synthesize", "deliver")
    g.add_edge("deliver", END)
    return g
