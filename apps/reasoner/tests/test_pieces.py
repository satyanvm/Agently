"""Tests for the Activepieces piece-node path (docs/pieces-runtime-contract.md):
index loading, prepare (render/credential gating), record persistence, and the
workflow-side routing predicate. No Postgres/Temporal/network — FakeDB + a tmp
index fixture, same discipline as test_dynamic_nodes.py.
"""
from __future__ import annotations

import asyncio
import importlib
import json
import os
import tempfile
import unittest
from pathlib import Path

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")

from reasoner import activities, nodes, pieces  # noqa: E402

from test_engine import FakeDB  # noqa: E402 — reuse the in-memory db fake

# A minimal contract-conformant index (§2) with one auth-required and one
# auth-less action.
FIXTURE_INDEX = {
    "version": 1,
    "generatedAt": "2026-07-16T00:00:00Z",
    "nodes": [
        {
            "id": "pieces.slack.send_channel_message",
            "piece": "@activepieces/piece-slack",
            "pieceVersion": "0.11.4",
            "action": "send_channel_message",
            "label": "Send message to a channel",
            "description": "Sends a message.",
            "kind": "action",
            "search": ["slack", "message"],
            "auth": {"type": "oauth2", "credentialKey": "AP_SLACK_AUTH", "required": True},
            "props": [
                {"key": "channel", "label": "Channel", "type": "short_text", "required": True},
                {"key": "text", "label": "Text", "type": "long_text", "required": True},
            ],
        },
        {
            "id": "pieces.math-helper.add",
            "piece": "@activepieces/piece-math-helper",
            "pieceVersion": "0.1.0",
            "action": "add",
            "label": "Add numbers",
            "description": "Adds numbers.",
            "kind": "action",
            "search": ["math"],
            "auth": {"type": "none", "credentialKey": None, "required": False},
            "props": [
                {"key": "first_number", "label": "First", "type": "number", "required": True},
                {"key": "second_number", "label": "Second", "type": "number", "required": True},
            ],
        },
    ],
}


def _reload_with_fixture(index: dict | None):
    """Point PIECES_INDEX_PATH at a tmp fixture (or nowhere) and reload the module."""
    if index is None:
        os.environ["PIECES_INDEX_PATH"] = "/nonexistent/pieces-index.json"
    else:
        tmp = Path(tempfile.mkdtemp()) / "index.json"
        tmp.write_text(json.dumps(index))
        os.environ["PIECES_INDEX_PATH"] = str(tmp)
    importlib.reload(pieces)


def tearDownModule():  # noqa: N802 — unittest hook
    """Restore the real (repo) index for any suites imported after this one."""
    os.environ.pop("PIECES_INDEX_PATH", None)
    importlib.reload(pieces)


class PiecesIndexTest(unittest.TestCase):
    def test_loads_fixture_and_resolves_specs(self):
        _reload_with_fixture(FIXTURE_INDEX)
        self.assertEqual(pieces.size(), 2)
        spec = pieces.piece_spec_for("pieces.slack.send_channel_message")
        self.assertEqual(spec["piece"], "@activepieces/piece-slack")
        self.assertEqual(pieces.credential_key_for(spec), "AP_SLACK_AUTH")
        self.assertTrue(pieces.auth_required(spec))
        noauth = pieces.piece_spec_for("pieces.math-helper.add")
        self.assertIsNone(pieces.credential_key_for(noauth))
        self.assertFalse(pieces.auth_required(noauth))

    def test_missing_index_degrades_to_empty(self):
        _reload_with_fixture(None)
        self.assertEqual(pieces.size(), 0)
        self.assertIsNone(pieces.piece_spec_for("pieces.slack.send_channel_message"))

    def test_is_piece_type(self):
        self.assertTrue(pieces.is_piece_type("pieces.slack.send_channel_message"))
        self.assertFalse(pieces.is_piece_type("slack.sendMessage"))
        self.assertFalse(pieces.is_piece_type("agent.llm"))


