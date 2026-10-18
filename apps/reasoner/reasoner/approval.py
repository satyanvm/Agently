"""human.approval: the activities behind a durable "wait for a person" step.

The node itself is orchestrated in workflow.py (DynamicWorkflow). These are its
side effects, each a Temporal activity:

  open_approval      render the draft, insert the approvals row (idempotent),
                     notify Slack / email with a link to the Inbox
  set_waiting        runs.status = 'paused' while blocked, 'running' after
  resolve_approval   turn the decision (or a timeout) into the node's output

The decision itself reaches the workflow as a Temporal signal sent by the
dispatcher's relay (dispatcher.py relay_decisions) after the API records it.

Notifications are best effort: the Inbox is the source of truth. A failed
Slack post or email is logged as a warning on the run and stored on the
approval (approvals.notify) — never silently dropped, never fatal.
"""
from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from typing import Any

import httpx
from temporalio import activity

from . import db, nodes
from .activities import RunNodeResult
from .config import CONFIG
from .nodes import NodeContext

DEFAULT_TIMEOUT_HOURS = 72.0
MAX_TIMEOUT_HOURS = 24.0 * 30


@dataclass
class OpenApprovalInput:
    run_id: str
    node: dict[str, Any]
    agent_id: str
    upstream: dict[str, Any] = field(default_factory=dict)
    run_input: dict[str, Any] = field(default_factory=dict)
    extra: dict[str, Any] = field(default_factory=dict)
    iteration: int = 0


@dataclass
class OpenApprovalResult:
    approval_id: str = ""
    title: str = ""
    timeout_s: float = 0.0
    # A decision that already exists (the workflow restarted after it was made).
    status: str = "pending"
    failed: bool = False
    error: str = ""


@dataclass
class SetWaitingInput:
    run_id: str
    waiting: bool
    step: str


@dataclass
class ResolveApprovalInput:
    run_id: str
    agent_id: str
    key: str
    approval_id: str
    on_timeout: str = "skip"   # skip | fail
    # True when the wait ran out. Otherwise the decision is read back from the
    # approvals row (the source of truth); the signal only wakes the run.
    timed_out: bool = False


def timeout_seconds(cfg: dict[str, Any]) -> float:
    raw = cfg.get("timeoutHours")
    try:
        hours = float(raw) if raw not in (None, "") else DEFAULT_TIMEOUT_HOURS
    except (TypeError, ValueError) as exc:
        raise ValueError(f"timeoutHours must be a number, not {raw!r}") from exc
    if hours <= 0 or hours > MAX_TIMEOUT_HOURS:
        raise ValueError(f"timeoutHours must be between 0 and {MAX_TIMEOUT_HOURS:g}")
    return hours * 3600


def _signal_ids(raw: str) -> list[str]:
    raw = raw.strip()
    if not raw:
        return []
    if raw.startswith("["):
        vals = json.loads(raw)
        return [str(v) for v in vals if v]
    return [p.strip() for p in raw.replace("\n", ",").split(",") if p.strip()]


def inbox_url(approval_id: str) -> str:
    return f"{CONFIG.app_base_url}/inbox/{approval_id}"


def _message(row: dict[str, Any]) -> str:
    excerpt = row["draft"] if len(row["draft"]) <= 600 else row["draft"][:599] + "…"
    who = f" for {row['assignee']}" if row["assignee"] else ""
    where = f" ({row['channel']})" if row["channel"] else ""
    return (f"Approval needed{who}: {row['title'] or 'Draft'}{where}\n\n{excerpt}\n\n"
            f"Review, edit, approve or reject: {inbox_url(row['id'])}")


