"""human.approval in DynamicWorkflow, on Temporal's time-skipping test server
with fake activities registered under the real names.

Covers: approve (with an edit) → downstream runs with the final text; reject
and timeout → downstream skipped; onTimeout=fail; two approvals open at once
(neither waits for the other); an approval nothing depends on; approvals
inside a loop; and a worker restart while the run is waiting.
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
from reasoner import approval as AP  # noqa: E402
from reasoner import plan  # noqa: E402
from reasoner import waits as W  # noqa: E402
from reasoner.workflow import DynamicWorkflow  # noqa: E402

QUEUE = "test-approval"


class Fake:
    """Fake activities + an in-memory 'approvals table' the test decides in."""

    def __init__(self, graph: list[dict[str, Any]]):
        self.graph = graph
        self.ran: list[str] = []
        self.upstream: dict[str, dict] = {}
        self.skipped: list[str] = []
        self.finish: A.FinishInput | None = None
        self.opened: list[tuple[str, int]] = []           # (node key, iteration), in order
        self.waiting: list[bool] = []
        self.rows: dict[str, dict[str, Any]] = {}          # approval id → row
        self.events: list[str] = []                        # open/wait ordering
        self.outcomes: list[tuple[str, str, tuple[str, ...]]] = []  # (node, status, approval ids)
        self.outcome_ids: list[str] = []
        self.waited: list[tuple[str, float, bool, str]] = []  # (node, hours, from start, resumed at)
        self.fail: set[str] = set()                        # node keys whose run_node fails
        self.publishing: list[str] = []                    # node keys load_graph reports as publishing

    def decide(self, approval_id: str, status: str, content: str | None = None, by: str = "Rahul") -> None:
        row = self.rows[approval_id]
        row.update(status=status, content=content if content is not None else row["draft"], by=by)

    def acts(self):
        f = self

        @activity.defn(name="load_graph")
        async def load_graph(inp: A.LoadGraphInput) -> A.LoadGraphResult:
            g = [A._lift_reserved(dict(n)) for n in plan.build_plan(f.graph).ordered]
            return A.LoadGraphResult(dynamic=True, graph=g, ordered_keys=[n["key"] for n in g],
                                     agent_ids={n["key"]: f"ra_{n['key']}" for n in g}, prior_status={},
                                     publishing=list(f.publishing))

        @activity.defn(name="run_node")
        async def run_node(inp: A.RunNodeInput) -> A.RunNodeResult:
            key = inp.node["key"]
            f.ran.append(key)
            f.upstream[key] = inp.upstream
            if inp.node.get("type") == "logic.loop":
                return A.RunNodeResult(output={"items": ["a", "b"], "count": 2})
            if key in f.fail:
                return A.RunNodeResult(failed=True, error=f"{key}: the service said no")
            return A.RunNodeResult(output={"text": f"{key} output"})

        @activity.defn(name="skip_node")
        async def skip_node(inp: A.SkipNodeInput) -> None:
            f.skipped.append(inp.key)

        @activity.defn(name="use_output")
        async def use_output(inp: A.UseOutputInput) -> A.RunNodeResult:
            return A.RunNodeResult(output=inp.output)

        @activity.defn(name="fail_node")
        async def fail_node(inp: A.FailNodeInput) -> None:
            return None

        @activity.defn(name="finish_run")
        async def finish_run(inp: A.FinishInput) -> None:
            f.finish = inp

        @activity.defn(name="add_edge")
        async def add_edge(inp: A.EdgeInput) -> None:
            return None

        @activity.defn(name="set_progress")
        async def set_progress(inp: A.ProgressInput) -> None:
            return None

        @activity.defn(name="open_approval")
        async def open_approval(inp: AP.OpenApprovalInput) -> AP.OpenApprovalResult:
            key = inp.node["key"]
            aid = f"apr_{key}_{inp.iteration}"
            f.events.append(f"open:{key}")
            if aid not in f.rows:
                f.opened.append((key, inp.iteration))
                draft = str((inp.node.get("config") or {}).get("draft") or "draft")
                f.rows[aid] = {"status": "pending", "draft": draft, "content": None, "by": None}
            return AP.OpenApprovalResult(approval_id=aid, title=key,
                                         timeout_s=AP.timeout_seconds(inp.node.get("config") or {}),
                                         status=f.rows[aid]["status"])

        @activity.defn(name="set_waiting")
        async def set_waiting(inp: AP.SetWaitingInput) -> None:
            f.waiting.append(inp.waiting)
            if inp.waiting:
                f.events.append("wait")

        @activity.defn(name="resolve_approval")
        async def resolve_approval(inp: AP.ResolveApprovalInput) -> A.RunNodeResult:
            row = f.rows[inp.approval_id]
            if inp.timed_out and row["status"] == "pending":
                row["status"] = "timed_out"
            if row["status"] == "pending":
                raise RuntimeError("woken without a decision")
            approved = row["status"] == "approved"
            if row["status"] == "timed_out" and inp.on_timeout == "fail":
                return A.RunNodeResult(failed=True, error="human.approval: approval timed out")
            return A.RunNodeResult(output={"decision": row["status"], "approved": approved,
                                           "content": row["content"] if approved else None,
                                           "approver": row["by"] or "", "passed": approved},
                                   gate_open=approved)

        @activity.defn(name="record_outcome")
        async def record_outcome(inp: AP.RecordOutcomeInput) -> None:
            if not set(inp.outcome_ids) & set(f.outcome_ids):   # same ids on a retry → nothing new
                f.outcomes.append((inp.key, inp.status, tuple(inp.approval_ids)))
                f.outcome_ids.extend(inp.outcome_ids)

        @activity.defn(name="prepare_wait")
        async def prepare_wait(inp: W.PrepareWaitInput) -> W.PrepareWaitResult:
            cfg = inp.node.get("config") or {}
            raw = cfg.get("hours")
            if isinstance(raw, str) and raw.startswith("{{item."):
                raw = inp.extra["item"][raw[7:-2]]
            try:
                hours = W.parse_hours(raw)
            except ValueError as exc:
                return W.PrepareWaitResult(failed=True, error=str(exc))
            return W.PrepareWaitResult(hours=hours, from_run_start=cfg.get("from") == "run_start")

        @activity.defn(name="finish_wait")
        async def finish_wait(inp: W.FinishWaitInput) -> A.RunNodeResult:
            f.waited.append((inp.key, inp.hours, inp.from_run_start, inp.resumed_at))
            return A.RunNodeResult(output={"waitedHours": inp.hours, "resumedAt": inp.resumed_at})

        return [load_graph, run_node, skip_node, use_output, fail_node, finish_run, add_edge,
                set_progress, open_approval, set_waiting, resolve_approval, record_outcome,
                prepare_wait, finish_wait]


def llm(key: str, deps: list[str] | None = None) -> dict[str, Any]:
    return {"key": key, "type": "agent.llm", "config": {}, "dependsOn": deps or []}


def approve(key: str, deps: list[str], **config: Any) -> dict[str, Any]:
    return {"key": key, "type": "human.approval", "dependsOn": deps,
            "config": {"draft": f"draft of {key}", **config}}


class ApprovalWorkflowTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.loop = asyncio.new_event_loop()
        cls.env = cls.loop.run_until_complete(WorkflowEnvironment.start_time_skipping())

    @classmethod
    def tearDownClass(cls):
        cls.loop.run_until_complete(cls.env.shutdown())
        cls.loop.close()

    def run_wf(self, fake: Fake, decide=None) -> dict[str, Any]:
        """Start the run; once `decide`'s approvals are open, call it (it
        decides in the fake table and returns approval ids to signal)."""
        async def go():
            async with Worker(self.env.client, task_queue=QUEUE, workflows=[DynamicWorkflow],
                              activities=fake.acts()):
                handle = await self.env.client.start_workflow(
                    DynamicWorkflow.run, {"run_id": "run_x", "input": {}, "exec": {}},
                    id=f"wf-{uuid.uuid4()}", task_queue=QUEUE)
                if decide:
                    await self._until(lambda: decide.ready(fake))
                    for aid in decide(fake):
                        await handle.signal(DynamicWorkflow.approval_decided, {"approvalId": aid})
                return await handle.result()
        return self.loop.run_until_complete(go())

    @staticmethod
    async def _until(cond, timeout_s: float = 10.0) -> None:
        for _ in range(int(timeout_s / 0.02)):
            if cond():
                return
            await asyncio.sleep(0.02)
        raise AssertionError("condition never became true")

    @staticmethod
    def deciding(keys: list[str], fn):
        """A decide callback that waits until approvals for `keys` are open
        and the run is blocked on one. Deciding before the block lets the
        signal land first, so the run never waits and the waiting
        assertions race."""
        fn.ready = lambda f: all(any(k == o[0] for o in f.opened) for k in keys) and "wait" in f.events
        return fn

    def test_approve_with_edit_runs_downstream_with_final_text(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"]), llm("publish", ["ok"])])

        def decide(f):
            f.decide("apr_ok_0", "approved", content="edited by Rahul")
            return ["apr_ok_0"]
        res = self.run_wf(fake, self.deciding(["ok"], decide))
        self.assertTrue(res["done"])
        self.assertEqual(fake.finish.status, "succeeded")
        self.assertIn("publish", fake.ran)
        self.assertEqual(fake.upstream["publish"]["ok"]["content"], "edited by Rahul")
        self.assertEqual(fake.waiting, [True, False])  # paused while blocked, running after
        self.assertEqual(fake.outcomes, [("publish", "succeeded", ("apr_ok_0",))])

    def test_reject_skips_everything_downstream(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"]), llm("publish", ["ok"]), llm("after", ["publish"])])

        def decide(f):
            f.decide("apr_ok_0", "rejected", by="Ana")
            return ["apr_ok_0"]
        self.run_wf(fake, self.deciding(["ok"], decide))
        self.assertEqual(fake.finish.status, "succeeded")
        self.assertNotIn("publish", fake.ran)
        self.assertEqual(sorted(fake.skipped), ["after", "publish"])
        self.assertEqual(sorted(fake.outcomes), [("after", "skipped", ("apr_ok_0",)),
                                                 ("publish", "skipped", ("apr_ok_0",))])

    def test_timeout_skips_by_default(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"], timeoutHours="1"), llm("publish", ["ok"])])
        self.run_wf(fake)  # nobody decides; the time-skipping server fast-forwards the hour
        self.assertEqual(fake.rows["apr_ok_0"]["status"], "timed_out")
        self.assertEqual(fake.finish.status, "succeeded")
        self.assertEqual(fake.skipped, ["publish"])

    def test_timeout_can_fail_the_run(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"], timeoutHours="2", onTimeout="fail"), llm("publish", ["ok"])])
        res = self.run_wf(fake)
        self.assertFalse(res["done"])
        self.assertEqual(fake.finish.status, "failed")
        self.assertIn("timed out", fake.finish.error)

    def test_two_approvals_open_before_either_waits(self):
        fake = Fake([llm("analyse"),
                     llm("post", ["analyse"]), approve("ok_post", ["post"]), llm("publish_post", ["ok_post"]),
                     llm("email", ["analyse"]), approve("ok_email", ["email"]), llm("send_email", ["ok_email"])])

        def decide(f):
            f.decide("apr_ok_post_0", "approved")
            f.decide("apr_ok_email_0", "rejected")
            return ["apr_ok_post_0", "apr_ok_email_0"]
        self.run_wf(fake, self.deciding(["ok_post", "ok_email"], decide))
        # Both requests reach the Inbox before the run blocks on the first one.
        self.assertEqual(fake.events[:3], ["open:ok_post", "open:ok_email", "wait"])
        self.assertIn("publish_post", fake.ran)
        self.assertNotIn("send_email", fake.ran)
        self.assertEqual(fake.skipped, ["send_email"])
        # Each outcome is filed under its own approval, not the other branch's.
        self.assertEqual(sorted(fake.outcomes), [("publish_post", "succeeded", ("apr_ok_post_0",)),
                                                 ("send_email", "skipped", ("apr_ok_email_0",))])

    def test_approval_with_no_dependents_is_resolved_before_finishing(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"]), llm("other", ["draft"])])

        def decide(f):
            f.decide("apr_ok_0", "approved")
            return ["apr_ok_0"]
        self.run_wf(fake, self.deciding(["ok"], decide))
        self.assertEqual(fake.rows["apr_ok_0"]["status"], "approved")
        self.assertEqual(fake.finish.status, "succeeded")

    def test_approval_inside_a_loop_is_one_row_per_item(self):
        fake = Fake([llm("start"),
                     {"key": "each", "type": "logic.loop", "config": {"items": "outputs.start.items"}, "dependsOn": ["start"]},
                     approve("ok", ["each"]), llm("publish", ["ok"])])

        async def decide_as_opened(f, handle):
            for i in range(2):
                await self._until(lambda: ("ok", i) in f.opened)
                f.decide(f"apr_ok_{i}", "approved" if i == 0 else "rejected")
                await handle.signal(DynamicWorkflow.approval_decided, {"approvalId": f"apr_ok_{i}"})

        async def go():
            async with Worker(self.env.client, task_queue=QUEUE, workflows=[DynamicWorkflow], activities=fake.acts()):
                handle = await self.env.client.start_workflow(
                    DynamicWorkflow.run, {"run_id": "run_x", "input": {}, "exec": {}},
                    id=f"wf-{uuid.uuid4()}", task_queue=QUEUE)
                await decide_as_opened(fake, handle)
                return await handle.result()
        self.loop.run_until_complete(go())
        self.assertEqual(fake.opened, [("ok", 0), ("ok", 1)])
        self.assertEqual(fake.ran.count("publish"), 1)  # item 0 approved, item 1 rejected
        self.assertEqual(fake.outcomes, [("publish", "succeeded", ("apr_ok_0",)),
                                         ("publish", "skipped", ("apr_ok_1",))])

    def test_loop_items_open_all_approvals_then_publish_on_their_slots(self):
        # The weekly-plan shape: per post, approve → wait for its slot → publish.
        fake = Fake([llm("plan"),
                     {"key": "each", "type": "logic.loop", "config": {"items": "outputs.plan.items"}, "dependsOn": ["plan"]},
                     approve("ok", ["each"]),
                     {"key": "slot", "type": "logic.wait", "dependsOn": ["ok"],
                      "config": {"hours": "{{item.delayHours}}", "from": "run_start"}},
                     llm("publish", ["slot"])])
        orig = fake.acts

        def acts():
            out = orig()
            for i, a in enumerate(out):
                if getattr(a, "__temporal_activity_definition").name == "run_node":
                    @activity.defn(name="run_node")
                    async def run_node(inp: A.RunNodeInput) -> A.RunNodeResult:
                        fake.ran.append(inp.node["key"])
                        if inp.node.get("type") == "logic.loop":
                            return A.RunNodeResult(output={"items": [{"delayHours": 24}, {"delayHours": 48}], "count": 2})
                        return A.RunNodeResult(output={"text": "ok"})
                    out[i] = run_node
            return out
        fake.acts = acts

        async def go():
            async with Worker(self.env.client, task_queue=QUEUE, workflows=[DynamicWorkflow], activities=fake.acts()):
                handle = await self.env.client.start_workflow(
                    DynamicWorkflow.run, {"run_id": "run_x", "input": {}, "exec": {}},
                    id=f"wf-{uuid.uuid4()}", task_queue=QUEUE)
                # Both posts reach the Inbox before anything waits.
                await self._until(lambda: len(fake.opened) == 2 and "wait" in fake.events)
                self.assertEqual(fake.events[:3], ["open:ok", "open:ok", "wait"])
                for i in range(2):
                    fake.decide(f"apr_ok_{i}", "approved")
                    await handle.signal(DynamicWorkflow.approval_decided, {"approvalId": f"apr_ok_{i}"})
                return await handle.result(), (await handle.describe()).start_time
        (res, started) = self.loop.run_until_complete(go())
        self.assertTrue(res["done"])
        self.assertEqual(fake.ran.count("publish"), 2)
        # Slots are absolute from the run start (24 h, 48 h), not 24 h after the previous one.
        import datetime as dt
        resumed = [dt.datetime.fromisoformat(w[3]) for w in fake.waited]
        self.assertEqual([round((r - started).total_seconds() / 3600) for r in resumed], [24, 48])
        self.assertEqual([o[:2] for o in fake.outcomes if o[0] == "publish"],
                         [("publish", "succeeded"), ("publish", "succeeded")])

    def test_wait_with_bad_hours_fails_the_run(self):
        fake = Fake([llm("a"), {"key": "slot", "type": "logic.wait", "dependsOn": ["a"], "config": {"hours": "soon"}},
                     llm("b", ["slot"])])
        res = self.run_wf(fake)
        self.assertFalse(res["done"])
        self.assertIn("hours must be a number", fake.finish.error)
        self.assertNotIn("b", fake.ran)

    def test_worker_restart_while_waiting(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"]), llm("publish", ["ok"])])
        wf_id = f"wf-{uuid.uuid4()}"

        async def go():
            # No workflow cache on either worker: worker 2 must rebuild the run
            # from Temporal history alone, which is what a crash leaves behind.
            # (A cached run would also pin the signal's task to worker 1's sticky
            # queue, which the time-skipping test server never times out.)
            async with Worker(self.env.client, task_queue=QUEUE, workflows=[DynamicWorkflow], activities=fake.acts(),
                              max_cached_workflows=0):
                handle = await self.env.client.start_workflow(
                    DynamicWorkflow.run, {"run_id": "run_x", "input": {}, "exec": {}}, id=wf_id, task_queue=QUEUE)
                await self._until(lambda: fake.waiting == [True])
            # The worker is gone; the run is just a Temporal history waiting for a signal.
            fake.decide("apr_ok_0", "approved", content="final")
            await handle.signal(DynamicWorkflow.approval_decided, {"approvalId": "apr_ok_0"})
            async with Worker(self.env.client, task_queue=QUEUE, workflows=[DynamicWorkflow], activities=fake.acts(),
                              max_cached_workflows=0):
                return await handle.result()
        res = self.loop.run_until_complete(go())
        self.assertTrue(res["done"])
        self.assertEqual(fake.opened, [("ok", 0)], "the approval must not be opened twice")
        self.assertEqual(fake.ran.count("draft"), 1, "completed nodes must not re-run")
        self.assertEqual(fake.upstream["publish"]["ok"]["content"], "final")
        self.assertEqual(fake.outcomes, [("publish", "succeeded", ("apr_ok_0",))])

    def test_nodes_before_the_approval_record_no_outcome(self):
        fake = Fake([llm("draft"), approve("ok", ["draft"]), llm("publish", ["ok"]), llm("side", ["draft"])])

        def decide(f):
            f.decide("apr_ok_0", "approved")
            return ["apr_ok_0"]
        self.run_wf(fake, self.deciding(["ok"], decide))
        self.assertEqual([o[0] for o in fake.outcomes], ["publish"])

    def test_failed_publish_does_not_strand_other_drafts(self):
        fake = Fake([llm("analyse"),
                     llm("post", ["analyse"]), approve("ok_post", ["post"]), llm("publish_post", ["ok_post"]),
                     llm("after_post", ["publish_post"]),
                     llm("email", ["analyse"]), approve("ok_email", ["email"]), llm("send_email", ["ok_email"])])
        fake.fail = {"publish_post"}

        def decide(f):
            f.decide("apr_ok_post_0", "approved")
            f.decide("apr_ok_email_0", "approved")
            return ["apr_ok_post_0", "apr_ok_email_0"]
        res = self.run_wf(fake, self.deciding(["ok_post", "ok_email"], decide))
        # The email still goes out; only what depended on the failed post is skipped.
        self.assertIn("send_email", fake.ran)
        self.assertEqual(fake.skipped, ["after_post"])
        self.assertIn(("publish_post", "failed", ("apr_ok_post_0",)), fake.outcomes)
        self.assertIn(("send_email", "succeeded", ("apr_ok_email_0",)), fake.outcomes)
        # The run still ends failed, naming the step that failed.
        self.assertFalse(res["done"])
        self.assertEqual(fake.finish.status, "failed")
        self.assertIn("publish_post", fake.finish.error)

    def test_publishing_step_is_never_retried(self):
        def graph():
            return [llm("draft"), approve("ok", ["draft"]),
                    {**llm("publish", ["ok"]), "settings": {"retries": 2, "retryWaitSec": 1}}]

        def decide(f):
            f.decide("apr_ok_0", "approved")
            return ["apr_ok_0"]
        # An ordinary step is retried as its settings say...
        plain = Fake(graph())
        plain.fail = {"publish"}
        self.run_wf(plain, self.deciding(["ok"], decide))
        self.assertEqual(plain.ran.count("publish"), 3)
        # ...a publishing step runs once: a retry could post twice.
        pub = Fake(graph())
        pub.fail, pub.publishing = {"publish"}, ["publish"]
        self.run_wf(pub, self.deciding(["ok"], decide))
        self.assertEqual(pub.ran.count("publish"), 1)
        self.assertEqual(pub.finish.status, "failed")

    def test_published_url_extraction(self):
        self.assertEqual(AP.published_url({"result": {"id": "1", "permalink": "https://x.test/p/1"}}),
                         "https://x.test/p/1")
        self.assertEqual(AP.published_url({"result": [{"webViewLink": "https://docs.test/d"}]}), "https://docs.test/d")
        self.assertEqual(AP.published_url({"url": "not a link", "text": "hi"}), "")

    def test_published_link_per_integration(self):
        # Output shapes as record_piece_result stores them: the piece's return
        # value spread at the top level and kept whole under `result`.
        def stored(raw):
            return {**raw, "result": raw, "executed": True}
        docs = stored({"documentId": "1AbC", "replies": [{}], "writeControl": {}})
        self.assertEqual(AP.published_link("pieces.google-docs.create_document", docs),
                         "https://docs.google.com/document/d/1AbC/edit")
        self.assertEqual(AP.published_link("pieces.google-docs.append_text", docs),
                         "https://docs.google.com/document/d/1AbC/edit")
        draft = stored({"data": {"id": "r-55", "message": {"id": "18c0ffee", "threadId": "18c0ffee"}}, "status": 200})
        self.assertEqual(AP.published_link("pieces.gmail.send_email", draft),
                         "https://mail.google.com/mail/u/0/#drafts?compose=18c0ffee")
        sent = stored({"data": {"id": "18c0beef", "threadId": "18c0beef", "labelIds": ["SENT"]}, "status": 200})
        self.assertEqual(AP.published_link("pieces.gmail.send_email", sent),
                         "https://mail.google.com/mail/u/0/#all/18c0beef")
        self.assertEqual(AP.published_link("pieces.linkedin.create_share_update",
                                           stored({"success": True, "post_urn": "urn:li:share:7000"})),
                         "https://www.linkedin.com/feed/update/urn:li:share:7000/")
        self.assertEqual(AP.published_link("pieces.twitter.create-tweet",
                                           stored({"data": {"id": "1850000000000000000", "text": "hi"}})),
                         "https://x.com/i/web/status/1850000000000000000")
        # No id in the output → no link, never a guessed one (the LinkedIn
        # 0.2.x piece returned only {success: true}).
        self.assertEqual(AP.published_link("pieces.linkedin.create_share_update", stored({"success": True})), "")
        # A Gmail response's request URL is not the published item.
        self.assertEqual(AP.published_link("pieces.gmail.send_email",
                                           stored({"config": {"url": "https://gmail.googleapis.com/gmail/v1/users/me/drafts"}})),
                         "")
        # Other integrations still use a link their output contains.
        self.assertEqual(AP.published_link("output.slack", {"permalink": "https://slack.test/p/1"}),
                         "https://slack.test/p/1")

    def test_timeout_hours_validation(self):
        self.assertEqual(AP.timeout_seconds({}), 72 * 3600)
        self.assertEqual(AP.timeout_seconds({"timeoutHours": "0.5"}), 1800)
        for bad in ("0", "-1", "abc", str(24 * 31)):
            with self.assertRaises(ValueError):
                AP.timeout_seconds({"timeoutHours": bad})


if __name__ == "__main__":
    unittest.main()
