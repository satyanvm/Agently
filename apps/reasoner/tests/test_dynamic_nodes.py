"""Tests for the intelligent-DAG additions: catalog integration executor,
templating helpers, sandboxed tool.code, tool.db gating, and loop fan-out.

Runnable without Postgres/Temporal/network — same FakeDB discipline as
test_engine.py; httpx and the sandbox gate are patched per-test.
"""
from __future__ import annotations

import asyncio
import json
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

from reasoner import catalog, db, engine, nodes, plan, sandbox  # noqa: E402
from reasoner.config import CONFIG  # noqa: E402
from reasoner.nodes import NodeContext  # noqa: E402

from test_engine import FakeDB  # noqa: E402 — reuse the in-memory db fake


def ctx_for(node: dict, upstream: dict | None = None, run_input: dict | None = None,
            extra: dict | None = None) -> NodeContext:
    return NodeContext(
        run_id="run_1", agent_id="ra_x", node=node,
        upstream=upstream or {}, run_input=run_input or {}, extra=extra or {},
    )


class RenderTplTest(unittest.TestCase):
    def test_json_and_urlencode_helpers(self):
        ctx = ctx_for({"key": "n", "config": {}}, run_input={"topic": 'he said "hi" & left'})
        roots = {"config": {"channel": "#general"}, "credentials": {"TOK": "s3cr3t"}}
        self.assertEqual(
            nodes.render_tpl('{{json config.channel}}', ctx, roots), '"#general"')
        self.assertEqual(
            nodes.render_tpl('{{urlencode input.topic}}', ctx, roots),
            "he%20said%20%22hi%22%20%26%20left")
        self.assertEqual(
            nodes.render_tpl("Bearer {{credentials.TOK}}", ctx, roots), "Bearer s3cr3t")
        with self.assertRaisesRegex(ValueError, "template reference not found"):
            nodes.render_tpl("x={{config.nope}}", ctx, roots)
        with self.assertRaisesRegex(ValueError, "template reference not found"):
            nodes.render_tpl("{{json config.nope}}", ctx, roots)

    def test_extra_roots_reach_render(self):
        ctx = ctx_for({"key": "n", "config": {}}, extra={"item": {"title": "T"},
                                                         "loop": {"l": {"index": 3}}})
        self.assertEqual(nodes.render("i={{item.title}} n={{loop.l.index}}", ctx), "i=T n=3")


class CatalogTest(unittest.TestCase):
    def test_catalog_loads_and_routes_handlers(self):
        self.assertGreater(catalog.size(), 100, "generated catalog should be loaded")
        self.assertIsNotNone(catalog.spec_for("slack.webhookPost"))
        # Builtins stay code-backed; catalog types get the generic executor;
        # unknown types fail explicitly.
        self.assertIs(nodes.handler_for("agent.llm"), nodes._agent)
        self.assertIs(nodes.handler_for("slack.webhookPost"), nodes._integration)
        self.assertIs(nodes.handler_for("totally.unknown"), nodes._unsupported)


class IntegrationHandlerTest(unittest.TestCase):
    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()

    def test_missing_credentials_fail(self):
        node = {"key": "s", "type": "slack.sendMessage",
                "config": {"channel": "#g", "text": "hi"}}
        os.environ.pop("SLACK_BOT_TOKEN", None)
        with self.assertRaisesRegex(RuntimeError, "SLACK_BOT_TOKEN"):
            asyncio.run(nodes._integration(ctx_for(node)))

    def test_http_runtime_renders_and_lifts_outputs(self):
        sent = {}

        class FakeResp:
            status_code = 200
            text = '{"ok": true, "ts": "123.45"}'

            def json(self):
                return json.loads(self.text)

        class FakeClient:
            def __init__(self, **kw):
                pass

            async def __aenter__(self):
                return self

            async def __aexit__(self, *a):
                return False

            async def request(self, method, url, headers=None, content=None, auth=None):
                sent.update(method=method, url=url, headers=headers or {}, content=content)
                return FakeResp()

        orig = nodes.httpx.AsyncClient
        nodes.httpx.AsyncClient = FakeClient
        try:
            node = {"key": "s", "type": "slack.webhookPost",
                    "config": {"webhookUrl": "https://hooks.slack.com/services/X",
                               "text": "deploy done"}}
            res = asyncio.run(nodes._integration(ctx_for(node)))
        finally:
            nodes.httpx.AsyncClient = orig

        self.assertEqual(sent["method"], "POST")
        self.assertEqual(sent["url"], "https://hooks.slack.com/services/X")
        self.assertEqual(json.loads(sent["content"]), {"text": "deploy done"})
        self.assertEqual(res.output["status"], 200)