async def _notify(run_id: str, key: str, row: dict[str, Any], cfg: dict[str, Any], ctx: NodeContext) -> dict[str, Any]:
    results: dict[str, Any] = {"url": inbox_url(row["id"]), "targets": []}
    text = _message(row)
    webhook = nodes.render(cfg.get("notifySlackWebhook") or "", ctx).strip() or CONFIG.approval_slack_webhook
    email = nodes.render(cfg.get("notifyEmail") or "", ctx).strip()

    async def record(target: str, ok: bool, detail: str) -> None:
        results["targets"].append({"target": target, "ok": ok, "detail": detail})
        level = "info" if ok else "warn"
        msg = f"Approval request sent to {target}" if ok else f"Approval notification to {target} failed: {detail}"
        await db.append_log(run_id, level, "system", key, msg)

    if webhook:
        try:
            async with httpx.AsyncClient(timeout=15) as client:
                resp = await client.post(webhook, json={"text": text})
            resp.raise_for_status()
            await record("slack", True, f"HTTP {resp.status_code}")
        except Exception as exc:  # noqa: BLE001 - best effort, recorded
            await record("slack", False, str(exc))
    if email:
        if not CONFIG.smtp_enabled:
            await record(f"email {email}", False, "SMTP_HOST is not configured")
        else:
            try:
                await asyncio.to_thread(nodes._send_smtp, email,
                                        f"Approval needed: {row['title'] or 'Draft'}", text)
                await record(f"email {email}", True, "sent")
            except Exception as exc:  # noqa: BLE001 - best effort, recorded
                await record(f"email {email}", False, str(exc))
    if not results["targets"]:
        await db.append_log(run_id, "info", "system", key,
                            f"No notification configured; the draft is waiting in the Inbox: {inbox_url(row['id'])}")
    return results


@activity.defn
async def open_approval(inp: OpenApprovalInput) -> OpenApprovalResult:
    key = inp.node["key"]
    cfg = dict(inp.node.get("config") or {})
    ctx = NodeContext(run_id=inp.run_id, agent_id=inp.agent_id, node=inp.node, upstream=inp.upstream,
                      run_input=inp.run_input, extra=inp.extra)
    try:
        draft = nodes.render(cfg.get("draft") or "", ctx).strip()
        if not draft:
            raise ValueError("human.approval needs a draft to approve (config.draft is empty)")
        title = nodes.render(cfg.get("title") or "", ctx).strip() or "Draft"
        channel = nodes.render(cfg.get("channel") or "", ctx).strip()
        assignee = nodes.render(cfg.get("assignee") or "", ctx).strip()
        timeout_s = timeout_seconds(cfg)
        on_timeout = str(cfg.get("onTimeout") or "skip")
        if on_timeout not in ("skip", "fail"):
            raise ValueError("onTimeout must be skip or fail")
        signal_ids = _signal_ids(nodes.render(cfg.get("signalIds") or "", ctx)) or await db.signal_ids_for_run(inp.run_id)
        ws = await db.run_workspace_id(inp.run_id)
        row, created = await db.open_approval(
            ws, run_id=inp.run_id, node_key=key, iteration=inp.iteration, title=title[:300],
            channel=channel[:120], draft=draft, assignee=assignee[:120], signal_ids=signal_ids,
            timeout_s=timeout_s,
        )
    except Exception as exc:  # noqa: BLE001 - surfaced as a node failure
        await db.set_agent_status(inp.agent_id, "failed", summary=str(exc)[:280])
        await db.set_agent_result(inp.agent_id, error=str(exc), attempts=1)
        await db.append_log(inp.run_id, "error", "system", key, f"Approval could not be opened: {exc}")
        return OpenApprovalResult(failed=True, error=str(exc))

    if created:
        notify = await _notify(inp.run_id, key, row, cfg, ctx)
        await db.set_approval_notify(row["id"], notify)
        await db.append_log(inp.run_id, "info", "system", key,
                            f"Waiting for approval: {title}" + (f" (assigned to {assignee})" if assignee else ""),
                            detail=inbox_url(row["id"]))
    if row["status"] == "pending":
        await db.set_agent_status(inp.agent_id, "waiting",
                                  summary=f"Waiting for approval{' from ' + assignee if assignee else ''}")
    return OpenApprovalResult(approval_id=row["id"], title=title, timeout_s=timeout_s, status=row["status"])


@activity.defn
async def set_waiting(inp: SetWaitingInput) -> None:
    await db.set_run_waiting(inp.run_id, inp.waiting, inp.step)


def _decision_from_row(row: dict[str, Any]) -> dict[str, Any]:
    decided_at = row.get("decided_at")
    return {
        "approvalId": row["id"], "status": row["status"], "content": row.get("final_content"),
        "decidedBy": row.get("decided_by"), "note": row.get("decision_note"),
        "decidedAt": decided_at.isoformat() if hasattr(decided_at, "isoformat") else decided_at,
    }


