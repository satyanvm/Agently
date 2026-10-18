"""DynamicWorkflow node settings + builder test runs, on a real Temporal test
server (time-skipping) with fake activities registered under the real names.

Covers: per-node retries, continue-on-fail, timeouts, pinned data, and step
runs (target node + reused upstream outputs).
"""
from __future__ import annotations

import asyncio
import os
import unittest
import uuid
from typing import Any

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("GEMINI_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")
os.environ.setdefault("SMTP_HOST", "localhost")

from temporalio import activity  # noqa: E402
from temporalio.testing import WorkflowEnvironment  # noqa: E402
from temporalio.worker import Worker  # noqa: E402

from reasoner import activities as A  # noqa: E402
from reasoner import plan  # noqa: E402
from reasoner.workflow import DynamicWorkflow  # noqa: E402

QUEUE = "test-policy"


class Recorder:
    """Fake activity implementations; behaviour per node key via `fail_times`
    (fail the first N attempts), `always_fail` and `sleep_s`."""

    def __init__(self, graph: list[dict[str, Any]]):
        self.graph = graph
        self.ran: list[tuple[str, int]] = []
        self.used: list[tuple[str, str]] = []
        self.skipped: list[tuple[str, str]] = []
        self.failed: list[A.FailNodeInput] = []
        self.finish: A.FinishInput | None = None
        self.upstream: dict[str, dict] = {}
        self.fail_times: dict[str, int] = {}
        self.always_fail: set[str] = set()
        self.sleep_s: dict[str, float] = {}

    def acts(self):
        rec = self

        @activity.defn(name="load_graph")
        async def load_graph(inp: A.LoadGraphInput) -> A.LoadGraphResult:
            g = [A._lift_reserved(dict(n)) for n in plan.build_plan(rec.graph).ordered]
            return A.LoadGraphResult(
                dynamic=True, graph=g, ordered_keys=[n["key"] for n in g],
                agent_ids={n["key"]: f"ra_{n['key']}" for n in g}, prior_status={},
            )

        @activity.defn(name="run_node")
        async def run_node(inp: A.RunNodeInput) -> A.RunNodeResult:
            key = inp.node["key"]
            rec.ran.append((key, inp.attempt))
            rec.upstream[key] = inp.upstream
            if key in rec.sleep_s:
                await asyncio.sleep(rec.sleep_s[key])
            if key in rec.always_fail or inp.attempt <= rec.fail_times.get(key, 0):
                return A.RunNodeResult(failed=True, error=f"{key} broke")
            return A.RunNodeResult(output={"value": key})

        @activity.defn(name="skip_node")
        async def skip_node(inp: A.SkipNodeInput) -> None:
            rec.skipped.append((inp.key, inp.reason))

        @activity.defn(name="use_output")
        async def use_output(inp: A.UseOutputInput) -> A.RunNodeResult:
            rec.used.append((inp.key, inp.source))
            return A.RunNodeResult(output=inp.output)

        @activity.defn(name="fail_node")
        async def fail_node(inp: A.FailNodeInput) -> None:
            rec.failed.append(inp)

        @activity.defn(name="finish_run")
        async def finish_run(inp: A.FinishInput) -> None:
            rec.finish = inp

        @activity.defn(name="add_edge")
        async def add_edge(inp: A.EdgeInput) -> None:
            return None

        @activity.defn(name="set_progress")
        async def set_progress(inp: A.ProgressInput) -> None:
            return None

        return [load_graph, run_node, skip_node, use_output, fail_node, finish_run, add_edge, set_progress]


def node(key: str, deps: list[str] | None = None, **config: Any) -> dict[str, Any]:
    return {"key": key, "type": "agent.llm", "config": config, "dependsOn": deps or []}


class WorkflowPolicyTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.loop = asyncio.new_event_loop()
        cls.env = cls.loop.run_until_complete(WorkflowEnvironment.start_time_skipping())

    @classmethod
    def tearDownClass(cls):
        cls.loop.run_until_complete(cls.env.shutdown())
        cls.loop.close()

    def run_wf(self, rec: Recorder, exec_opts: dict[str, Any] | None = None) -> dict[str, Any]:
        async def go():
            async with Worker(self.env.client, task_queue=QUEUE, workflows=[DynamicWorkflow],
                              activities=rec.acts()):
                return await self.env.client.execute_workflow(
                    DynamicWorkflow.run,
                    {"run_id": "run_x", "input": {}, "exec": exec_opts or {}},
                    id=f"wf-{uuid.uuid4()}", task_queue=QUEUE,
                )
        return self.loop.run_until_complete(go())

    def test_retries_until_success(self):
        rec = Recorder([node("a"), node("b", ["a"], __settings={"retries": 2, "retryWaitSec": 30})])
        rec.fail_times["b"] = 2
        res = self.run_wf(rec)
        self.assertTrue(res["done"])
        self.assertEqual([r for r in rec.ran if r[0] == "b"], [("b", 1), ("b", 2), ("b", 3)])
        self.assertEqual(rec.finish.status, "succeeded")

    def test_failure_without_settings_fails_run(self):
        rec = Recorder([node("a"), node("b", ["a"])])
        rec.always_fail.add("a")
        res = self.run_wf(rec)
        self.assertFalse(res["done"])
        self.assertEqual(rec.finish.status, "failed")
        self.assertNotIn("b", [k for k, _ in rec.ran])

    def test_continue_on_fail_passes_error_downstream(self):
        rec = Recorder([node("a", __settings={"continueOnFail": True, "retries": 1, "retryWaitSec": 1}),
                        node("b", ["a"])])
        rec.always_fail.add("a")
        res = self.run_wf(rec)
        self.assertTrue(res["done"])
        self.assertEqual(rec.upstream["b"], {"a": {"error": "a broke"}})
        self.assertEqual((rec.failed[0].attempts, rec.failed[0].continued), (2, True))
        self.assertEqual(rec.finish.status, "succeeded")

    def test_timeout_fails_node(self):
        rec = Recorder([node("slow", __settings={"timeoutSec": 1})])
        rec.sleep_s["slow"] = 5
        res = self.run_wf(rec)
        self.assertFalse(res["done"])
        self.assertIn("timed out after 1s", rec.failed[0].error)
        self.assertEqual(len(rec.ran), 1)  # a user timeout is not retried by Temporal

    def test_pinned_node_not_executed_in_test_runs_only(self):
        graph = [node("a", __pinned={"value": "pinned!"}), node("b", ["a"])]
        rec = Recorder(graph)
        self.run_wf(rec, {"usePins": True})
        self.assertEqual(rec.used, [("a", "pinned")])
        self.assertEqual(rec.upstream["b"], {"a": {"value": "pinned!"}})
        self.assertNotIn("a", [k for k, _ in rec.ran])

        prod = Recorder(graph)  # production run: pins ignored
        self.run_wf(prod)
        self.assertIn("a", [k for k, _ in prod.ran])

    def test_step_run_reuses_ancestors_and_skips_the_rest(self):
        rec = Recorder([node("a"), node("b", ["a"]), node("c", ["b"]), node("side")])
        res = self.run_wf(rec, {"targetNode": "b", "seed": {"a": {"value": "from last run"}}})
        self.assertTrue(res["done"])
        self.assertEqual(rec.used, [("a", "reused")])
        self.assertEqual([k for k, _ in rec.ran], ["b"])
        self.assertEqual(rec.upstream["b"], {"a": {"value": "from last run"}})
        self.assertEqual(sorted(k for k, _ in rec.skipped), ["c", "side"])
        self.assertIn("Step run of b", rec.finish.step)


class PlanPolicyTest(unittest.TestCase):
    def test_node_policy_clamps(self):
        self.assertEqual(plan.node_policy({}), (0, 5, None, False))
        self.assertEqual(plan.node_policy({"settings": {"retries": 99, "timeoutSec": "30"}}), (10, 5, 30.0, False))
        self.assertEqual(plan.node_policy({"settings": {"retries": "x"}})[0], 0)

    def test_ancestors(self):
        g = [node("a"), node("b", ["a"]), node("c", ["b", "d"]), node("d")]
        self.assertEqual(plan.ancestors("c", g), {"a", "b", "d"})
        self.assertEqual(plan.ancestors("a", g), set())


if __name__ == "__main__":
    unittest.main()
