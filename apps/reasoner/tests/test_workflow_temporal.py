"""DynamicWorkflow on a real Temporal dev server, with the activities stubbed.

Checks what the pure-logic tests in test_engine.py cannot: that independent
nodes really overlap, that the concurrency cap holds, and that both the new
parallel schedule and a history recorded by the old sequential loop replay
deterministically.

Needs the Temporal CLI. It is downloaded on first use; set TEMPORAL_CLI_PATH to
an existing binary to skip the download. If neither works the tests skip.
"""
from __future__ import annotations

import asyncio
import json
import os
import unittest
import uuid
from pathlib import Path

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("GEMINI_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")
os.environ.setdefault("SMTP_HOST", "localhost")

from temporalio import activity  # noqa: E402
from temporalio.client import WorkflowHistory  # noqa: E402
from temporalio.testing import WorkflowEnvironment  # noqa: E402
from temporalio.worker import Replayer, Worker  # noqa: E402

from reasoner import activities as acts  # noqa: E402
from reasoner import plan  # noqa: E402
from reasoner.workflow import DynamicWorkflow  # noqa: E402

FIXTURES = Path(__file__).parent / "fixtures"
QUEUE = "test-dynamic"


def node(key, deps=(), type_="agent.llm", **config):
    return {"key": key, "type": type_, "config": config, "dependsOn": list(deps)}


class Recorder:
    """Stub activities sharing one log, so a test can see what ran and when."""

    def __init__(self, graph, *, delay=0.2, delays=None, fail=(), closed_gates=(), items=None, prior=None):
        self.graph = plan.build_plan(graph).ordered
        self.delay = delay
        self.delays = delays or {}  # per-key override of `delay`
        self.fail = set(fail)
        self.closed_gates = set(closed_gates)
        self.items = items or {}
        self.prior = prior or {}
        self.events: list[tuple[str, str]] = []  # ("start"|"end", key)
        self.skipped: list[str] = []
        self.finished: list[tuple[str, str]] = []
        self.running = 0
        self.peak = 0

    def activities(self):
        rec = self

        @activity.defn(name="load_graph")
        async def load_graph(inp: acts.LoadGraphInput) -> acts.LoadGraphResult:
            return acts.LoadGraphResult(
                dynamic=True,
                graph=rec.graph,
                ordered_keys=[n["key"] for n in rec.graph],
                agent_ids={n["key"]: f"ra_{n['key']}" for n in rec.graph},
                prior_status={f"ra_{k}": v for k, v in rec.prior.items()},
            )

        @activity.defn(name="run_node")
        async def run_node(inp: acts.RunNodeInput) -> acts.RunNodeResult:
            key = inp.node["key"]
            rec.events.append(("start", key))
            rec.running += 1
            rec.peak = max(rec.peak, rec.running)
            try:
                await asyncio.sleep(rec.delays.get(key, rec.delay))
            finally:
                rec.running -= 1
                rec.events.append(("end", key))
            if key in rec.fail:
                return acts.RunNodeResult(failed=True, error=f"{key} broke")
            output = {"text": f"{key}:{inp.extra.get('item', '')}"}
            if key in rec.items:
                output["items"] = rec.items[key]
            return acts.RunNodeResult(output=output, gate_open=key not in rec.closed_gates)

        @activity.defn(name="prepare_piece_node")
        async def prepare_piece_node(inp: acts.PreparePieceInput) -> acts.PreparePieceResult:
            from temporalio.exceptions import ApplicationError
            rec.events.append(("start", inp.node["key"]))
            raise ApplicationError("pieces.slack.x is missing credential AP_SLACK_AUTH", non_retryable=True)

        @activity.defn(name="skip_node")
        async def skip_node(inp: acts.SkipNodeInput) -> None:
            rec.skipped.append(inp.key)

        @activity.defn(name="add_edge")
        async def add_edge(inp: acts.EdgeInput) -> None:
            pass

        @activity.defn(name="set_progress")
        async def set_progress(inp: acts.ProgressInput) -> None:
            pass

        @activity.defn(name="finish_run")
        async def finish_run(inp: acts.FinishInput) -> None:
            rec.finished.append((inp.status, inp.error))

        return [load_graph, run_node, prepare_piece_node, skip_node, add_edge, set_progress, finish_run]

    def started(self) -> list[str]:
        return [k for e, k in self.events if e == "start"]

    def index(self, kind: str, key: str) -> int:
        return self.events.index((kind, key))


async def _start_env() -> WorkflowEnvironment:
    path = os.environ.get("TEMPORAL_CLI_PATH")
    if path:
        return await WorkflowEnvironment.start_local(dev_server_existing_path=path)
    return await WorkflowEnvironment.start_local()


async def run_workflow(env, rec: Recorder, params: dict) -> tuple[dict, WorkflowHistory]:
    async with Worker(env.client, task_queue=QUEUE, workflows=[DynamicWorkflow], activities=rec.activities()):
        handle = await env.client.start_workflow(
            DynamicWorkflow.run, {"run_id": "run_test", **params},
            id=f"wf-{uuid.uuid4()}", task_queue=QUEUE,
        )
        result = await handle.result()
        history = await handle.fetch_history()
    return result, history


