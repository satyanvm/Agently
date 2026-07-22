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

import asyncio
import json
import os
import re
import smtplib
import urllib.parse
from dataclasses import dataclass, field
from email.message import EmailMessage
from typing import Any, Awaitable, Callable

import httpx

from . import browser, catalog, db, llm, pieces, sandbox
from .config import CONFIG


@dataclass
class NodeContext:
    run_id: str
    agent_id: str
    node: dict[str, Any]           # the GraphNode: key/role/type/config/dependsOn
    upstream: dict[str, dict]      # upstream node key → that node's output dict
    run_input: dict[str, Any]      # the run's trigger input
    label: str = ""
    # Extra template roots (loop fan-out injects {"item": …, "loop": {...}} here so
    # body nodes can reference {{item}} / {{loop.<key>.index}}).
    extra: dict[str, Any] = field(default_factory=dict)


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
    """Return the handler for a node type.

    Resolution order: code-backed built-ins (this registry) → the shared
    integration catalog (generic executor, hundreds of types as data) →
    Activepieces piece actions (record-intent here; see note on _piece_fallback)
    → passthrough for genuinely unknown types.
    """
    if node_type in _REGISTRY:
        return _REGISTRY[node_type]
    if catalog.spec_for(node_type) is not None:
        return _integration
    if pieces.is_piece_type(node_type):
        return _piece_fallback
    return _passthrough


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

    root = {"input": ctx.run_input, "outputs": ctx.upstream, **ctx.extra}

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


def render_tpl(template: Any, ctx: NodeContext, extra_roots: dict[str, Any]) -> str:
    """render() plus catalog-template extensions.

    Integration definitions (packages/nodes) template over two more roots —
    `config` (the node's filled config) and `credentials` (env vars) — and two
    helpers for safe embedding: `{{json expr}}` (JSON-encoded, for JSON bodies)
    and `{{urlencode expr}}` (percent-encoded, for query strings / form bodies).
    Same fail-open semantics as render(): unknown references collapse to "".
    """
    if not isinstance(template, str):
        return "" if template is None else str(template)

    root = {"input": ctx.run_input, "outputs": ctx.upstream, **ctx.extra, **extra_roots}

    def lookup(expr: str) -> Any:
        cur: Any = root
        for part in expr.split("."):
            if isinstance(cur, dict):
                cur = cur.get(part)
            else:
                return None
        return cur

    def resolve(raw: str) -> str:
        expr = raw.strip()
        helper = ""
        for prefix in ("json ", "urlencode "):
            if expr.startswith(prefix):
                helper, expr = prefix.strip(), expr[len(prefix):].strip()
                break
        val = lookup(expr)
        if helper == "json":
            return json.dumps("" if val is None else val)
        if helper == "urlencode":
            s = "" if val is None else (val if isinstance(val, str) else json.dumps(val))
            return urllib.parse.quote(s, safe="")
        if val is None:
            return ""
        return val if isinstance(val, str) else json.dumps(val)

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


@handles("tool.code")
async def _code(ctx: NodeContext) -> NodeResult:
    """Execute a code snippet in the subprocess sandbox — opt-in via
    TOOL_CODE_ENABLED=1. Unconfigured environments record the source instead
    (the old behaviour), with a loud log pointing at the gate.
    """
    language = _cfg(ctx, "language") or "python"
    source = _cfg(ctx, "source")
    key = ctx.node["key"]
    if not CONFIG.tool_code_enabled:
        await db.append_log(
            ctx.run_id, "warn", "system", key,
            "Code recorded, not executed — set TOOL_CODE_ENABLED=1 to run tool.code for real",
            detail=source or None,
        )
        return NodeResult(output={"code": source, "executed": False}, summary="Recorded code (sandbox disabled)")

    payload = {"input": ctx.run_input, "outputs": ctx.upstream, "config": ctx.node.get("config", {}), **ctx.extra}
    res = await sandbox.run(language, source, payload)
    if not res.ok:
        await db.append_log(ctx.run_id, "error", "tool", key, f"Code failed: {res.error}", detail=res.stdout or None)
        return NodeResult(output={"error": res.error, "stdout": res.stdout, "executed": True}, summary=f"Code failed: {_clip(res.error, 120)}")
    await db.append_log(ctx.run_id, "info", "tool", key, f"Code ran ({language})", detail=_clip(res.stdout, 500) or None)
    return NodeResult(
        output={"result": res.result, "stdout": res.stdout, "executed": True},
        summary="Code ran" if res.result is None else _clip(json.dumps(res.result)),
    )


