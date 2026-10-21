"""Tests for competitor watching: page normalization, diffing, fetching
(against a local HTTP server, no internet), and the intel.* nodes end to end
against a real Postgres test database when AGENTLY_TEST_DATABASE_URL is set.
"""
from __future__ import annotations

import asyncio
import os
import threading
import unittest
from dataclasses import replace
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("GEMINI_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")

from reasoner import db, intel, nodes  # noqa: E402
from reasoner.config import CONFIG  # noqa: E402
from reasoner.nodes import NodeContext  # noqa: E402

PRICING_V1 = """<html><head><title>Acme Pricing</title><style>.x{color:red}</style>
<script>var t = Date.now();</script></head><body>
<nav>Home | Pricing</nav>
<h1>Plans</h1>
<div class="plan"><h2>Starter</h2><p>$10 / month</p><ul><li>3 seats</li><li>Email support</li></ul></div>
<div class="plan"><h2>Team</h2><p>$40 / month</p></div>
<p>Updated 5 minutes ago</p>
<footer>© 2024-2026 Acme Inc. All rights reserved.</footer>
</body></html>"""

PRICING_V2 = PRICING_V1.replace("$10 / month", "$12 / month").replace(
    "<li>Email support</li>", "<li>Email support</li><li>AI assistant</li>"
).replace("Updated 5 minutes ago", "Updated 2 hours ago")


class NormalizeTest(unittest.TestCase):
    def test_visible_text_only_with_noise_removed(self):
        title, text = intel.html_to_text(PRICING_V1)
        norm = intel.normalize_text(text)
        self.assertEqual(title, "Acme Pricing")
        lines = norm.splitlines()
        self.assertIn("$10 / month", lines)          # prices are the signal: kept
        self.assertIn("Starter", lines)
        self.assertNotIn("Date.now", norm)            # script dropped
        self.assertNotIn("color:red", norm)           # style dropped
        self.assertFalse(any("ago" in l for l in lines))       # relative time dropped
        self.assertFalse(any(l.startswith("©") for l in lines))  # copyright year dropped

    def test_timestamp_only_change_is_not_a_change(self):
        a = intel.normalize_text(intel.html_to_text(PRICING_V1)[1])
        b = intel.normalize_text(intel.html_to_text(PRICING_V1.replace("5 minutes", "1 hour"))[1])
        self.assertEqual(intel.content_hash(a), intel.content_hash(b))

    def test_ignore_patterns(self):
        text = "Visitors today: 4,211\nStarter $10"
        self.assertEqual(intel.normalize_text(text, [r"^Visitors today"]), "Starter $10")

    def test_diff_reports_added_and_removed_lines(self):
        a = intel.normalize_text(intel.html_to_text(PRICING_V1)[1])
        b = intel.normalize_text(intel.html_to_text(PRICING_V2)[1])
        d = intel.diff_text(a, b)
        self.assertTrue(d.changed)
        self.assertEqual(sorted(d.added), ["$12 / month", "AI assistant"])
        self.assertEqual(d.removed, ["$10 / month"])
        self.assertEqual(d.summary(), "2 lines added, 1 line removed")
        self.assertFalse(intel.diff_text(a, a).changed)
        self.assertEqual(intel.diff_text(a, a).summary(), "No change")


class _Site:
    """A local website whose pages and robots.txt a test can change."""

    def __init__(self) -> None:
        self.pages: dict[str, tuple[int, str, str]] = {}  # path → (status, content-type, body)
        self.robots = "User-agent: *\nDisallow: /private\n"
        site = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):  # noqa: N802
                if self.path == "/robots.txt":
                    status, ctype, body = (200, "text/plain", site.robots) if site.robots is not None else (404, "text/plain", "")
                else:
                    status, ctype, body = site.pages.get(self.path, (404, "text/html", "<h1>Not found</h1>"))
                data = body.encode()
                self.send_response(status)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, *args):  # silence
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()


class FetchTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.site = _Site()
        cls.site.pages["/pricing"] = (200, "text/html; charset=utf-8", PRICING_V1)
        cls.site.pages["/private/plan"] = (200, "text/html", "<p>secret</p>")
        cls.site.pages["/logo.png"] = (200, "image/png", "PNG")
        cls.site.pages["/down"] = (503, "text/html", "<p>maintenance</p>")

    @classmethod
    def tearDownClass(cls):
        cls.site.close()

    def fetch(self, path: str) -> intel.Fetched:
        return asyncio.run(intel.fetch_http(self.site.base + path, "AgentlyTest/1.0"))

    def test_fetches_and_normalizes(self):
        got = self.fetch("/pricing")
        self.assertEqual(got.status, 200)
        self.assertEqual(got.title, "Acme Pricing")
        self.assertIn("$10 / month", got.text)

    def test_robots_disallow_fails_with_reason(self):
        with self.assertRaisesRegex(intel.FetchError, "robots.txt disallows"):
            self.fetch("/private/plan")

    def test_missing_robots_allows(self):
        self.site.robots = None
        try:
            self.assertIn("secret", self.fetch("/private/plan").text)
        finally:
            self.site.robots = "User-agent: *\nDisallow: /private\n"

    def test_http_errors_and_non_pages_fail(self):
        with self.assertRaisesRegex(intel.FetchError, "returned 404"):
            self.fetch("/nope")
        with self.assertRaisesRegex(intel.FetchError, "returned 503"):
            self.fetch("/down")
        with self.assertRaisesRegex(intel.FetchError, "image/png, not a web page"):
            self.fetch("/logo.png")


TEST_DB = os.environ.get("AGENTLY_TEST_DATABASE_URL", "")


