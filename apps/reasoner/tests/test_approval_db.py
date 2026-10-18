"""The approval activities and the decision relay against a real Postgres
test database (AGENTLY_TEST_DATABASE_URL) and a local Slack-webhook stand-in.

Covers: open is idempotent and notifies once (with the Inbox link); a failed
notification is recorded, not fatal; a decision made in Postgres (as the API
does) is relayed as a signal exactly once; resolve reads the decision back;
a timeout that races a last-moment decision keeps the decision.
"""
from __future__ import annotations

import asyncio
import json
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
os.environ.setdefault("SMTP_HOST", "localhost")

from temporalio.service import RPCError, RPCStatusCode  # noqa: E402

from reasoner import approval, db, dispatcher  # noqa: E402
from reasoner.config import CONFIG  # noqa: E402

TEST_DB = os.environ.get("AGENTLY_TEST_DATABASE_URL", "")


class _Webhook:
    def __init__(self, status: int = 200) -> None:
        self.posts: list[dict] = []
        self.status = status
        hook = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):  # noqa: N802
                body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                hook.posts.append(json.loads(body or b"{}"))
                self.send_response(hook.status)
                self.end_headers()

            def log_message(self, *args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}/hook"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()


class _FakeHandle:
    def __init__(self, client, wf_id):
        self.client, self.wf_id = client, wf_id

    async def signal(self, sig, payload):
        if self.wf_id in self.client.gone:
            raise RPCError("workflow execution already completed", RPCStatusCode.NOT_FOUND, b"")
        self.client.signals.append((self.wf_id, payload))


class _FakeClient:
    def __init__(self):
        self.signals: list[tuple[str, dict]] = []
        self.gone: set[str] = set()

    def get_workflow_handle(self, wf_id):
        return _FakeHandle(self, wf_id)