@handles("tool.db")
async def _db_query(ctx: NodeContext) -> NodeResult:
    """Run SQL against the dedicated TOOL_DB_URL database — never the platform
    Postgres. Unconfigured environments record the query instead.
    """
    query = render(_cfg(ctx, "query"), ctx)
    key = ctx.node["key"]
    if not CONFIG.tool_db_url:
        await db.append_log(
            ctx.run_id, "info", "system", key,
            "Query recorded, not executed — set TOOL_DB_URL to run tool.db against a database",
            detail=query or None,
        )
        return NodeResult(output={"query": query, "executed": False}, summary="Recorded query (no TOOL_DB_URL)")

    try:
        rows, count = await db.run_tool_query(CONFIG.tool_db_url, query, max_rows=200)
    except Exception as exc:  # noqa: BLE001 — surface as node output, not a crash
        await db.append_log(ctx.run_id, "error", "tool", key, f"Query failed: {exc}", detail=query)
        return NodeResult(output={"error": str(exc), "executed": True}, summary=f"Query failed: {_clip(str(exc), 120)}")
    await db.append_log(ctx.run_id, "info", "tool", key, f"Query returned {count} row(s)", detail=query)
    return NodeResult(output={"rows": rows, "rowCount": count, "executed": True}, summary=f"{count} row(s)")


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
    """Resolve the items list. The ORCHESTRATOR owns the fan-out.

    This handler's only job is resolving config.items (a dotted ref like
    `outputs.fetch.items`) to a concrete list. Both orchestrators — the per-node
    DynamicWorkflow and the single-activity engine.execute_graph — then run the
    loop's dominated body once per item (plan.loop_body), injecting `{{item}}` /
    `{{loop.<key>.index}}`, and attach the collected per-item outputs here as
    `outputs.<key>.results`. Splitting it this way keeps the handler pure enough
    to run as ONE Temporal activity while the fan-out stays deterministic
    workflow-side.
    """
    expr = _cfg(ctx, "items")
    items = _resolve_ref(expr, ctx)
    items = items if isinstance(items, list) else []
    count = len(items)
    await db.append_log(
        ctx.run_id, "info", "system", ctx.node["key"],
        f"Loop resolved {count} item(s) — fanning body out per item",
    )
    return NodeResult(
        output={"items": items, "count": count},
        summary=f"{count} item(s)",
    )


@handles("output.email")
async def _email(ctx: NodeContext) -> NodeResult:
    """Deliver a run digest by email over SMTP; fall back to record-intent.

    Mirrors the retired Go worker's SMTP seam (archive/worker/internal/notifier): SMTP_HOST /
    SMTP_PORT / SMTP_USER / SMTP_PASS / SMTP_FROM. When SMTP is unconfigured (or the
    send fails, or there is no recipient) we degrade to recording the intent — a log
    line plus an artifact — so the run never fails and the UI still shows something.
    """
    to = render(_cfg(ctx, "to"), ctx)
    subject = render(_cfg(ctx, "subject"), ctx) or "Agently workflow result"
    body = _notify_body(ctx)
    key = ctx.node["key"]

    delivered = False
    detail = subject
    if CONFIG.smtp_enabled and to:
        try:
            await asyncio.to_thread(_send_smtp, to, subject, body)
            delivered = True
            await db.append_log(ctx.run_id, "success", "system", key, f"Emailed {to}", detail=subject)
        except Exception as exc:  # noqa: BLE001 — never fail the run on a delivery error
            detail = f"{subject} — send failed, recorded instead: {exc}"
            await db.append_log(ctx.run_id, "warn", "system", key, f"Email send failed: {exc}", detail=subject)
    else:
        reason = "SMTP not configured" if not CONFIG.smtp_enabled else "no recipient"
        await db.append_log(ctx.run_id, "info", "system", key, f"Email recorded ({reason})", detail=subject)

    # Always leave an artifact so the digest is visible in the UI regardless.
    await db.add_artifact(ctx.run_id, f"{_slug(subject)}.eml", "file", ctx.label or "Email", f"To: {to}\nSubject: {subject}\n\n{body}")
    channel = f"email to {to}" if to else "email (no recipient)"
    return NodeResult(output={"delivered": delivered, "channel": channel, "to": to}, summary=("Emailed " if delivered else "Recorded ") + channel)