@activity.defn
async def resolve_approval(inp: ResolveApprovalInput) -> RunNodeResult:
    row = await (db.time_out_approval(inp.approval_id) if inp.timed_out else db.approval_by_id(inp.approval_id))
    if row is None:
        raise RuntimeError(f"approval {inp.approval_id} not found")
    if row["status"] == "pending":
        raise RuntimeError(f"approval {inp.approval_id} is still pending; the run was woken without a decision")
    decision = _decision_from_row(row)
    status = str(decision.get("status") or "")
    approved = status == "approved"
    out = {
        "approvalId": inp.approval_id, "decision": status, "approved": approved,
        "content": decision.get("content") if approved else None,
        "approver": decision.get("decidedBy") or "", "note": decision.get("note") or "",
        "decidedAt": decision.get("decidedAt") or "", "passed": approved,
        "url": inbox_url(inp.approval_id),
    }
    if status == "timed_out":
        summary = "Timed out waiting for approval"
    else:
        summary = f"{'Approved' if approved else 'Rejected'} by {out['approver'] or 'someone'}"
    level = "success" if approved else "warn"
    await db.append_log(inp.run_id, level, "system", inp.key, summary, detail=out["note"] or None)

    if status == "timed_out" and inp.on_timeout == "fail":
        error = "approval timed out (onTimeout = fail)"
        await db.set_agent_status(inp.agent_id, "failed", summary=summary)
        await db.set_agent_result(inp.agent_id, output=out, error=error, attempts=1)
        return RunNodeResult(failed=True, error=f"human.approval: {error}")
    await db.set_agent_status(inp.agent_id, "succeeded", summary=summary)
    await db.set_agent_result(inp.agent_id, output=out, attempts=1)
    return RunNodeResult(output=out, gate_open=approved)


@dataclass
class RecordOutcomeInput:
    run_id: str
    agent_id: str
    key: str
    node_type: str
    status: str                 # succeeded | failed | skipped
    approval_ids: list[str] = field(default_factory=list)
    outcome_ids: list[str] = field(default_factory=list)  # one per approval, minted by the workflow


# Output fields that commonly hold a link to the thing a node created
# (a post, a doc, a page). Searched a few levels deep.
_URL_KEYS = ("url", "permalink", "link", "webViewLink", "htmlUrl", "html_url", "documentUrl",
             "postUrl", "publicUrl", "href")
MAX_OUTCOME_OUTPUT = 64_000


def published_url(output: Any, depth: int = 0) -> str:
    """Best-effort link to what a node produced; '' when there is none."""
    if depth > 3:
        return ""
    if isinstance(output, dict):
        for k in _URL_KEYS:
            v = output.get(k)
            if isinstance(v, str) and v.startswith(("http://", "https://")):
                return v
        for v in output.values():
            if isinstance(v, (dict, list)) and (found := published_url(v, depth + 1)):
                return found
    elif isinstance(output, list):
        for v in output[:20]:
            if found := published_url(v, depth + 1):
                return found
    return ""


@activity.defn
async def record_outcome(inp: RecordOutcomeInput) -> None:
    """Link a node that ran (or was skipped) after a decision to that
    approval — for a publish node, this row IS the published item."""
    row = await db.agent_row(inp.agent_id) or {}
    output = row.get("output")
    if output is not None and len(json.dumps(output, default=str)) > MAX_OUTCOME_OUTPUT:
        output = {"truncated": True, "note": "output too large to keep with the approval; see the run"}
    summary = str(row.get("summary") or row.get("error") or "")[:500]
    url = published_url(row.get("output")) if inp.status == "succeeded" else ""
    for approval_id, outcome_id in zip(inp.approval_ids, inp.outcome_ids):
        await db.insert_approval_outcome(
            outcome_id, approval_id=approval_id, run_id=inp.run_id, node_key=inp.key, node_type=inp.node_type,
            status=inp.status, summary=summary, url=url, output=output,
        )


ACTIVITIES = [open_approval, set_waiting, resolve_approval, record_outcome]