@unittest.skipUnless(TEST_DB, "AGENTLY_TEST_DATABASE_URL not set")
class ApprovalDbTest(unittest.TestCase):
    def setUp(self):
        self.hook = _Webhook()
        cfg = replace(CONFIG, database_url=TEST_DB, app_base_url="https://agently.test", approval_slack_webhook="")
        self.patches = [mock.patch.object(m, "CONFIG", cfg) for m in (db, approval, dispatcher)]
        for p in self.patches:
            p.start()
        self.ws, self.wf = db.gen_id("ws"), db.gen_id("wf")
        self.run = db.gen_id("run")
        asyncio.run(self._seed())

    def tearDown(self):
        asyncio.run(self._sql("delete from workspaces where id = %s", (self.ws,)))
        for p in self.patches:
            p.stop()
        self.hook.close()

    async def _sql(self, q: str, args: tuple = ()):
        async with db._conn() as conn:
            cur = await conn.execute(q, args)
            return await cur.fetchall() if cur.description else None

    async def _seed(self):
        await self._sql("insert into workspaces (id, name, slug) values (%s, 't', %s)", (self.ws, self.ws))
        await self._sql("insert into workflows (id, workspace_id, slug, name) values (%s, %s, 'w', 'w')", (self.wf, self.ws))
        await self._sql("insert into runs (id, workspace_id, workflow_id, number, triggered_by, region, status) "
                        "values (%s, %s, %s, 1, '{}', 'local', 'running')", (self.run, self.ws, self.wf))
        await self._sql("insert into run_agents (id, run_id, name, role, model) values ('ra_ok', %s, 'ok', 'validator', 'none')",
                        (self.run,))
        self.signal_id, _ = await db.insert_signal(self.ws, source="page_change", title="Acme pricing changed", run_id=self.run)

    def open(self, **config) -> approval.OpenApprovalResult:
        node = {"key": "ok", "type": "human.approval",
                "config": {"draft": "Post: {{outputs.write.text}}", "title": "LinkedIn post", "assignee": "Rahul",
                           "channel": "LinkedIn", "notifySlackWebhook": self.hook.url, **config}}
        inp = approval.OpenApprovalInput(run_id=self.run, node=node, agent_id="ra_ok",
                                         upstream={"write": {"text": "We ship faster."}})
        return asyncio.run(approval.open_approval(inp))

    def test_open_notify_decide_relay_resolve(self):
        opened = self.open()
        self.assertFalse(opened.failed, opened.error)
        self.assertEqual(opened.timeout_s, 72 * 3600)
        row = asyncio.run(db.approval_by_id(opened.approval_id))
        self.assertEqual(row["draft"], "Post: We ship faster.")
        self.assertEqual(row["signal_ids"], [self.signal_id])   # signals of this run, automatically
        self.assertEqual(row["status"], "pending")
        [post] = self.hook.posts
        self.assertIn("Approval needed for Rahul: LinkedIn post (LinkedIn)", post["text"])
        self.assertIn(f"https://agently.test/inbox/{opened.approval_id}", post["text"])
        self.assertEqual(row["notify"]["targets"][0]["ok"], True)
        status = asyncio.run(self._sql("select status, summary from run_agents where id = 'ra_ok'"))[0]
        self.assertEqual(status[0], "waiting")

        # A retried activity returns the same approval and doesn't notify again.
        again = self.open()
        self.assertEqual(again.approval_id, opened.approval_id)
        self.assertEqual(len(self.hook.posts), 1)

        asyncio.run(approval.set_waiting(approval.SetWaitingInput(run_id=self.run, waiting=True, step="Waiting")))
        self.assertEqual(asyncio.run(self._sql("select status from runs where id = %s", (self.run,)))[0][0], "paused")

        # Nothing to relay until a person decides…
        client = _FakeClient()
        self.assertEqual(asyncio.run(dispatcher.relay_decisions(client)), 0)
        # …which the API does with this update (pgApprovalRepo.Decide).
        asyncio.run(self._sql(
            "update approvals set status='approved', final_content='We ship 2x faster.', decided_by='Rahul', "
            "decided_at=now() where id = %s and status='pending'", (opened.approval_id,)))
        self.assertEqual(asyncio.run(dispatcher.relay_decisions(client)), 1)
        self.assertEqual(client.signals, [(f"agently-run-{self.run}", {"approvalId": opened.approval_id})])
        self.assertEqual(asyncio.run(dispatcher.relay_decisions(client)), 0, "delivered once")

        res = asyncio.run(approval.resolve_approval(approval.ResolveApprovalInput(
            run_id=self.run, agent_id="ra_ok", key="ok", approval_id=opened.approval_id)))
        self.assertTrue(res.gate_open)
        self.assertEqual(res.output["content"], "We ship 2x faster.")
        self.assertEqual(res.output["approver"], "Rahul")
        self.assertEqual(res.output["url"], f"https://agently.test/inbox/{opened.approval_id}")
        asyncio.run(approval.set_waiting(approval.SetWaitingInput(run_id=self.run, waiting=False, step="Resumed")))
        self.assertEqual(asyncio.run(self._sql("select status from runs where id = %s", (self.run,)))[0][0], "running")

    def test_failed_notification_is_recorded_not_fatal(self):
        self.hook.status = 500
        opened = self.open()
        self.assertFalse(opened.failed)
        row = asyncio.run(db.approval_by_id(opened.approval_id))
        [target] = row["notify"]["targets"]
        self.assertFalse(target["ok"])
        self.assertIn("500", target["detail"])
        logs = asyncio.run(self._sql("select level, message from run_logs where run_id = %s and level = 'warn'", (self.run,)))
        self.assertTrue(any("notification to slack failed" in m for _, m in logs))

    def test_empty_draft_fails_the_node(self):
        res = self.open(draft="   ")
        self.assertTrue(res.failed)
        self.assertIn("needs a draft", res.error)

    def test_timeout_and_last_moment_decision(self):
        opened = self.open(timeoutHours="1")
        res = asyncio.run(approval.resolve_approval(approval.ResolveApprovalInput(
            run_id=self.run, agent_id="ra_ok", key="ok", approval_id=opened.approval_id, timed_out=True)))
        self.assertFalse(res.gate_open)
        self.assertEqual(res.output["decision"], "timed_out")
        row = asyncio.run(db.approval_by_id(opened.approval_id))
        self.assertEqual(row["status"], "timed_out")
        self.assertEqual(asyncio.run(dispatcher.relay_decisions(_FakeClient())), 0, "a timeout is never relayed")

        # Second approval: the person decides just as the timer fires — they win.
        node2 = {"key": "ok2", "type": "human.approval", "config": {"draft": "x"}}
        asyncio.run(self._sql("insert into run_agents (id, run_id, name, role, model) values ('ra_ok2', %s, 'ok2', 'validator', 'none')", (self.run,)))
        o2 = asyncio.run(approval.open_approval(approval.OpenApprovalInput(run_id=self.run, node=node2, agent_id="ra_ok2")))
        asyncio.run(self._sql("update approvals set status='rejected', decided_by='Ana', decided_at=now() where id = %s", (o2.approval_id,)))
        res2 = asyncio.run(approval.resolve_approval(approval.ResolveApprovalInput(
            run_id=self.run, agent_id="ra_ok2", key="ok2", approval_id=o2.approval_id, timed_out=True,
            on_timeout="fail")))
        self.assertFalse(res2.failed, "a real decision is not a timeout")
        self.assertEqual(res2.output["decision"], "rejected")

    def test_relay_to_a_finished_run_is_stamped(self):
        opened = self.open()
        asyncio.run(self._sql("update approvals set status='approved', decided_by='R', decided_at=now() where id = %s",
                              (opened.approval_id,)))
        client = _FakeClient()
        client.gone.add(f"agently-run-{self.run}")
        self.assertEqual(asyncio.run(dispatcher.relay_decisions(client)), 1)
        self.assertIsNotNone(asyncio.run(db.approval_by_id(opened.approval_id)) and
                             asyncio.run(self._sql("select signaled_at from approvals where id = %s", (opened.approval_id,)))[0][0])


if __name__ == "__main__":
    unittest.main()
