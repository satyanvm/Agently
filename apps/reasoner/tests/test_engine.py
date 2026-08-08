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
from dataclasses import replace

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("ANTHROPIC_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")
os.environ.setdefault("SMTP_HOST", "localhost")

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
            # Mirror the real `on conflict do nothing`: never clobber an existing row
            # (so a resume seeded via seed_status keeps its status).
            if aid not in self.agents:
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

        async def fetch_agent_statuses(run_id):
            return {aid: a.get("status", "idle") for aid, a in self.agents.items()}

        for name, fn in locals().items():
            if name != "self" and callable(fn):
                setattr(db, name, fn)

    def seed_status(self, key: str, status: str) -> None:
        """Pretend a node already finished in a prior attempt (resume tests)."""
        self.agents[f"ra_{key}"] = {"key": key, "status": status}


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

    def test_unknown_ref_fails(self):
        with self.assertRaisesRegex(ValueError, "template reference not found"):
            nodes.render("x={{outputs.nope.text}}", self._ctx())

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

    def test_branch_false_skips_descendants(self):
        # A branch whose condition is false should skip everything downstream of it,
        # while a sibling not gated by the branch still runs.
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "gate", "type": "logic.branch", "config": {"condition": "1 == 2"}, "dependsOn": ["start"]},
            {"key": "guarded", "type": "agent.llm", "config": {}, "dependsOn": ["gate"]},
            {"key": "sibling", "type": "agent.llm", "config": {}, "dependsOn": ["start"]},
        ]
        result = _run(engine.execute_graph("run_branch", g, {}))
        self.assertTrue(result["done"])
        self.assertEqual(result["skipped"], ["guarded"])
        self.assertEqual(self.fake.run_status, "succeeded")
        # progress still reaches total even though a node was skipped, not run.
        self.assertEqual(self.fake.progress, (4, 4))
        self.assertEqual(self.fake.agents["ra_guarded"]["status"], "blocked")
        self.assertEqual(self.fake.agents["ra_gate"]["status"], "succeeded")
        self.assertEqual(self.fake.agents["ra_sibling"]["status"], "succeeded")

    def test_skip_propagates_through_dag(self):
        # gate=false → g1 skipped; g2 depends only on g1 so it is skipped too;
        # the skip must propagate transitively and never block completion.
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "gate", "type": "logic.filter", "config": {"condition": "1 == 2"}, "dependsOn": ["start"]},
            {"key": "g1", "type": "agent.llm", "config": {}, "dependsOn": ["gate"]},
            {"key": "g2", "type": "output.report", "config": {"title": "Nope"}, "dependsOn": ["g1"]},
        ]
        result = _run(engine.execute_graph("run_prop", g, {}))
        self.assertTrue(result["done"])
        self.assertEqual(result["skipped"], ["g1", "g2"])
        self.assertEqual(self.fake.agents["ra_g1"]["status"], "blocked")
        self.assertEqual(self.fake.agents["ra_g2"]["status"], "blocked")
        # the skipped report never wrote its artifact.
        self.assertEqual(self.fake.artifacts, [])

    def test_branch_true_runs_descendants(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "gate", "type": "logic.branch", "config": {"condition": "1 == 1"}, "dependsOn": ["start"]},
            {"key": "guarded", "type": "agent.llm", "config": {}, "dependsOn": ["gate"]},
        ]
        result = _run(engine.execute_graph("run_true", g, {}))
        self.assertEqual(result["skipped"], [])
        self.assertEqual(self.fake.agents["ra_guarded"]["status"], "succeeded")


class NotifyFailureTest(unittest.TestCase):
    """Undeliverable output nodes must fail the run with the real reason."""

    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()

        async def fake_complete(run_id, node, system, user, *, model=None):
            return Completion(text=f"answer:{node}", tokens_in=1, tokens_out=1, cost_usd=0.0)

        nodes.llm.complete = fake_complete
        # Ensure SMTP is treated as unconfigured regardless of the ambient env.
        self._orig_config = nodes.CONFIG
        nodes.CONFIG = replace(nodes.CONFIG, smtp_host="")

    def tearDown(self):
        nodes.CONFIG = self._orig_config

    def test_email_fails_when_unconfigured(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "mail", "type": "output.email",
             "config": {"to": "a@b.com", "subject": "Hi", "body": "the digest"}, "dependsOn": ["start"]},
        ]
        result = _run(engine.execute_graph("run_mail", g, {}))
        self.assertFalse(result["done"])
        self.assertEqual(self.fake.run_status, "failed")
        self.assertEqual(result["failed_node"], "mail")
        self.assertIn("SMTP_HOST", result["error"])
        self.assertEqual(self.fake.artifacts, [])

    def test_slack_fails_when_no_webhook(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "post", "type": "output.slack",
             "config": {"message": "ping"}, "dependsOn": ["start"]},
        ]
        result = _run(engine.execute_graph("run_slack", g, {}))
        self.assertFalse(result["done"])
        self.assertEqual(result["failed_node"], "post")
        self.assertIn("webhookUrl", result["error"])
        self.assertEqual(self.fake.artifacts, [])


