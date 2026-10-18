"""Brand context in agent.llm (config.brand: true): what reaches the model,
the empty-brand failure, and the real brand_context row via Postgres when
AGENTLY_TEST_DATABASE_URL is set.
"""
from __future__ import annotations

import asyncio
import datetime as dt
import os
import unittest
from dataclasses import replace
from unittest import mock

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("GEMINI_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")

from reasoner import db, llm, nodes  # noqa: E402
from reasoner.config import CONFIG  # noqa: E402
from reasoner.nodes import NodeContext  # noqa: E402

BRAND = {
    "voice": "Plain and direct. No exclamation marks.",
    "product_facts": "Starter is $12/seat/month.",
    "positioning": "",
    "avoid_claims": "Never claim to be the cheapest.",
    "updated_at": dt.datetime(2026, 10, 18, 9, 0, tzinfo=dt.timezone.utc),
    "updated_by": "Satya",
}


def ctx(brand=None, run_id="run_1") -> NodeContext:
    cfg = {"system": "You write LinkedIn posts.", "prompt": "Write a post about our pricing."}
    if brand is not None:
        cfg["brand"] = brand
    return NodeContext(run_id=run_id, agent_id="ra_x", upstream={}, run_input={},
                       node={"key": "write", "type": "agent.llm", "config": cfg})


class BrandPromptTest(unittest.TestCase):
    def setUp(self):
        self.calls: list[dict] = []
        self.logs: list[str] = []

        async def fake_complete(run_id, node, system, user, *, model=None):
            self.calls.append({"system": system, "user": user})
            return llm.Completion(text="draft", tokens_in=1, tokens_out=1, cost_usd=0.0)

        async def append_log(run_id, level, channel, source, message, **kw):
            self.logs.append(message)

        self.brand_row = dict(BRAND)
        self.patches = [
            mock.patch.object(nodes.llm, "complete", fake_complete),
            mock.patch.object(nodes.db, "run_workspace_id", mock.AsyncMock(return_value="ws_1")),
            mock.patch.object(nodes.db, "brand_context", mock.AsyncMock(side_effect=lambda ws: self.brand_row)),
            mock.patch.object(nodes.db, "append_log", append_log),
        ]
        for p in self.patches:
            p.start()

    def tearDown(self):
        for p in self.patches:
            p.stop()

    def run_agent(self, c: NodeContext):
        return asyncio.run(nodes.handler_for("agent.llm")(c))

    def test_brand_true_adds_every_filled_section(self):
        for flag in (True, "true", "True"):
            self.calls.clear()
            res = self.run_agent(ctx(flag))
            system = self.calls[0]["system"]
            self.assertTrue(system.startswith("You write LinkedIn posts."))
            self.assertIn("## Brand context", system)
            self.assertIn("Plain and direct. No exclamation marks.", system)
            self.assertIn("the only product claims you may make\nStarter is $12/seat/month.", system)
            self.assertIn("Never say — claims to avoid\nNever claim to be the cheapest.", system)
            self.assertNotIn("Positioning", system, "empty sections are left out")
            self.assertEqual(res.output["brandContextUpdatedAt"], "2026-10-18T09:00:00+00:00")
        self.assertIn("Drafted with brand context saved 2026-10-18T09:00:00+00:00 by Satya", self.logs)

    def test_brand_off_or_absent_leaves_prompt_alone(self):
        for flag in (None, False, "false", ""):
            self.calls.clear()
            res = self.run_agent(ctx(flag))
            self.assertEqual(self.calls[0]["system"], "You write LinkedIn posts.")
            self.assertNotIn("brandContextUpdatedAt", res.output)
        nodes.db.brand_context.assert_not_awaited()

    def test_empty_brand_fails_instead_of_drafting_generic_text(self):
        for row in (None, {k: "" for k in ("voice", "product_facts", "positioning", "avoid_claims")}):
            self.brand_row = row
            with self.assertRaisesRegex(RuntimeError, "the Brand page is empty"):
                self.run_agent(ctx(True))
        self.assertEqual(self.calls, [], "no model call without the brand context")


TEST_DB = os.environ.get("AGENTLY_TEST_DATABASE_URL", "")


@unittest.skipUnless(TEST_DB, "AGENTLY_TEST_DATABASE_URL not set")
class BrandContextDbTest(unittest.TestCase):
    def setUp(self):
        self.patch = mock.patch.object(db, "CONFIG", replace(CONFIG, database_url=TEST_DB))
        self.patch.start()
        self.ws = db.gen_id("ws")

    def tearDown(self):
        asyncio.run(self._sql("delete from workspaces where id = %s", (self.ws,)))
        self.patch.stop()

    async def _sql(self, q, args=()):
        async with db._conn() as conn:
            await conn.execute(q, args)

    def test_reads_the_saved_row(self):
        asyncio.run(self._sql("insert into workspaces (id, name, slug) values (%s, 't', %s)", (self.ws, self.ws)))
        self.assertIsNone(asyncio.run(db.brand_context(self.ws)))
        asyncio.run(self._sql(
            "insert into brand_context (workspace_id, voice, avoid_claims, updated_by) values (%s, 'Plain.', 'No hype.', 'Satya')",
            (self.ws,)))
        row = asyncio.run(db.brand_context(self.ws))
        self.assertEqual((row["voice"], row["avoid_claims"], row["product_facts"], row["updated_by"]),
                         ("Plain.", "No hype.", "", "Satya"))
        self.assertIn("No hype.", nodes.brand_block(row))


if __name__ == "__main__":
    unittest.main()