class SandboxTest(unittest.TestCase):
    def test_python_happy_path_and_stdin_payload(self):
        src = ("import sys, json\n"
               "p = json.load(sys.stdin)\n"
               "print(json.dumps({'echo': p['config']['x'], 'n': 2 + 2}))\n")
        res = asyncio.run(sandbox.run("python", src, {"config": {"x": "hello"}}))
        self.assertTrue(res.ok, res.error)
        self.assertEqual(res.result, {"echo": "hello", "n": 4})

    def test_environment_is_stripped(self):
        src = ("import os, json\n"
               "print(json.dumps({'db': os.environ.get('DATABASE_URL')}))\n")
        res = asyncio.run(sandbox.run("python", src, {}))
        self.assertTrue(res.ok, res.error)
        self.assertIsNone(res.result["db"], "secrets must not leak into the sandbox")

    def test_failure_is_reported_not_raised(self):
        res = asyncio.run(sandbox.run("python", "raise RuntimeError('boom')", {}))
        self.assertFalse(res.ok)
        self.assertIn("boom", res.error)


class ToolCodeGateTest(unittest.TestCase):
    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()
        self._orig = nodes.CONFIG

    def tearDown(self):
        nodes.CONFIG = self._orig

    def test_gated_off_fails(self):
        nodes.CONFIG = replace(CONFIG, tool_code_enabled=False)
        node = {"key": "c", "type": "tool.code",
                "config": {"language": "python", "source": "print('x')"}}
        with self.assertRaisesRegex(RuntimeError, "TOOL_CODE_ENABLED"):
            asyncio.run(nodes._code(ctx_for(node)))

    def test_enabled_executes(self):
        nodes.CONFIG = replace(CONFIG, tool_code_enabled=True)
        node = {"key": "c", "type": "tool.code",
                "config": {"language": "python",
                           "source": "import json;print(json.dumps({'v': 7}))"}}
        res = asyncio.run(nodes._code(ctx_for(node)))
        self.assertTrue(res.output["executed"])
        self.assertEqual(res.output["result"], {"v": 7})


class ToolDbGateTest(unittest.TestCase):
    def setUp(self):
        FakeDB().install()
        self._orig = nodes.CONFIG

    def tearDown(self):
        nodes.CONFIG = self._orig

    def test_no_url_fails(self):
        nodes.CONFIG = replace(CONFIG, tool_db_url="")
        node = {"key": "q", "type": "tool.db", "config": {"query": "select 1"}}
        with self.assertRaisesRegex(RuntimeError, "TOOL_DB_URL"):
            asyncio.run(nodes._db_query(ctx_for(node)))


class LoopBodyTest(unittest.TestCase):
    def test_dominated_subgraph_only(self):
        graph = [
            {"key": "t", "type": "trigger.manual"},
            {"key": "fetch", "dependsOn": ["t"]},
            {"key": "loop", "type": "logic.loop", "dependsOn": ["fetch"]},
            {"key": "per_item", "dependsOn": ["loop"]},
            {"key": "per_item2", "dependsOn": ["per_item"]},
            # join depends on the loop's body AND on fetch — NOT dominated.
            {"key": "join", "dependsOn": ["per_item2", "fetch"]},
        ]
        ordered = plan.topo_order(graph)
        deps = plan.dependents_of(ordered)
        self.assertEqual(plan.loop_body("loop", ordered, deps), ["per_item", "per_item2"])


class LoopFanOutTest(unittest.TestCase):
    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()
        # A test-only node type that records each execution and echoes {{item}}.
        self.echoed: list[str] = []

        @nodes.handles("test.echo")
        async def _echo(ctx: NodeContext):  # noqa: ANN202
            text = nodes.render("{{item.name}}#{{loop.l.index}}", ctx)
            self.echoed.append(text)
            return nodes.NodeResult(output={"text": text}, summary=text)

    def tearDown(self):
        nodes._REGISTRY.pop("test.echo", None)

    def test_execute_graph_fans_out_per_item(self):
        graph = [
            {"key": "t", "type": "trigger.manual", "config": {}},
            {"key": "l", "type": "logic.loop", "dependsOn": ["t"],
             "config": {"items": "input.things"}},
            {"key": "echo", "type": "test.echo", "dependsOn": ["l"], "config": {}},
        ]
        run_input = {"things": [{"name": "a"}, {"name": "b"}, {"name": "c"}]}
        out = asyncio.run(engine.execute_graph("run_1", graph, run_input))
        self.assertTrue(out["done"], out)
        self.assertEqual(self.echoed, ["a#0", "b#1", "c#2"])
        results = out["outputs"]["l"]["results"]
        self.assertEqual([r["text"] for r in results], ["a#0", "b#1", "c#2"])

    def test_zero_items_skips_body(self):
        graph = [
            {"key": "t", "type": "trigger.manual", "config": {}},
            {"key": "l", "type": "logic.loop", "dependsOn": ["t"],
             "config": {"items": "input.things"}},
            {"key": "echo", "type": "test.echo", "dependsOn": ["l"], "config": {}},
        ]
        out = asyncio.run(engine.execute_graph("run_1", graph, {"things": []}))
        self.assertTrue(out["done"], out)
        self.assertEqual(self.echoed, [])
        self.assertEqual(self.fake.agents["ra_echo"]["status"], plan._SKIPPED_STATUS)


if __name__ == "__main__":
    unittest.main()