class HandlerRoutingTest(unittest.TestCase):
    """pieces.* types must fall through the registry+catalog to _piece_fallback
    (the workflow, not run_node, owns real piece execution)."""

    def test_piece_type_routes_to_fallback_handler(self):
        _reload_with_fixture(FIXTURE_INDEX)
        self.assertIs(nodes.handler_for("pieces.slack.send_channel_message"), nodes._piece_fallback)
        self.assertIs(nodes.handler_for("agent.llm"), nodes._agent)
        self.assertIs(nodes.handler_for("totally.unknown"), nodes._passthrough)

    def test_fallback_records_intent(self):
        FakeDB().install()
        node = {"key": "s", "type": "pieces.slack.send_channel_message", "config": {}}
        ctx = nodes.NodeContext(run_id="r", agent_id="ra_s", node=node, upstream={}, run_input={})
        res = asyncio.run(nodes._piece_fallback(ctx))
        self.assertFalse(res.output["executed"])
        self.assertEqual(res.output["reason"], "fallback-orchestrator")


class PreparePieceNodeTest(unittest.TestCase):
    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()
        _reload_with_fixture(FIXTURE_INDEX)
        # activities holds its own reference to the module object's functions via
        # `from . import pieces` — reloading rebinds reasoner.pieces in-place, so
        # activities.pieces sees the fixture. Verify that assumption holds:
        self.assertEqual(activities.pieces.size(), 2)

    def _prepare(self, node, upstream=None, run_input=None):
        inp = activities.PreparePieceInput(
            run_id="run_1", node=node, agent_id="ra_x",
            upstream=upstream or {}, run_input=run_input or {},
        )
        return asyncio.run(activities.prepare_piece_node(inp))

    def test_renders_templates_and_builds_payload(self):
        os.environ["AP_SLACK_AUTH"] = "xoxb-token"
        try:
            node = {
                "key": "s", "type": "pieces.slack.send_channel_message",
                "config": {"channel": "#general", "text": "Digest: {{outputs.research.text}}"},
            }
            prep = self._prepare(node, upstream={"research": {"text": "hello world"}})
        finally:
            os.environ.pop("AP_SLACK_AUTH", None)
        self.assertEqual(prep.mode, "execute")
        self.assertEqual(prep.payload["piece"], "@activepieces/piece-slack")
        self.assertEqual(prep.payload["action"], "send_channel_message")
        self.assertEqual(prep.payload["props"]["channel"], "#general")
        self.assertEqual(prep.payload["props"]["text"], "Digest: hello world")
        # Secret stays OUT of the payload — only the env key name crosses.
        self.assertEqual(prep.payload["authEnvKey"], "AP_SLACK_AUTH")
        self.assertNotIn("xoxb-token", json.dumps(prep.payload))
        # Agent shows live while the Node worker runs it.
        self.assertEqual(self.fake.agents["ra_x"]["status"], "running")

    def test_typed_and_templated_props(self):
        node = {
            "key": "m", "type": "pieces.math-helper.add",
            "config": {"first_number": 2, "second_number": "{{outputs.calc.n}}"},
        }
        prep = self._prepare(node, upstream={"calc": {"n": 40}})
        self.assertEqual(prep.mode, "execute")
        self.assertEqual(prep.payload["props"], {"first_number": 2, "second_number": 40})
        self.assertIsNone(prep.payload["authEnvKey"])

    def test_missing_credential_records(self):
        os.environ.pop("AP_SLACK_AUTH", None)
        node = {"key": "s", "type": "pieces.slack.send_channel_message",
                "config": {"channel": "#g", "text": "hi"}}
        prep = self._prepare(node)
        self.assertEqual(prep.mode, "record")
        self.assertEqual(prep.result["missingCredentials"], ["AP_SLACK_AUTH"])
        self.assertFalse(prep.result["executed"])

    def test_credential_id_counts_as_present_and_travels(self):
        """A DB credential selection (config.__credentialId) satisfies the
        presence gate without the env var; the ID travels in the payload
        (docs/credentials-contract.md §7), stripped from props."""
        os.environ.pop("AP_SLACK_AUTH", None)
        node = {
            "key": "s", "type": "pieces.slack.send_channel_message",
            "config": {"channel": "#g", "text": "hi", "__credentialId": "cred_abc123"},
        }
        prep = self._prepare(node)
        self.assertEqual(prep.mode, "execute")
        self.assertEqual(prep.payload["credentialId"], "cred_abc123")
        self.assertEqual(prep.payload["authEnvKey"], "AP_SLACK_AUTH")
        self.assertNotIn("__credentialId", prep.payload["props"])

    def test_no_credential_id_payload_field_is_none(self):
        os.environ["AP_SLACK_AUTH"] = "xoxb-token"
        try:
            node = {"key": "s", "type": "pieces.slack.send_channel_message",
                    "config": {"channel": "#g", "text": "hi"}}
            prep = self._prepare(node)
        finally:
            os.environ.pop("AP_SLACK_AUTH", None)
        self.assertEqual(prep.mode, "execute")
        self.assertIsNone(prep.payload["credentialId"])

    def test_unknown_piece_records(self):
        node = {"key": "x", "type": "pieces.ghost.do_thing", "config": {}}
        prep = self._prepare(node)
        self.assertEqual(prep.mode, "record")
        self.assertEqual(prep.result["reason"], "unknown-piece")

    def test_undeclared_config_keys_stay_out_of_props(self):
        node = {
            "key": "m", "type": "pieces.math-helper.add",
            "config": {"first_number": 1, "second_number": 2, "model": "claude-x"},
        }
        prep = self._prepare(node)
        self.assertNotIn("model", prep.payload["props"])