class DynamicWorkflowTemporalTest(unittest.IsolatedAsyncioTestCase):
    env: WorkflowEnvironment | None = None

    async def asyncSetUp(self):
        try:
            self.env = await _start_env()
        except Exception as exc:  # noqa: BLE001 - no CLI available offline
            self.skipTest(f"Temporal dev server unavailable: {exc}")

    async def asyncTearDown(self):
        if self.env is not None:
            await self.env.shutdown()

    async def assertReplays(self, history: WorkflowHistory):
        await Replayer(workflows=[DynamicWorkflow]).replay_workflow(history)

    async def test_independent_nodes_overlap_up_to_the_cap(self):
        fans = [node(f"src{i}", ["start"]) for i in range(6)]
        graph = [node("start", type_="trigger.manual"), *fans, node("join", [f["key"] for f in fans])]
        rec = Recorder(graph)
        result, history = await run_workflow(self.env, rec, {"max_parallel": 3})

        self.assertTrue(result["done"])
        self.assertEqual(rec.peak, 3)  # six ready sources, three slots
        self.assertEqual(rec.finished, [("succeeded", "")])
        # The join waits for every source.
        join_start = rec.index("start", "join")
        for i in range(6):
            self.assertLess(rec.index("end", f"src{i}"), join_start)
        await self.assertReplays(history)

    async def test_branches_do_not_wait_for_each_other(self):
        # a1→a2 is a slow chain only in time: b runs alongside it, not after it.
        graph = [
            node("start", type_="trigger.manual"),
            node("a1", ["start"]), node("a2", ["a1"]),
            node("b", ["start"]),
            node("end", ["a2", "b"]),
        ]
        rec = Recorder(graph)
        result, history = await run_workflow(self.env, rec, {"max_parallel": 8})
        self.assertTrue(result["done"])
        self.assertLess(rec.index("start", "b"), rec.index("end", "a1"))
        await self.assertReplays(history)

    async def test_closed_gate_skips_its_subgraph_only(self):
        graph = [
            node("start", type_="trigger.manual"),
            node("gate", ["start"], type_="logic.branch"),
            node("guarded", ["gate"]), node("deep", ["guarded"]),
            node("sibling", ["start"]),
        ]
        rec = Recorder(graph, closed_gates={"gate"})
        result, history = await run_workflow(self.env, rec, {})
        self.assertTrue(result["done"])
        self.assertEqual(result["skipped"], ["deep", "guarded"])
        self.assertEqual(sorted(rec.skipped), ["deep", "guarded"])
        self.assertIn("sibling", rec.started())
        self.assertNotIn("guarded", rec.started())
        await self.assertReplays(history)

    async def test_failure_stops_new_work_and_fails_the_run(self):
        graph = [
            node("start", type_="trigger.manual"),
            node("bad", ["start"]), node("slow", ["start"]),
            node("after_bad", ["bad"]), node("after_slow", ["slow"]),
        ]
        # slow outlives bad by a wide margin, so its dependents are only ever
        # reached after the failure is known.
        rec = Recorder(graph, fail={"bad"}, delays={"slow": 1.0})
        result, history = await run_workflow(self.env, rec, {})
        self.assertFalse(result["done"])
        self.assertEqual(result["failed_node"], "bad")
        self.assertEqual(rec.finished, [("failed", "bad broke")])
        self.assertIn("slow", rec.started())       # was already running
        self.assertNotIn("after_bad", rec.started())
        self.assertNotIn("after_slow", rec.started())  # nothing new after the failure
        await self.assertReplays(history)

    async def test_loop_body_runs_per_item(self):
        graph = [
            node("start", type_="trigger.manual"),
            node("loop", ["start"], type_="logic.loop"),
            node("body", ["loop"]),
            node("other", ["start"]),
            node("after", ["loop", "other"]),
        ]
        rec = Recorder(graph, items={"loop": ["x", "y", "z"]})
        result, history = await run_workflow(self.env, rec, {})
        self.assertTrue(result["done"])
        self.assertEqual(rec.started().count("body"), 3)
        self.assertEqual(rec.started()[-1], "after")
        await self.assertReplays(history)

    async def test_piece_config_error_reason_reaches_the_run(self):
        graph = [node("start", type_="trigger.manual"), node("post", ["start"], type_="pieces.slack.x")]
        rec = Recorder(graph)
        result, _ = await run_workflow(self.env, rec, {})
        self.assertEqual(result["failed_node"], "post")
        self.assertEqual(rec.started().count("post"), 1)  # not retried
        self.assertEqual(
            rec.finished, [("failed", "pieces.slack.x: pieces.slack.x is missing credential AP_SLACK_AUTH")]
        )

    async def test_resumed_nodes_are_not_rerun(self):
        graph = [node("start", type_="trigger.manual"), node("a", ["start"]), node("b", ["a"])]
        rec = Recorder(graph, prior={"start": "succeeded", "a": "succeeded"})
        result, _ = await run_workflow(self.env, rec, {})
        self.assertTrue(result["done"])
        self.assertEqual(rec.started(), ["b"])

    async def test_history_from_the_sequential_loop_still_replays(self):
        # Recorded from the one-node-at-a-time workflow before parallel-nodes-v1;
        # runs in flight during the upgrade must replay on the old path.
        raw = (FIXTURES / "dynamic_workflow_sequential_history.json").read_text()
        history = WorkflowHistory.from_json("wf-sequential", json.loads(raw))
        await self.assertReplays(history)


if __name__ == "__main__":
    unittest.main()
