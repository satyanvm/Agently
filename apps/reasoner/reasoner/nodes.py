"""Type→handler registry for the dynamic execution engine.

Each entry maps a node `type` (the catalog id from
apps/web/components/builder/node-catalog.ts) to an async handler. A handler
receives a `NodeContext` — the node definition, the resolved outputs of its
upstream nodes, and the run's trigger input — and returns an output dict that
downstream nodes can reference as `{{outputs.<key>.<field>}}`.

Keep the supported types and their config keys in sync with `NODE_FIELDS` in the
web node catalog: the inspector writes those keys, and the handlers read them.
Handlers reuse the same llm / browser / db / obs modules as the static graph, so
a dynamic run writes the identical run_logs / run_agents / artifacts / browser_*
rows the UI already renders.
"""
from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable

import httpx

from . import browser, db, llm
from .config import CONFIG


@dataclass
class NodeContext:
    run_id: str
    agent_id: str
    node: dict[str, Any]           # the GraphNode: key/role/type/config/dependsOn
    upstream: dict[str, dict]      # upstream node key → that node's output dict
    run_input: dict[str, Any]      # the run's trigger input
    label: str = ""


@dataclass
class NodeResult:
    output: dict[str, Any] = field(default_factory=dict)
    summary: str = ""
    tokens_in: int = 0
    tokens_out: int = 0
    cost_usd: float = 0.0


Handler = Callable[[NodeContext], Awaitable[NodeResult]]

_REGISTRY: dict[str, Handler] = {}


def handles(*types: str) -> Callable[[Handler], Handler]:
    def register(fn: Handler) -> Handler:
        for t in types:
            _REGISTRY[t] = fn
        return fn
    return register


def handler_for(node_type: str) -> Handler:
    """Return the handler for a node type, or a passthrough for unknown types."""
    return _REGISTRY.get(node_type, _passthrough)


def supported_types() -> list[str]:
    return sorted(_REGISTRY)


# ─────────────────────────── templating ───────────────────────────

_TOKEN = re.compile(r"\{\{\s*([^}]+?)\s*\}\}")


def render(template: Any, ctx: NodeContext) -> str:
    """Resolve {{input.x}} / {{outputs.key.field}} references in a string.

    Non-string templates are coerced with str(). Unknown references collapse to
    an empty string rather than raising, so a half-configured node still runs.
    """
    if not isinstance(template, str):
        return "" if template is None else str(template)

    root = {"input": ctx.run_input, "outputs": ctx.upstream}

    def resolve(expr: str) -> str:
        cur: Any = root
        for part in expr.split("."):
            if isinstance(cur, dict):
                cur = cur.get(part)
            else:
                return ""
        if cur is None:
            return ""
        return cur if isinstance(cur, str) else json.dumps(cur)

    return _TOKEN.sub(lambda m: resolve(m.group(1)), template)


def _clip(s: str, n: int = 280) -> str:
    s = (s or "").strip().replace("\n", " ")
    return s if len(s) <= n else s[: n - 1] + "…"


def _cfg(ctx: NodeContext, key: str, default: str = "") -> str:
    val = ctx.node.get("config", {}).get(key)
    return default if val is None else str(val)


# ─────────────────────────── handlers ───────────────────────────

@handles("trigger.manual", "trigger.webhook", "trigger.schedule")
async def _trigger(ctx: NodeContext) -> NodeResult:
    """A trigger simply seeds the run's input for downstream nodes."""
    return NodeResult(output={"input": ctx.run_input}, summary="Triggered")


@handles("agent.llm", "agent.chat")
async def _agent(ctx: NodeContext) -> NodeResult:
    system = render(_cfg(ctx, "system") or "You are a helpful agent.", ctx)
    prompt = render(_cfg(ctx, "prompt"), ctx)
    if not prompt.strip():
        # No explicit prompt: fall back to the run topic + upstream text so the
        # node still does something useful.
        topic = ctx.run_input.get("topic") or ctx.run_input.get("prompt") or "the task"
        upstream = "\n\n".join(
            str(v.get("text", "")) for v in ctx.upstream.values() if isinstance(v, dict)
        )
        prompt = f"Task: {topic}\n\n{upstream}".strip()

    model = _cfg(ctx, "model") or CONFIG.model
    out = await llm.complete(ctx.run_id, ctx.node["key"], system=system, user=prompt, model=model)
    return NodeResult(
        output={"text": out.text, "model": model},
        summary=_clip(out.text),
        tokens_in=out.tokens_in,
        tokens_out=out.tokens_out,
        cost_usd=out.cost_usd,
    )