@handles("output.slack")
async def _slack(ctx: NodeContext) -> NodeResult:
    """POST a message to a Slack incoming webhook; fall back to record-intent.

    Follows the same provider-seam discipline as the rest of the reasoner: with a
    webhook URL we POST via httpx; without one (or on any error) we record the
    intent and still write an artifact. Never fails the run.
    """
    webhook = render(_cfg(ctx, "webhookUrl"), ctx)
    message = render(_cfg(ctx, "message"), ctx) or _notify_body(ctx)
    key = ctx.node["key"]

    delivered = False
    if webhook:
        try:
            async with httpx.AsyncClient(timeout=15) as client:
                resp = await client.post(webhook, json={"text": message})
            resp.raise_for_status()
            delivered = True
            await db.append_log(ctx.run_id, "success", "system", key, f"Posted to Slack → {resp.status_code}", detail=_clip(message))
        except Exception as exc:  # noqa: BLE001 — never fail the run on a delivery error
            await db.append_log(ctx.run_id, "warn", "system", key, f"Slack post failed: {exc}", detail=_clip(message))
    else:
        await db.append_log(ctx.run_id, "info", "system", key, "Slack recorded (no webhook configured)", detail=_clip(message))

    await db.add_artifact(ctx.run_id, "slack-message.txt", "file", ctx.label or "Slack", message)
    channel = "Slack" if webhook else "Slack (no webhook)"
    return NodeResult(output={"delivered": delivered, "channel": channel}, summary=("Posted to " if delivered else "Recorded ") + channel)


def _notify_body(ctx: NodeContext) -> str:
    """Build the notification body: explicit `body`/`message` config, else the most
    recent upstream text output (the digest a report/agent produced)."""
    explicit = render(_cfg(ctx, "body") or _cfg(ctx, "message"), ctx)
    if explicit.strip():
        return explicit
    for v in reversed(list(ctx.upstream.values())):
        if isinstance(v, dict) and v.get("text"):
            return str(v["text"])
    return "Your Agently workflow finished."


def _send_smtp(to: str, subject: str, body: str) -> None:
    """Blocking SMTP send (run via asyncio.to_thread). STARTTLS + PLAIN auth,
    matching the Go notifier's net/smtp.SendMail path and message shape."""
    msg = EmailMessage()
    msg["From"] = CONFIG.smtp_from or CONFIG.smtp_user
    msg["To"] = to
    msg["Subject"] = subject
    msg.set_content(body)
    with smtplib.SMTP(CONFIG.smtp_host, int(CONFIG.smtp_port or "587"), timeout=30) as server:
        try:
            server.starttls()
        except smtplib.SMTPException:
            pass  # server may not support STARTTLS (e.g. local relay) — send anyway
        if CONFIG.smtp_user and CONFIG.smtp_pass:
            server.login(CONFIG.smtp_user, CONFIG.smtp_pass)
        server.send_message(msg)


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