class PlanModuleTest(unittest.TestCase):
    """The pure plan module must be IO-free and self-consistent with engine's
    re-exports, so the Temporal workflow sandbox can import it safely."""

    def test_plan_module_has_no_io_imports(self):
        import reasoner.plan as plan_mod
        with open(plan_mod.__file__) as f:
            src = f.read()
        for banned in ("import psycopg", "import httpx", "from . import db",
                       "from . import nodes", "import playwright"):
            self.assertNotIn(banned, src, f"plan.py must not import IO dep: {banned}")

    def test_engine_reexports_plan(self):
        from reasoner import engine, plan
        # The names the workflow + engine rely on are the same objects.
        self.assertIs(engine.topo_order, plan.topo_order)
        self.assertIs(engine.build_plan, plan.build_plan)
        self.assertIs(engine.should_skip, plan.should_skip)
        self.assertIs(engine.descendants, plan.descendants)
        self.assertIs(engine.is_gate_open, plan.is_gate_open)
        self.assertIs(engine.GraphError, plan.GraphError)

    def test_build_plan_orders_and_grids(self):
        from reasoner import plan
        g = [
            {"key": "d", "type": "output.report", "dependsOn": ["b", "c"]},
            {"key": "a", "type": "trigger.manual", "dependsOn": []},
            {"key": "b", "type": "agent.llm", "dependsOn": ["a"]},
            {"key": "c", "type": "agent.llm", "dependsOn": ["a"]},
        ]
        p = plan.build_plan(g)
        self.assertEqual([n["key"] for n in p.ordered], ["a", "b", "c", "d"])
        self.assertEqual(p.total, 4)
        self.assertEqual(p.dependents["a"], ["b", "c"])
        self.assertEqual(sorted(p.dependents["b"] + p.dependents["c"]), ["d", "d"])
        self.assertEqual(p.grid["d"][0], 2)  # longest-path depth

    def test_dependents_of(self):
        from reasoner import plan
        g = [
            {"key": "a", "dependsOn": []},
            {"key": "b", "dependsOn": ["a"]},
            {"key": "c", "dependsOn": ["a"]},
        ]
        dep = plan.dependents_of(g)
        self.assertEqual(sorted(dep["a"]), ["b", "c"])
        self.assertEqual(dep["b"], [])

    def test_should_skip(self):
        from reasoner import plan
        # A node with no deps is never skipped by dep-propagation.
        self.assertFalse(plan.should_skip({"key": "a", "dependsOn": []}, set()))
        # Directly pruned.
        self.assertTrue(plan.should_skip({"key": "a", "dependsOn": []}, {"a"}))
        # All deps skipped → skip.
        self.assertTrue(plan.should_skip({"key": "c", "dependsOn": ["a", "b"]}, {"a", "b"}))
        # Some deps live → run.
        self.assertFalse(plan.should_skip({"key": "c", "dependsOn": ["a", "b"]}, {"a"}))

    def test_is_gate_open(self):
        from reasoner import plan
        gate = {"type": "logic.branch"}
        self.assertTrue(plan.is_gate_open(gate, {"passed": True}))
        self.assertFalse(plan.is_gate_open(gate, {"passed": False}))
        # Non-gate node is always open regardless of output.
        self.assertTrue(plan.is_gate_open({"type": "agent.llm"}, {"passed": False}))


class ResumeFallbackTest(unittest.TestCase):
    """execute_graph must not re-run nodes already marked succeeded (resume)."""

    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()
        self.calls: list[str] = []

        async def counting_complete(run_id, node, system, user, *, model=None):
            self.calls.append(node)
            return Completion(text=f"answer:{node}", tokens_in=1, tokens_out=1, cost_usd=0.0)

        nodes.llm.complete = counting_complete

    def test_succeeded_node_not_rerun(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "a", "type": "agent.llm", "config": {"prompt": "x"}, "dependsOn": ["start"]},
            {"key": "b", "type": "agent.llm", "config": {"prompt": "y"}, "dependsOn": ["a"]},
        ]
        # Pretend 'a' already finished in a crashed attempt.
        self.fake.seed_status("a", "succeeded")
        result = _run(engine.execute_graph("run_resume", g, {}))
        self.assertTrue(result["done"])
        # 'a' must NOT have been re-run; only 'b' calls the LLM.
        self.assertEqual(self.calls, ["b"])
        self.assertEqual(self.fake.run_status, "succeeded")

    def test_failed_node_is_reattempted(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "a", "type": "agent.llm", "config": {"prompt": "x"}, "dependsOn": ["start"]},
        ]
        # A prior FAILED row must be retried, not treated as done.
        self.fake.seed_status("a", "failed")
        result = _run(engine.execute_graph("run_retry", g, {}))
        self.assertTrue(result["done"])
        self.assertEqual(self.calls, ["a"])