@handles("tool.browser")
async def _browser(ctx: NodeContext) -> NodeResult:
    raw = render(_cfg(ctx, "urls"), ctx)
    urls = [u.strip() for u in re.split(r"[\s,]+", raw) if u.strip()]
    if not urls:
        # Accept urls handed down from an upstream node's output.
        for v in ctx.upstream.values():
            if isinstance(v, dict) and isinstance(v.get("urls"), list):
                urls = [str(u) for u in v["urls"]]
                break
    findings = await browser.run_browse(ctx.run_id, ctx.label or "Browser", urls)
    return NodeResult(
        output={"text": findings, "urls": urls, "pages": len(urls)},
        summary=f"Visited {len(urls)} page(s)",
    )


@handles("tool.http")
async def _http(ctx: NodeContext) -> NodeResult:
    method = (_cfg(ctx, "method") or "GET").upper()
    url = render(_cfg(ctx, "url"), ctx)
    if not url:
        return NodeResult(output={"error": "no url configured"}, summary="Skipped — no URL")
    headers = _parse_json_obj(render(_cfg(ctx, "headers"), ctx))
    body = render(_cfg(ctx, "body"), ctx)
    try:
        async with httpx.AsyncClient(timeout=30, follow_redirects=True) as client:
            resp = await client.request(
                method, url, headers=headers or None, content=(body or None)
            )
        text = resp.text[:8000]
        await db.append_log(
            ctx.run_id, "info", "system", ctx.node["key"],
            f"{method} {url} → {resp.status_code}",
        )
        return NodeResult(
            output={"status": resp.status_code, "body": text},
            summary=f"{method} {url} → {resp.status_code}",
        )
    except Exception as exc:  # noqa: BLE001 — surface as node output, not a crash
        return NodeResult(output={"error": str(exc)}, summary=f"HTTP failed: {exc}")


@handles("tool.code", "tool.db")
async def _recorded(ctx: NodeContext) -> NodeResult:
    """Code / DB steps are recorded honestly, not executed on the shared runtime.

    Matches the web catalog's help text ("Recorded, not executed …"). The source
    is preserved so a later, sandboxed executor can run it for real.
    """
    kind = "code" if ctx.node["type"] == "tool.code" else "query"
    source = _cfg(ctx, "source") or _cfg(ctx, "query")
    await db.append_log(
        ctx.run_id, "info", "system", ctx.node["key"],
        f"Recorded {kind} (not executed on shared runtime)", detail=source or None,
    )
    return NodeResult(output={kind: source, "executed": False}, summary=f"Recorded {kind}")


@handles("logic.branch", "logic.filter")
async def _condition(ctx: NodeContext) -> NodeResult:
    expr = _cfg(ctx, "condition")
    passed = _eval_condition(expr, ctx)
    await db.append_log(
        ctx.run_id, "info", "system", ctx.node["key"],
        f"Condition `{expr or 'true'}` → {passed}",
    )
    return NodeResult(output={"passed": passed, "condition": expr}, summary=f"→ {passed}")


@handles("logic.loop")
async def _loop(ctx: NodeContext) -> NodeResult:
    expr = _cfg(ctx, "items")
    items = _resolve_ref(expr, ctx)
    count = len(items) if isinstance(items, list) else 0
    return NodeResult(
        output={"items": items if isinstance(items, list) else [], "count": count},
        summary=f"{count} item(s)",
    )


@handles("output.email", "output.slack")
async def _notify(ctx: NodeContext) -> NodeResult:
    node_type = ctx.node["type"]
    if node_type == "output.email":
        target = render(_cfg(ctx, "to"), ctx)
        subject = render(_cfg(ctx, "subject"), ctx) or "Agently workflow result"
        channel = f"email to {target}" if target else "email (no recipient)"
    else:
        target = render(_cfg(ctx, "webhookUrl"), ctx)
        subject = render(_cfg(ctx, "message"), ctx)
        channel = "Slack" if target else "Slack (no webhook)"
    # Delivery is wired to the Go notifier in a later phase; record the intent.
    await db.append_log(
        ctx.run_id, "info", "system", ctx.node["key"],
        f"Would deliver via {channel}", detail=subject or None,
    )
    return NodeResult(output={"delivered": bool(target), "channel": channel}, summary=channel)


