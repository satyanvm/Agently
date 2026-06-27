"""The Temporal workflow that drives the reasoning graph.

The workflow itself is deterministic and tiny: it compiles the registered LangGraph
graph and invokes it. All the real work (LLM calls, browsing, DB writes) happens in
the graph's nodes, which the LangGraphPlugin runs as activities. Temporal's event
history is the durable checkpoint — kill the worker mid-run and it resumes at the
in-flight node activity.
"""
from __future__ import annotations

from typing import Any

from temporalio import workflow
from temporalio.contrib.langgraph import graph

# Hardcoded (not imported from .graph) so the workflow sandbox never pulls in the
# node modules' non-deterministic deps (psycopg, httpx, playwright). Must match
# reasoner.graph.GRAPH_NAME.
GRAPH_NAME = "agently-reason"


@workflow.defn
class ReasoningWorkflow:
    @workflow.run
    async def run(self, params: dict[str, Any]) -> dict[str, Any]:
        run_id = params["run_id"]
        initial = {"run_id": run_id, "input": params.get("input", {})}
        result = await graph(GRAPH_NAME).compile().ainvoke(initial)
        return {"run_id": run_id, "done": bool(result.get("done", False))}