@unittest.skipUnless(TEST_DB, "AGENTLY_TEST_DATABASE_URL not set")
class IntelNodesPostgresTest(unittest.TestCase):
    """The nodes against real tables: baseline → unchanged → changed → signal."""

    def setUp(self):
        self.site = _Site()
        self.site.pages["/pricing"] = (200, "text/html", PRICING_V1)
        self.site.pages["/careers"] = (200, "text/html", "<h1>Jobs</h1><p>Engineer</p>")
        self.patch = mock.patch.object(db, "CONFIG", replace(CONFIG, database_url=TEST_DB))
        self.patch.start()
        self.ws, self.wf = db.gen_id("ws"), db.gen_id("wf")
        self.runs = [db.gen_id("run") for _ in range(4)]
        asyncio.run(self._seed())

    def tearDown(self):
        asyncio.run(self._sql("delete from workspaces where id = %s", (self.ws,)))
        self.patch.stop()
        self.site.close()

    async def _sql(self, q: str, args: tuple = ()):
        async with db._conn() as conn:
            cur = await conn.execute(q, args)
            return await cur.fetchall() if cur.description else None

    async def _seed(self):
        await self._sql("insert into workspaces (id, name, slug) values (%s, 't', %s)", (self.ws, self.ws))
        await self._sql("insert into workflows (id, workspace_id, slug, name) values (%s, %s, 'w', 'w')", (self.wf, self.ws))
        for i, r in enumerate(self.runs):
            await self._sql(
                "insert into runs (id, workspace_id, workflow_id, number, triggered_by, region) values (%s, %s, %s, %s, '{}', 'local')",
                (r, self.ws, self.wf, i + 1),
            )
        pages = (
            '[{"kind": "pricing", "url": "%s/pricing", "render": "http"},'
            ' {"kind": "careers", "url": "%s/careers", "render": "http"},'
            ' {"kind": "blog", "url": "%s/gone", "render": "http"}]'
        ) % (self.site.base, self.site.base, self.site.base)
        await self._sql(
            "insert into competitors (id, workspace_id, name, pages) values (%s, %s, 'Acme', %s::jsonb)",
            (db.gen_id("cmp"), self.ws, pages),
        )

    def watch(self, run_id: str, **cfg) -> nodes.NodeResult:
        ctx = NodeContext(run_id=run_id, agent_id="ra_x", upstream={}, run_input={},
                          node={"key": "watch", "type": "intel.watchCompetitors", "config": cfg})
        return asyncio.run(nodes.handler_for("intel.watchCompetitors")(ctx))

    def test_watch_lifecycle(self):
        first = self.watch(self.runs[0])
        self.assertFalse(first.output["changed"])
        self.assertEqual(first.output["checked"], 3)
        self.assertEqual(first.output["baselines"], 2)
        # The dead blog page is reported, not hidden, and doesn't fail the node.
        self.assertEqual([f["pageKind"] for f in first.output["failed"]], ["blog"])
        self.assertIn("returned 404", first.output["failed"][0]["error"])

        again = self.watch(self.runs[1])
        self.assertFalse(again.output["changed"])
        self.assertEqual(again.output["baselines"], 0)
        rows = asyncio.run(self._sql("select count(*) from page_snapshots where workspace_id = %s", (self.ws,)))
        self.assertEqual(rows[0][0], 2, "an unchanged page must not store a new version")

        self.site.pages["/pricing"] = (200, "text/html", PRICING_V2)
        changed = self.watch(self.runs[2])
        self.assertTrue(changed.output["changed"])
        [change] = changed.output["changes"]
        self.assertEqual(change["competitor"], "Acme")
        self.assertEqual(change["pageKind"], "pricing")
        self.assertEqual(change["removed"], ["$10 / month"])
        self.assertIn("+ $12 / month", changed.output["text"])
        self.assertTrue(change["signalId"].startswith("sig_"))
        sig = asyncio.run(self._sql(
            "select source, title, run_id, snapshot_id, previous_snapshot_id from signals where id = %s",
            (change["signalId"],)))[0]
        self.assertEqual(sig[0], "page_change")
        self.assertEqual(sig[1], "Acme pricing page changed")
        self.assertEqual(sig[2], self.runs[2])
        self.assertEqual(sig[3], change["snapshotId"])
        self.assertEqual(sig[4], change["previousSnapshotId"])

        # Next run: the change was already reported, so it's not reported again.
        after = self.watch(self.runs[3], pageKinds="pricing")
        self.assertFalse(after.output["changed"])
        self.assertEqual(after.output["checked"], 1)

    def test_page_kinds_as_a_list_or_a_string(self):
        # The planner writes JSON lists; the builder field holds "a, b".
        as_list = self.watch(self.runs[0], pageKinds=["pricing"], competitors=["Acme"])
        self.assertEqual(as_list.output["checked"], 1)
        as_text = self.watch(self.runs[1], pageKinds="pricing, careers")
        self.assertEqual(as_text.output["checked"], 2)

    def test_no_competitors_or_all_failed_raise(self):
        asyncio.run(self._sql("update competitors set pages = %s::jsonb where workspace_id = %s",
                              (f'[{{"kind": "blog", "url": "{self.site.base}/gone"}}]', self.ws)))
        with self.assertRaisesRegex(RuntimeError, "every page failed"):
            self.watch(self.runs[0])
        with self.assertRaisesRegex(ValueError, "no competitor named 'Globex'"):
            self.watch(self.runs[0], competitors="Globex")

    def test_snapshot_then_diff_nodes(self):
        url = self.site.base + "/pricing"

        def run(run_id: str, node_type: str, cfg: dict, upstream: dict | None = None) -> dict:
            ctx = NodeContext(run_id=run_id, agent_id="ra_x", upstream=upstream or {}, run_input={},
                              node={"key": node_type.split(".")[1], "type": node_type, "config": cfg})
            return asyncio.run(nodes.handler_for(node_type)(ctx)).output

        snap = run(self.runs[0], "intel.snapshotPage", {"url": url, "competitor": "acme", "pageKind": "pricing"})
        self.assertTrue(snap["first"])
        diff = run(self.runs[0], "intel.diffPage", {"snapshotId": "{{outputs.snap.snapshotId}}"}, {"snap": snap})
        self.assertTrue(diff["first"])
        self.assertFalse(diff["changed"])

        self.site.pages["/pricing"] = (200, "text/html", PRICING_V2)
        snap2 = run(self.runs[1], "intel.snapshotPage", {"url": url})
        self.assertTrue(snap2["newVersion"])
        diff2 = run(self.runs[1], "intel.diffPage", {"url": url})
        self.assertTrue(diff2["changed"])
        self.assertEqual(diff2["previousSnapshotId"], snap["snapshotId"])
        # Diffing the same version twice records one signal.
        diff3 = run(self.runs[1], "intel.diffPage", {"url": url})
        self.assertEqual(diff3["signalId"], diff2["signalId"])

    def test_record_signal_items_dedupe(self):
        articles = [{"title": "Acme raises $20M", "url": "https://news.test/a"},
                    {"title": "Acme launches AI", "url": "https://news.test/b"}]

        def record(run_id: str) -> dict:
            ctx = NodeContext(run_id=run_id, agent_id="ra_x", run_input={},
                              upstream={"news": {"articles": articles}},
                              node={"key": "rec", "type": "intel.recordSignal", "config": {
                                  "source": "news", "items": "outputs.news.articles",
                                  "titleField": "title", "urlField": "url", "competitor": "Acme"}})
            return asyncio.run(nodes.handler_for("intel.recordSignal")(ctx)).output

        first = record(self.runs[0])
        self.assertEqual((first["count"], first["created"]), (2, 2))
        second = record(self.runs[1])
        self.assertEqual((second["created"], second["duplicates"]), (0, 2))
        self.assertEqual(sorted(second["signalIds"]), sorted(first["signalIds"]))


if __name__ == "__main__":
    unittest.main()
