"""Tests for the dynamic execution engine — runnable without Postgres/Temporal.

    python -m unittest discover -s tests   (from apps/reasoner)

A dummy DATABASE_URL is set before import so config.load() succeeds; every db
function the engine touches is then replaced with an in-memory fake, so no real
connection is ever opened.
"""
from __future__ import annotations

import asyncio
import os
import unittest

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")

from reasoner import db, engine, nodes  # noqa: E402
from reasoner.llm import Completion  # noqa: E402
from reasoner.nodes import NodeContext  # noqa: E402


class FakeDB:
    """Records every write the engine makes so tests can assert on them."""

    def __init__(self) -> None:
        self.agents: dict[str, dict] = {}
        self.logs: list[tuple] = []
        self.artifacts: list[tuple] = []
        self.messages: list[tuple] = []
        self.run_status: str | None = None
        self.progress: tuple | None = None
        self.usage = [0, 0, 0.0]

    def install(self) -> None:
        async def upsert_agent(run_id, key, name, role, model, deps, col, row):
            aid = f"ra_{key}"
            self.agents[aid] = {"key": key, "role": role, "col": col, "row": row, "status": "idle"}
            return aid

        async def set_agent_status(agent_id, status, summary=None):
            self.agents.setdefault(agent_id, {})["status"] = status

        async def append_log(run_id, level, channel, source, message, detail=None, reasoning=False):
            self.logs.append((level, source, message))

        async def add_artifact(run_id, name, kind, produced_by_name, preview):
            self.artifacts.append((name, kind, preview))
            return f"art_{name}"

        async def add_message(run_id, frm, to, label):
            self.messages.append((frm, to, label))

        async def set_run_running(run_id, step):
            self.run_status = "running"

        async def set_run_progress(run_id, done, total, step):
            self.progress = (done, total)

        async def finish_run(run_id, status, step, error=None):
            self.run_status = status

        async def add_usage(run_id, ti, to, cost):
            self.usage[0] += ti
            self.usage[1] += to
            self.usage[2] += cost

        async def set_langfuse_trace(run_id, trace_id):
            pass

        for name, fn in locals().items():
            if name != "self" and callable(fn):
                setattr(db, name, fn)


def _run(coro):
    return asyncio.run(coro)


class ToposortTest(unittest.TestCase):
    def test_linear_order(self):
        g = [
            {"key": "c", "type": "output.report", "dependsOn": ["b"]},
            {"key": "a", "type": "trigger.manual", "dependsOn": []},
            {"key": "b", "type": "agent.llm", "dependsOn": ["a"]},
        ]
        order = [n["key"] for n in engine.topo_order(g)]
        self.assertEqual(order, ["a", "b", "c"])

    def test_cycle_detected(self):
        g = [
            {"key": "a", "type": "agent.llm", "dependsOn": ["b"]},
            {"key": "b", "type": "agent.llm", "dependsOn": ["a"]},
        ]
        with self.assertRaises(engine.GraphError):
            engine.topo_order(g)

    def test_missing_dependency(self):
        g = [{"key": "a", "type": "agent.llm", "dependsOn": ["ghost"]}]
        with self.assertRaises(engine.GraphError):
            engine.topo_order(g)

    def test_diamond_grid_columns(self):
        g = [
            {"key": "a", "type": "trigger.manual", "dependsOn": []},
            {"key": "b", "type": "agent.llm", "dependsOn": ["a"]},
            {"key": "c", "type": "agent.llm", "dependsOn": ["a"]},
            {"key": "d", "type": "output.report", "dependsOn": ["b", "c"]},
        ]
        grid = engine._grid(g)
        self.assertEqual(grid["a"][0], 0)
        self.assertEqual(grid["b"][0], 1)
        self.assertEqual(grid["c"][0], 1)
        self.assertEqual(grid["d"][0], 2)  # longest-path depth


class TemplateTest(unittest.TestCase):
    def _ctx(self):
        return NodeContext(
            run_id="run_1", agent_id="ra_x",
            node={"key": "x", "type": "agent.llm", "config": {}},
            upstream={"fetch": {"text": "hello", "status": 200}},
            run_input={"topic": "otters"},
        )

    def test_input_and_output_refs(self):
        ctx = self._ctx()
        self.assertEqual(nodes.render("t={{input.topic}}", ctx), "t=otters")
        self.assertEqual(nodes.render("u={{outputs.fetch.text}}", ctx), "u=hello")

    def test_unknown_ref_is_empty(self):
        self.assertEqual(nodes.render("x={{outputs.nope.text}}", self._ctx()), "x=")

    def test_condition_eval(self):
        ctx = self._ctx()
        self.assertTrue(nodes._eval_condition("outputs.fetch.status == 200", ctx))
        self.assertFalse(nodes._eval_condition("outputs.fetch.status == 500", ctx))
        self.assertTrue(nodes._eval_condition("", ctx))  # empty → fail-open true


class ExecuteGraphTest(unittest.TestCase):
    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()

        async def fake_complete(run_id, node, system, user, *, model=None):
            return Completion(text=f"answer:{node}", tokens_in=10, tokens_out=5, cost_usd=0.01)

        async def fake_browse(run_id, label, urls, **kw):
            return f"browsed {len(urls)} urls"

        nodes.llm.complete = fake_complete
        nodes.browser.run_browse = fake_browse

    def test_end_to_end_run(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "research", "type": "agent.llm", "config": {"prompt": "on {{input.topic}}"}, "dependsOn": ["start"]},
            {"key": "out", "type": "output.report", "config": {"title": "Digest"}, "dependsOn": ["research"]},
        ]
        result = _run(engine.execute_graph("run_abc", g, {"topic": "sea otters"}))

        self.assertTrue(result["done"])
        self.assertEqual(self.fake.run_status, "succeeded")
        self.assertEqual(self.fake.progress, (3, 3))
        # every agent ended succeeded
        self.assertTrue(all(a["status"] == "succeeded" for a in self.fake.agents.values()))
        # the report handler wrote an artifact from the agent's text output
        self.assertEqual(len(self.fake.artifacts), 1)
        self.assertIn("answer:research", self.fake.artifacts[0][2])
        # usage rolled up from the one llm call
        self.assertEqual(self.fake.usage[0], 10)

    def test_node_failure_fails_run(self):
        async def boom(run_id, node, system, user, *, model=None):
            raise RuntimeError("model exploded")

        nodes.llm.complete = boom
        g = [
            {"key": "a", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "b", "type": "agent.llm", "config": {}, "dependsOn": ["a"]},
        ]
        result = _run(engine.execute_graph("run_x", g, {}))
        self.assertFalse(result["done"])
        self.assertEqual(result["failed_node"], "b")
        self.assertEqual(self.fake.run_status, "failed")


if __name__ == "__main__":
    unittest.main()
