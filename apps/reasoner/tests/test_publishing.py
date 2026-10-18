"""Run-time enforcement of "nothing publishes without approval"."""
from __future__ import annotations

import asyncio
import os
import unittest
import uuid
from unittest import mock

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("GEMINI_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")

from temporalio import activity  # noqa: E402
from temporalio.testing import WorkflowEnvironment  # noqa: E402
from temporalio.worker import Worker  # noqa: E402

from reasoner import activities as A  # noqa: E402
from reasoner import publishing  # noqa: E402
from reasoner.workflow import DynamicWorkflow  # noqa: E402

POST = "pieces.linkedin.create_share_update"
UNGUARDED = [{"key": "draft", "type": "agent.llm"}, {"key": "post", "type": POST, "dependsOn": ["draft"]}]
GUARDED = [{"key": "draft", "type": "agent.llm"},
           {"key": "ok", "type": "human.approval", "dependsOn": ["draft"]},
           {"key": "tidy", "type": "agent.llm", "dependsOn": ["ok"]},
           {"key": "post", "type": POST, "dependsOn": ["tidy"]}]


class UnguardedTest(unittest.TestCase):
    def test_checker(self):
        self.assertEqual(publishing.unguarded(UNGUARDED), ["post"])
        self.assertEqual(publishing.unguarded(GUARDED), [])
        self.assertEqual(publishing.unguarded([{"key": "s", "type": "slack.sendMessage"}]), [])
        self.assertIn("pieces.mailchimp.send_campaign", publishing.publishing_types())

    def test_load_graph_refuses_an_unguarded_graph(self):
        with mock.patch.object(A.db, "fetch_graph_nodes", mock.AsyncMock(return_value=UNGUARDED)):
            res = asyncio.run(A.load_graph(A.LoadGraphInput(run_id="run_1")))
        self.assertTrue(res.dynamic)
        self.assertIn("refusing to run: post publishes publicly with no human.approval", res.error)
        self.assertEqual(res.graph, [], "nothing is seeded for a graph that won't run")


class WorkflowRefusesTest(unittest.TestCase):
    def test_run_fails_with_the_reason_and_runs_nothing(self):
        ran: list[str] = []
        finished: list[A.FinishInput] = []

        @activity.defn(name="load_graph")
        async def load_graph(inp: A.LoadGraphInput) -> A.LoadGraphResult:
            return A.LoadGraphResult(dynamic=True, error="refusing to run: post publishes publicly")

        @activity.defn(name="run_node")
        async def run_node(inp: A.RunNodeInput) -> A.RunNodeResult:
            ran.append(inp.node["key"])
            return A.RunNodeResult()

        @activity.defn(name="finish_run")
        async def finish_run(inp: A.FinishInput) -> None:
            finished.append(inp)

        async def go():
            env = await WorkflowEnvironment.start_time_skipping()
            try:
                async with Worker(env.client, task_queue="q-pub", workflows=[DynamicWorkflow],
                                  activities=[load_graph, run_node, finish_run]):
                    return await env.client.execute_workflow(
                        DynamicWorkflow.run, {"run_id": "run_x", "input": {}, "exec": {}},
                        id=f"wf-{uuid.uuid4()}", task_queue="q-pub")
            finally:
                await env.shutdown()

        res = asyncio.run(go())
        self.assertFalse(res["done"])
        self.assertEqual(ran, [])
        self.assertEqual(finished[0].status, "failed")
        self.assertIn("publishes publicly", finished[0].error)


if __name__ == "__main__":
    unittest.main()