def _simulate_per_node(fake, run_id, graph, run_input):
    """Deterministically re-enact reasoner.workflow.DynamicWorkflow WITHOUT Temporal.

    Mirrors the workflow's loop exactly (same plan/should_skip/is_gate_open/descendants
    helpers, same run_node/skip_node/finish flow) so we can unit-test the orchestration
    logic — ordering, resume-skipping of succeeded nodes, gate skip-propagation — with
    the FakeDB. A live Temporal replay does the same thing via event history; here we
    just call the shared pieces synchronously.
    """
    from reasoner import plan as plan_mod

    # === activities.load_graph ===
    p = plan_mod.build_plan(graph)
    agent_ids = _run(engine.seed_agents(run_id, p))
    prior = _run(db.fetch_agent_statuses(run_id))
    ordered = p.ordered
    dependents = plan_mod.dependents_of(ordered)

    outputs: dict = {}
    skipped: set = set()
    order_run: list = []
    for node in ordered:
        key = node["key"]
        agent_id = agent_ids[key]
        deps = list(node.get("dependsOn") or [])
        if plan_mod.should_skip(node, skipped):
            skipped.add(key)
            skipped.update(plan_mod.descendants(key, dependents))
            _run(engine.mark_skipped(run_id, agent_id, key))
            outputs[key] = {"skipped": True}
            continue
        if prior.get(agent_id) == "succeeded":
            outputs[key] = {}
            continue
        upstream = {d: outputs.get(d, {}) for d in deps}
        # === activities.run_node ===
        try:
            res = _run(engine.execute_one_node(run_id, node, agent_id, upstream, run_input))
        except Exception as exc:  # noqa: BLE001
            _run(db.finish_run(run_id, "failed", "Failed", error=str(exc)))
            return {"done": False, "failed_node": key}
        order_run.append(key)
        outputs[key] = res.output or {}
        if not plan_mod.is_gate_open(node, outputs[key]):
            skipped.update(plan_mod.descendants(key, dependents))
    _run(db.finish_run(run_id, "succeeded", "Completed"))
    return {"done": True, "skipped": sorted(skipped), "ran": order_run}


class PerNodeOrchestrationTest(unittest.TestCase):
    """Exercise the DynamicWorkflow orchestration logic deterministically (no
    Temporal): the same ordering/skip/resume semantics the workflow drives."""

    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()

        async def fake_complete(run_id, node, system, user, *, model=None):
            return Completion(text=f"answer:{node}", tokens_in=1, tokens_out=1, cost_usd=0.0)

        nodes.llm.complete = fake_complete

    def test_runs_in_topo_order(self):
        g = [
            {"key": "b", "type": "agent.llm", "config": {"prompt": "x"}, "dependsOn": ["a"]},
            {"key": "a", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "c", "type": "output.report", "config": {}, "dependsOn": ["b"]},
        ]
        res = _simulate_per_node(self.fake, "run_topo", g, {})
        self.assertTrue(res["done"])
        self.assertEqual(res["ran"], ["a", "b", "c"])
        self.assertEqual(self.fake.run_status, "succeeded")

    def test_gate_false_skips_descendants(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "gate", "type": "logic.branch", "config": {"condition": "1 == 2"}, "dependsOn": ["start"]},
            {"key": "guarded", "type": "agent.llm", "config": {}, "dependsOn": ["gate"]},
            {"key": "deep", "type": "output.report", "config": {}, "dependsOn": ["guarded"]},
            {"key": "sibling", "type": "agent.llm", "config": {}, "dependsOn": ["start"]},
        ]
        res = _simulate_per_node(self.fake, "run_gate", g, {})
        self.assertTrue(res["done"])
        # skip propagates transitively past the immediate child.
        self.assertEqual(res["skipped"], ["deep", "guarded"])
        self.assertIn("sibling", res["ran"])  # ungated sibling still runs
        self.assertNotIn("guarded", res["ran"])
        self.assertEqual(self.fake.agents["ra_guarded"]["status"], "blocked")

    def test_resume_skips_succeeded_node(self):
        g = [
            {"key": "start", "type": "trigger.manual", "config": {}, "dependsOn": []},
            {"key": "a", "type": "agent.llm", "config": {}, "dependsOn": ["start"]},
            {"key": "b", "type": "agent.llm", "config": {}, "dependsOn": ["a"]},
        ]
        # 'start' and 'a' already succeeded before a crash; resume must not re-run them.
        self.fake.seed_status("start", "succeeded")
        self.fake.seed_status("a", "succeeded")
        res = _simulate_per_node(self.fake, "run_resume2", g, {})
        self.assertTrue(res["done"])
        self.assertEqual(res["ran"], ["b"])  # only the in-flight/pending node runs


if __name__ == "__main__":
    unittest.main()