async def _integration(ctx: NodeContext) -> NodeResult:
    """Generic executor for catalog-defined integration nodes (packages/nodes).

    One handler for hundreds of node types: the definition declares a runtime —
      http    → render the request template (config/credentials roots + json/
                urlencode helpers) and perform it; outputMap lifts response fields.
      browser → drive the existing browser stack over the templated URLs.
      code    → run the definition's program in the tool.code sandbox (same
                TOOL_CODE_ENABLED gate).
      llm     → complete with the definition's system/prompt templates.

    Missing credentials degrade to record-intent with a loud log — a graph never
    fails because an env var isn't set on this deployment.
    """
    spec = catalog.spec_for(str(ctx.node.get("type", ""))) or {}
    key = ctx.node["key"]
    runtime = spec.get("runtime", "http")

    creds: dict[str, str] = {}
    missing: list[str] = []
    for c in spec.get("credentials") or []:
        val = os.getenv(c.get("key", ""), "")
        if val:
            creds[c["key"]] = val
        else:
            missing.append(c.get("key", "?"))
    if missing:
        await db.append_log(
            ctx.run_id, "warn", "system", key,
            f"{ctx.node.get('type')} recorded — missing credential env var(s): {', '.join(missing)}",
        )
        return NodeResult(
            output={"recorded": True, "executed": False, "missingCredentials": missing},
            summary=f"Recorded (needs {', '.join(missing)})",
        )

    cfg = ctx.node.get("config", {}) or {}
    roots = {"config": cfg, "credentials": creds}

    if runtime == "browser":
        raw = render_tpl(_cfg(ctx, "urls"), ctx, roots)
        urls = [u.strip() for u in re.split(r"[\s,]+", raw) if u.strip()]
        findings = await browser.run_browse(ctx.run_id, ctx.label or spec.get("label", key), urls)
        return NodeResult(output={"text": findings, "urls": urls, "pages": len(urls)}, summary=f"Visited {len(urls)} page(s)")

    if runtime == "code":
        code_spec = spec.get("code") or {}
        if not CONFIG.tool_code_enabled:
            await db.append_log(
                ctx.run_id, "warn", "system", key,
                f"{ctx.node.get('type')} recorded — set TOOL_CODE_ENABLED=1 to execute code-runtime nodes",
            )
            return NodeResult(output={"recorded": True, "executed": False}, summary="Recorded (sandbox disabled)")
        payload = {"input": ctx.run_input, "outputs": ctx.upstream, "config": cfg, **ctx.extra}
        res = await sandbox.run(code_spec.get("language", "python"), code_spec.get("source", ""), payload)
        if not res.ok:
            return NodeResult(output={"error": res.error, "stdout": res.stdout, "executed": True}, summary=f"Failed: {_clip(res.error, 120)}")
        out = res.result if isinstance(res.result, dict) else {"result": res.result}
        out.setdefault("stdout", res.stdout)
        out["executed"] = True
        return NodeResult(output=out, summary=_clip(json.dumps(res.result)) if res.result is not None else "Ran")

    if runtime == "llm":
        llm_spec = spec.get("llm") or {}
        system = render_tpl(llm_spec.get("system", "You are a helpful agent."), ctx, roots)
        prompt = render_tpl(llm_spec.get("prompt", ""), ctx, roots)
        model = _cfg(ctx, "model") or CONFIG.model
        out = await llm.complete(ctx.run_id, key, system=system, user=prompt, model=model)
        return NodeResult(
            output={"text": out.text, "model": model}, summary=_clip(out.text),
            tokens_in=out.tokens_in, tokens_out=out.tokens_out, cost_usd=out.cost_usd,
        )

    # Default: http.
    http_spec = spec.get("http") or {}
    method = (http_spec.get("method") or "GET").upper()
    url = render_tpl(http_spec.get("url", ""), ctx, roots)
    if not url:
        return NodeResult(output={"error": "no url in definition"}, summary="Skipped — no URL")
    headers = {
        str(hk): render_tpl(hv, ctx, roots)
        for hk, hv in (http_spec.get("headers") or {}).items()
    }
    headers = {k: v for k, v in headers.items() if v}  # drop headers that resolved empty
    body = render_tpl(http_spec.get("body", ""), ctx, roots)
    auth = None
    if isinstance(http_spec.get("auth"), dict) and http_spec["auth"].get("type") == "basic":
        auth = (
            render_tpl(http_spec["auth"].get("username", ""), ctx, roots),
            render_tpl(http_spec["auth"].get("password", ""), ctx, roots),
        )
    try:
        async with httpx.AsyncClient(timeout=30, follow_redirects=True) as client:
            resp = await client.request(method, url, headers=headers or None, content=(body or None), auth=auth)
    except Exception as exc:  # noqa: BLE001 — surface as node output, not a crash
        await db.append_log(ctx.run_id, "error", "tool", key, f"{method} {url} failed: {exc}")
        return NodeResult(output={"error": str(exc)}, summary=f"HTTP failed: {_clip(str(exc), 120)}")

    text = resp.text[:8000]
    output: dict[str, Any] = {"status": resp.status_code, "body": text}
    # outputMap: output field → dotted path into the parsed JSON response.
    if http_spec.get("outputMap"):
        try:
            parsed = resp.json()
        except ValueError:
            parsed = None
        if parsed is not None:
            for field_name, path in http_spec["outputMap"].items():
                cur: Any = parsed
                for part in str(path).split("."):
                    if isinstance(cur, dict):
                        cur = cur.get(part)
                    elif isinstance(cur, list) and part.isdigit() and int(part) < len(cur):
                        cur = cur[int(part)]
                    else:
                        cur = None
                        break
                if cur is not None:
                    output[field_name] = cur
    await db.append_log(ctx.run_id, "info", "tool", key, f"{method} {url} → {resp.status_code}")
    return NodeResult(output=output, summary=f"{method} {_clip(url, 80)} → {resp.status_code}")


async def _piece_fallback(ctx: NodeContext) -> NodeResult:
    """pieces.* node reached run_node instead of the pieces queue.

    The real execution path for piece nodes is workflow-side (DynamicWorkflow
    routes them to the Node pieces worker via execute_piece — see workflow.py
    _run_piece_node). This handler only fires on the single-activity fallback
    orchestrator (engine.execute_graph), which cannot make cross-queue calls;
    there we record intent rather than half-execute.
    """
    await db.append_log(
        ctx.run_id, "warn", "system", ctx.node["key"],
        f"{ctx.node.get('type')} recorded — piece nodes execute on the pieces "
        "worker via the per-node orchestrator, not the single-activity fallback",
    )
    return NodeResult(
        output={"recorded": True, "executed": False, "reason": "fallback-orchestrator"},
        summary="Recorded (pieces run on the pieces worker)",
    )


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