@handles("output.report")
async def _report(ctx: NodeContext) -> NodeResult:
    title = render(_cfg(ctx, "title"), ctx) or "report"
    fmt = _cfg(ctx, "format") or "markdown"
    # Compose from the most relevant upstream text output.
    body = ""
    for v in reversed(list(ctx.upstream.values())):
        if isinstance(v, dict) and v.get("text"):
            body = str(v["text"])
            break
    ext = "pdf" if fmt == "pdf" else "md"
    await db.add_artifact(ctx.run_id, f"{_slug(title)}.{ext}", "report", ctx.label or "Composer", body)
    return NodeResult(output={"title": title, "format": fmt}, summary=f"Report: {title}")


async def _passthrough(ctx: NodeContext) -> NodeResult:
    """Fallback for unknown node types — pass upstream through, log once."""
    await db.append_log(
        ctx.run_id, "warn", "system", ctx.node["key"],
        f"No handler for type '{ctx.node.get('type')}' — passing through",
    )
    merged: dict[str, Any] = {}
    for v in ctx.upstream.values():
        if isinstance(v, dict):
            merged.update(v)
    return NodeResult(output=merged, summary="Passthrough")


# ─────────────────────────── helpers ───────────────────────────

def _parse_json_obj(s: str) -> dict[str, Any]:
    if not s.strip():
        return {}
    try:
        val = json.loads(s)
        return val if isinstance(val, dict) else {}
    except (ValueError, TypeError):
        return {}


def _resolve_ref(expr: str, ctx: NodeContext) -> Any:
    """Resolve a dotted reference like `outputs.fetch.items` to a value."""
    if not expr:
        return None
    root = {"input": ctx.run_input, "outputs": ctx.upstream}
    cur: Any = root
    for part in expr.strip().split("."):
        if isinstance(cur, dict):
            cur = cur.get(part)
        else:
            return None
    return cur


# A deliberately tiny comparison evaluator — NOT a general expression engine.
# Supports `<lhs> <op> <rhs>` with ==, !=, >, <, >=, <=; both sides may be a
# dotted ref or a literal. Anything it can't parse defaults to True (fail-open),
# so a malformed condition never silently drops a branch.
_COND = re.compile(r"^\s*(.+?)\s*(==|!=|>=|<=|>|<)\s*(.+?)\s*$")


def _eval_condition(expr: str, ctx: NodeContext) -> bool:
    if not expr or not expr.strip():
        return True
    m = _COND.match(expr)
    if not m:
        # Bare truthiness of a reference, e.g. `outputs.fetch.ok`.
        val = _resolve_ref(expr, ctx)
        return bool(val)
    lhs = _coerce(_side(m.group(1), ctx))
    rhs = _coerce(_side(m.group(3), ctx))
    op = m.group(2)
    try:
        if op == "==":
            return lhs == rhs
        if op == "!=":
            return lhs != rhs
        if op == ">":
            return lhs > rhs
        if op == "<":
            return lhs < rhs
        if op == ">=":
            return lhs >= rhs
        if op == "<=":
            return lhs <= rhs
    except TypeError:
        return str(lhs) == str(rhs) if op == "==" else True
    return True


def _side(token: str, ctx: NodeContext) -> Any:
    token = token.strip()
    if (token.startswith('"') and token.endswith('"')) or (
        token.startswith("'") and token.endswith("'")
    ):
        return token[1:-1]
    if token.startswith(("input.", "outputs.")):
        return _resolve_ref(token, ctx)
    return token


def _coerce(val: Any) -> Any:
    if isinstance(val, str):
        low = val.lower()
        if low in ("true", "false"):
            return low == "true"
        try:
            return int(val)
        except ValueError:
            try:
                return float(val)
            except ValueError:
                return val
    return val


def _slug(s: str) -> str:
    out = "".join(c if c.isalnum() else "-" for c in s.lower()).strip("-")
    return out or "report"