class RecordPieceResultTest(unittest.TestCase):
    def setUp(self):
        self.fake = FakeDB()
        self.fake.install()

    def _record(self, result, mode):
        inp = activities.RecordPieceInput(
            run_id="run_1", node={"key": "s", "type": "pieces.slack.send_channel_message"},
            agent_id="ra_s", result=result, mode=mode,
        )
        return asyncio.run(activities.record_piece_result(inp))

    def test_ok_result_lands_as_output(self):
        res = self._record({"ok": True, "output": {"ts": "123.45", "channel": "C1"}}, "execute")
        self.assertEqual(res.output["ts"], "123.45")
        self.assertTrue(res.output["executed"])
        self.assertFalse(res.failed)
        self.assertTrue(res.gate_open)
        self.assertEqual(self.fake.agents["ra_s"]["status"], "succeeded")

    def test_non_dict_output_is_wrapped(self):
        res = self._record({"ok": True, "output": [1, 2, 3]}, "execute")
        self.assertEqual(res.output["value"], [1, 2, 3])

    def test_piece_error_recorded_without_failing_run(self):
        res = self._record({"ok": False, "error": "channel_not_found",
                            "errorType": "PieceExecutionError"}, "execute")
        self.assertEqual(res.output["error"], "channel_not_found")
        self.assertFalse(res.failed, "piece errors must not fail the workflow")
        self.assertEqual(self.fake.agents["ra_s"]["status"], "succeeded")
        self.assertTrue(any("channel_not_found" in m for _, _, m in self.fake.logs))

    def test_record_intent_passthrough(self):
        res = self._record({"recorded": True, "executed": False,
                            "missingCredentials": ["AP_SLACK_AUTH"]}, "record")
        self.assertFalse(res.output["executed"])
        self.assertIn("AP_SLACK_AUTH", res.output["missingCredentials"])

    def test_worker_unavailable_shape(self):
        res = self._record({"recorded": True, "executed": False,
                            "reason": "pieces-worker-unavailable"}, "record")
        self.assertEqual(res.output["reason"], "pieces-worker-unavailable")
        self.assertEqual(self.fake.agents["ra_s"]["status"], "succeeded")


if __name__ == "__main__":
    unittest.main()
