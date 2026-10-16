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
import html
import json
import os
import random
import re
import smtplib
import urllib.parse
from dataclasses import dataclass, field
from email.message import EmailMessage
from typing import Any, Awaitable, Callable

import httpx

from . import browser, catalog, db, llm, pieces, sandbox
from .config import CONFIG

HTTP_TIMEOUT_S = float(os.environ.get("HTTP_TIMEOUT_S", "") or 60)
HTTP_RETRIES = 3  # extra attempts on 429/503 (and transport errors on GETs)
# HTTP_SOFT_FAIL=0 makes a failed catalog GET fail the run instead of continuing.
HTTP_SOFT_FAIL = os.environ.get("HTTP_SOFT_FAIL", "1").strip() != "0"


def _retry_wait(resp: Any, attempt: int) -> float:
    """Seconds to wait before retrying: Retry-After when sent, else jittered
    backoff (about 2s, 4s, 8s), so parallel nodes rate-limited together don't
    all come back at the same instant."""
    try:
        return min(float(resp.headers.get("retry-after", "")), 30.0)
    except (TypeError, ValueError, AttributeError):
        return float(2 ** (attempt + 1)) + random.uniform(0, 1.5)


# Most catalog requests to one host in flight at once, per worker. Parallel
# runs fan many nodes out to the same public API (six Semantic Scholar or arXiv
# searches at a time), which their anonymous rate limits answer with 429s.
HTTP_PER_HOST = max(1, int(os.environ.get("HTTP_PER_HOST", "") or 2))
_host_slots: dict[tuple[int, str], asyncio.Semaphore] = {}


def _host_slot(url: str) -> asyncio.Semaphore:
    # Keyed by event loop too: a semaphore binds to the loop that first waits on it.
    key = (id(asyncio.get_running_loop()), urllib.parse.urlsplit(url).netloc.lower())
    slot = _host_slots.get(key)
    if slot is None:
        slot = _host_slots[key] = asyncio.Semaphore(HTTP_PER_HOST)
    return slot


def _drop_empty_query_params(url: str) -> str:
    """Remove `name=` pairs with no value from a rendered URL.

    Optional config the planner leaves out renders as "", which turned
    `&sortBy={{urlencode config.sortBy}}` into `&sortBy=`; arXiv answers that
    with HTTP 400, and other APIs treat an empty value as a bad filter.
    """
    parts = urllib.parse.urlsplit(url)
    if not parts.query:
        return url
    kept = [p for p in parts.query.split("&") if p and not p.endswith("=")]
    return urllib.parse.urlunsplit(parts._replace(query="&".join(kept)))


HTTP_USER_AGENT = (
    os.environ.get("HTTP_USER_AGENT", "").strip()
    or "Agently/1.0 (+https://github.com/satyanvm/Agently)"
)


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
    Activepieces piece actions (which must be routed to the pieces worker) → an
    explicit unsupported-type failure.
    """
    if node_type in _REGISTRY:
        return _REGISTRY[node_type]
    if catalog.spec_for(node_type) is not None:
        return _integration
    if pieces.is_piece_type(node_type):
        return _misrouted_piece
    return _unsupported


def supported_types() -> list[str]:
    return sorted(_REGISTRY)


# ─────────────────────────── templating ───────────────────────────

_TOKEN = re.compile(r"\{\{\s*([^}]+?)\s*\}\}")


def render(template: Any, ctx: NodeContext) -> str:
    """Resolve {{input.x}} / {{outputs.key.field}} references in a string.

    Non-string templates are coerced with str(). Unknown references are errors;
    silently replacing them with empty strings produces corrupt downstream calls.
    """
    if not isinstance(template, str):
        return "" if template is None else str(template)

    root = {"input": ctx.run_input, "outputs": ctx.upstream, **ctx.extra}

    def resolve(expr: str) -> str:
        cur: Any = root
        for part in expr.split("."):
            if not isinstance(cur, dict) or part not in cur:
                raise ValueError(f"template reference not found: {expr}")
            cur = cur[part]
        if cur is None:
            raise ValueError(f"template reference is null: {expr}")
        return cur if isinstance(cur, str) else json.dumps(cur)

    return _TOKEN.sub(lambda m: resolve(m.group(1)), template)


def render_tpl(template: Any, ctx: NodeContext, extra_roots: dict[str, Any]) -> str:
    """render() plus catalog-template extensions.

    Integration definitions (packages/nodes) template over two more roots —
    `config` (the node's filled config) and `credentials` (env vars) — and two
    helpers for safe embedding: `{{json expr}}` (JSON-encoded, for JSON bodies)
    and `{{urlencode expr}}` (percent-encoded, for query strings / form bodies).
    Unknown references raise instead of collapsing to an empty string.
    """
    if not isinstance(template, str):
        return "" if template is None else str(template)

    root = {"input": ctx.run_input, "outputs": ctx.upstream, **ctx.extra, **extra_roots}

    def lookup(expr: str) -> Any:
        cur: Any = root
        for part in expr.split("."):
            if not isinstance(cur, dict) or part not in cur:
                raise ValueError(f"template reference not found: {expr}")
            cur = cur[part]
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
            if val is None:
                raise ValueError(f"template reference is null: {expr}")
            return json.dumps(val)
        if helper == "urlencode":
            if val is None:
                raise ValueError(f"template reference is null: {expr}")
            s = val if isinstance(val, str) else json.dumps(val)
            return urllib.parse.quote(s, safe="")
        if val is None:
            raise ValueError(f"template reference is null: {expr}")
        return val if isinstance(val, str) else json.dumps(val)

    return _TOKEN.sub(lambda m: resolve(m.group(1)), template)


def _clip(s: str, n: int = 280) -> str:
    s = (s or "").strip().replace("\n", " ")
    return s if len(s) <= n else s[: n - 1] + "…"


def _cfg(ctx: NodeContext, key: str, default: str = "") -> str:
    val = ctx.node.get("config", {}).get(key)
    return default if val is None else str(val)


def _render_config(cfg: dict[str, Any], ctx: NodeContext) -> dict[str, Any]:
    """Resolve {{input.*}} / {{outputs.*}} references inside string config values.

    Only run references are resolved; any other {{...}} (e.g. a template meant
    for a later stage) is left verbatim. Unknown run references still raise via
    render(), so a typo never collapses into an empty query.
    """
    def one(val: Any) -> Any:
        if not isinstance(val, str) or "{{" not in val:
            return val
        return _TOKEN.sub(
            lambda m: render(m.group(0), ctx)
            if m.group(1).strip().split(".", 1)[0] in ("input", "outputs")
            else m.group(0),
            val,
        )

    return {k: one(v) for k, v in cfg.items()}


# Reserved config key naming the DB credential a node uses
# (docs/credentials-contract.md §6). Never a template value: it is stripped from
# config before any prop/template rendering.
CREDENTIAL_ID_KEY = "__credentialId"


def split_credential(cfg: dict[str, Any]) -> tuple[dict[str, Any], str]:
    """Split a node config into (public config, credential id).

    The public config has the reserved __credentialId key removed — the shape
    every template root / sandbox payload must see.
    """
    cred_id = str(cfg.get(CREDENTIAL_ID_KEY) or "")
    public = {k: v for k, v in cfg.items() if k != CREDENTIAL_ID_KEY}
    return public, cred_id


async def _credential_values(run_id: str, key: str, cred_id: str) -> dict[str, Any]:
    """Fetch a credential row's secret values or raise with the exact reason."""
    if not cred_id:
        return {}
    try:
        data = await db.fetch_credential_data(cred_id)
    except Exception as exc:  # noqa: BLE001 - preserve the database reason
        raise RuntimeError(f"could not resolve credential {cred_id}: {exc}") from exc
    if data is None:
        raise RuntimeError(f"credential {cred_id} not found")
    return data


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
    # Feeds hand the model HTML-escaped text and it echoes the entities back
    # (&amp;, &#34;); unescape so every node's output reads as plain text.
    text = html.unescape(out.text)
    return NodeResult(
        output={"text": text, "model": model},
        summary=_clip(text),
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
        raise ValueError("tool.http requires a URL")
    headers = _parse_json_obj(render(_cfg(ctx, "headers"), ctx))
    body = render(_cfg(ctx, "body"), ctx)
    try:
        async with httpx.AsyncClient(timeout=30, follow_redirects=True) as client:
            resp = await client.request(
                method, url, headers=headers or None, content=(body or None)
            )
        resp.raise_for_status()
        text = resp.text[:8000]
        await db.append_log(
            ctx.run_id, "info", "system", ctx.node["key"],
            f"{method} {url} → {resp.status_code}",
        )
        return NodeResult(
            output={"status": resp.status_code, "body": text},
            summary=f"{method} {url} → {resp.status_code}",
        )
    except Exception as exc:  # noqa: BLE001 - fail the node with the provider reason
        raise RuntimeError(f"{method} {url} failed: {exc}") from exc


@handles("tool.code")
async def _code(ctx: NodeContext) -> NodeResult:
    """Execute a code snippet in the subprocess sandbox."""
    language = _cfg(ctx, "language") or "python"
    source = _cfg(ctx, "source")
    key = ctx.node["key"]
    if not CONFIG.tool_code_enabled:
        raise RuntimeError("TOOL_CODE_ENABLED=1 is required to execute tool.code")

    cfg, _ = split_credential(ctx.node.get("config", {}) or {})
    payload = {"input": ctx.run_input, "outputs": ctx.upstream, "config": cfg, **ctx.extra}
    res = await sandbox.run(language, source, payload)
    if not res.ok:
        await db.append_log(ctx.run_id, "error", "tool", key, f"Code failed: {res.error}", detail=res.stdout or None)
        raise RuntimeError(f"code execution failed: {res.error}")
    await db.append_log(ctx.run_id, "info", "tool", key, f"Code ran ({language})", detail=_clip(res.stdout, 500) or None)
    return NodeResult(
        output={"result": res.result, "stdout": res.stdout, "executed": True},
        summary="Code ran" if res.result is None else _clip(json.dumps(res.result)),
    )


@handles("tool.db")
async def _db_query(ctx: NodeContext) -> NodeResult:
    """Run SQL against the dedicated TOOL_DB_URL database — never the platform
    Postgres.
    """
    query = render(_cfg(ctx, "query"), ctx)
    key = ctx.node["key"]
    if not CONFIG.tool_db_url:
        raise RuntimeError("TOOL_DB_URL is required to execute tool.db")

    try:
        rows, count = await db.run_tool_query(CONFIG.tool_db_url, query, max_rows=200)
    except Exception as exc:  # noqa: BLE001 - fail with the database reason
        await db.append_log(ctx.run_id, "error", "tool", key, f"Query failed: {exc}", detail=query)
        raise RuntimeError(f"database query failed: {exc}") from exc
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
    """Deliver a run digest by email over SMTP or fail the node."""
    to = render(_cfg(ctx, "to"), ctx)
    subject = render(_cfg(ctx, "subject"), ctx) or "Agently workflow result"
    body = _notify_body(ctx)
    key = ctx.node["key"]

    if not CONFIG.smtp_enabled:
        raise RuntimeError("SMTP_HOST is required for output.email")
    if not to:
        raise ValueError("output.email requires a recipient")
    try:
        await asyncio.to_thread(_send_smtp, to, subject, body)
    except Exception as exc:  # noqa: BLE001 - fail with SMTP's reason
        raise RuntimeError(f"email delivery to {to} failed: {exc}") from exc
    await db.append_log(ctx.run_id, "success", "system", key, f"Emailed {to}", detail=subject)
    await db.add_artifact(ctx.run_id, f"{_slug(subject)}.eml", "file", ctx.label or "Email", f"To: {to}\nSubject: {subject}\n\n{body}")
    channel = f"email to {to}"
    return NodeResult(output={"delivered": True, "channel": channel, "to": to}, summary="Emailed " + channel)


@handles("output.slack")
async def _slack(ctx: NodeContext) -> NodeResult:
    """POST a message to a Slack incoming webhook or fail the node."""
    webhook = render(_cfg(ctx, "webhookUrl"), ctx)
    message = render(_cfg(ctx, "message"), ctx) or _notify_body(ctx)
    key = ctx.node["key"]

    if not webhook:
        raise ValueError("output.slack requires webhookUrl")
    try:
        async with httpx.AsyncClient(timeout=15) as client:
            resp = await client.post(webhook, json={"text": message})
        resp.raise_for_status()
    except Exception as exc:  # noqa: BLE001 - fail with Slack's reason
        raise RuntimeError(f"Slack delivery failed: {exc}") from exc
    await db.append_log(ctx.run_id, "success", "system", key, f"Posted to Slack → {resp.status_code}", detail=_clip(message))

    await db.add_artifact(ctx.run_id, "slack-message.txt", "file", ctx.label or "Slack", message)
    return NodeResult(output={"delivered": True, "channel": "Slack"}, summary="Posted to Slack")


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


def _compose_report(title: str, upstream: dict[str, Any]) -> str:
    """The report body: every upstream text output, in dependency order.

    One input is the whole body, as before. Several inputs each become a
    section headed by the producing node's key ("framework_profile_crewai" →
    "Framework profile crewai") under the report title. This used to keep only
    the LAST input, so a report fed by a summary, a trends section and a dozen
    profiles came out as whichever node happened to be listed last.

    Feeds (GDELT, RSS, Lobsters) hand the model HTML-escaped text and it echoes
    entities such as &#34; back, so they are decoded here.
    """
    sections = [
        (key, html.unescape(str(v["text"])).strip())
        for key, v in upstream.items()
        if isinstance(v, dict) and v.get("text")
    ]
    sections = [(k, t) for k, t in sections if t]
    if not sections:
        return ""
    if len(sections) == 1:
        return sections[0][1]
    parts = [f"# {title}"]
    for key, text in sections:
        heading = key.replace("_", " ").replace("-", " ").strip().capitalize() or key
        parts.append(f"## {heading}\n\n{text}")
    return "\n\n".join(parts) + "\n"


@handles("output.report")
async def _report(ctx: NodeContext) -> NodeResult:
    title = render(_cfg(ctx, "title"), ctx) or "report"
    fmt = _cfg(ctx, "format") or "markdown"
    body = _compose_report(title, ctx.upstream)
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

    Missing credentials and runtime failures raise so the run reflects reality.
    """
    spec = catalog.spec_for(str(ctx.node.get("type", ""))) or {}
    key = ctx.node["key"]
    runtime = spec.get("runtime", "http")

    # DB-backed credentials (docs/credentials-contract.md §7): the node's
    # __credentialId (stripped from the template config) resolves to the stored
    # secret values; each declared key falls back to the process env — today's
    # behaviour — when the row doesn't provide it.
    cfg, cred_id = split_credential(ctx.node.get("config", {}) or {})
    cred_data = await _credential_values(ctx.run_id, key, cred_id)

    creds: dict[str, Any] = {}
    missing: list[str] = []
    for c in spec.get("credentials") or []:
        ckey = c.get("key", "")
        val: Any = cred_data.get(ckey)
        if val is None or val == "":
            val = os.getenv(ckey, "")
        if val:
            creds[ckey] = val
        else:
            missing.append(ckey or "?")
    if missing:
        raise RuntimeError(
            f"{ctx.node.get('type')} is missing credential(s): {', '.join(missing)}"
        )

    # Config values are themselves often templates ("query": "{{input.topic}}"),
    # and the integration template then embeds them ({{urlencode config.query}}).
    # Resolve those run references first, or the literal "{{input.topic}}" is
    # what reaches the API.
    # Optional fields the planner left out render as empty (or their declared
    # default) instead of failing the node; required ones still raise.
    for f in spec.get("config") or []:
        fkey = f.get("key")
        if fkey and not f.get("required") and cfg.get(fkey) is None:
            cfg[fkey] = f.get("default", "")
    roots = {"config": _render_config(cfg, ctx), "credentials": creds}

    if runtime == "browser":
        raw = render_tpl(_cfg(ctx, "urls"), ctx, roots)
        urls = [u.strip() for u in re.split(r"[\s,]+", raw) if u.strip()]
        findings = await browser.run_browse(ctx.run_id, ctx.label or spec.get("label", key), urls)
        return NodeResult(output={"text": findings, "urls": urls, "pages": len(urls)}, summary=f"Visited {len(urls)} page(s)")

    if runtime == "code":
        code_spec = spec.get("code") or {}
        if not CONFIG.tool_code_enabled:
            raise RuntimeError(
                f"TOOL_CODE_ENABLED=1 is required to execute {ctx.node.get('type')}"
            )
        payload = {"input": ctx.run_input, "outputs": ctx.upstream, "config": cfg, **ctx.extra}
        res = await sandbox.run(code_spec.get("language", "python"), code_spec.get("source", ""), payload)
        if not res.ok:
            raise RuntimeError(f"{ctx.node.get('type')} code execution failed: {res.error}")
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
        text = html.unescape(out.text)
        return NodeResult(
            output={"text": text, "model": model}, summary=_clip(text),
            tokens_in=out.tokens_in, tokens_out=out.tokens_out, cost_usd=out.cost_usd,
        )

    # Default: http.
    http_spec = spec.get("http") or {}
    method = (http_spec.get("method") or "GET").upper()
    url = _drop_empty_query_params(render_tpl(http_spec.get("url", ""), ctx, roots))
    if not url:
        raise ValueError(f"{ctx.node.get('type')} resolved to an empty URL")
    headers = {
        str(hk): render_tpl(hv, ctx, roots)
        for hk, hv in (http_spec.get("headers") or {}).items()
    }
    headers = {k: v for k, v in headers.items() if v}  # drop headers that resolved empty
    # Public APIs (Wikipedia, arXiv, Crossref, Reddit) reject or throttle the
    # bare httpx user-agent; send an identifying one unless the spec sets its own.
    if not any(k.lower() == "user-agent" for k in headers):
        headers["User-Agent"] = HTTP_USER_AGENT
    body = render_tpl(http_spec.get("body", ""), ctx, roots)
    auth = None
    if isinstance(http_spec.get("auth"), dict) and http_spec["auth"].get("type") == "basic":
        auth = (
            render_tpl(http_spec["auth"].get("username", ""), ctx, roots),
            render_tpl(http_spec["auth"].get("password", ""), ctx, roots),
        )
    ntype = ctx.node.get("type")
    resp = None
    failure = ""
    for attempt in range(HTTP_RETRIES + 1):
        try:
            async with _host_slot(url):
                async with httpx.AsyncClient(timeout=HTTP_TIMEOUT_S, follow_redirects=True) as client:
                    resp = await client.request(method, url, headers=headers or None, content=(body or None), auth=auth)
        except Exception as exc:  # noqa: BLE001 - fail with the upstream reason
            resp, failure = None, f"request failed: {type(exc).__name__}: {exc}".rstrip(": ")
            await db.append_log(ctx.run_id, "error", "tool", key, f"{method} {url} {failure}")
            if method != "GET":
                break  # the write may have landed; never resend it
        else:
            if 200 <= resp.status_code < 300:
                break
            failure = f"returned HTTP {resp.status_code}: {resp.text[:300]}"
            if resp.status_code not in (429, 503):
                break
        if attempt < HTTP_RETRIES:
            await asyncio.sleep(_retry_wait(resp, attempt))

    if resp is None or not (200 <= resp.status_code < 300):
        # Public data sources are flaky (rate limits, outages). A failed read
        # leaves this node's outputs empty and the run continues, so one dead
        # source doesn't sink a large research graph. Writes still fail loudly.
        if method == "GET" and HTTP_SOFT_FAIL:
            await db.append_log(ctx.run_id, "warn", "tool", key, f"{method} {url} {failure}; continuing without it")
            output = {"status": resp.status_code if resp is not None else 0, "body": "", "error": failure}
            for field_name in http_spec.get("outputMap") or {}:
                output.setdefault(field_name, "")
            return NodeResult(output=output, summary=f"Skipped: {_clip(failure, 120)}")
        raise RuntimeError(f"{ntype} {failure}")

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
        # A declared output the response didn't carry (GDELT answers {} or a
        # plain-text notice when nothing matches) is empty, not missing, so
        # downstream references still render and the raw body stays readable.
        absent = [f for f in http_spec["outputMap"] if f not in output]
        for field_name in absent:
            output[field_name] = ""
        if absent:
            await db.append_log(
                ctx.run_id, "warn", "tool", key,
                f"response had no {', '.join(absent)}; body: {_clip(text, 200)}",
            )
    await db.append_log(ctx.run_id, "info", "tool", key, f"{method} {url} → {resp.status_code}")
    return NodeResult(output=output, summary=f"{method} {_clip(url, 80)} → {resp.status_code}")


async def _misrouted_piece(ctx: NodeContext) -> NodeResult:
    """pieces.* node reached run_node instead of the pieces queue.

    The real execution path for piece nodes is workflow-side (DynamicWorkflow
    routes them to the Node pieces worker via execute_piece — see workflow.py
    _run_piece_node). This handler only fires on the single-activity fallback
    orchestrator (engine.execute_graph), which cannot make cross-queue calls;
    reaching it is a routing error and must fail the run.
    """
    raise RuntimeError(
        f"{ctx.node.get('type')} cannot execute in the single-activity orchestrator; "
        "route the run through DynamicWorkflow and the pieces worker"
    )


async def _unsupported(ctx: NodeContext) -> NodeResult:
    raise RuntimeError(f"no execution handler for node type {ctx.node.get('type')!r}")


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
        if not isinstance(cur, dict) or part not in cur:
            raise ValueError(f"reference not found: {expr}")
        cur = cur[part]
    return cur


# A deliberately tiny comparison evaluator — NOT a general expression engine.
# Supports `<lhs> <op> <rhs>` with ==, !=, >, <, >=, <=; both sides may be a
# dotted ref or a literal. Invalid comparisons raise instead of opening a branch.
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
    except TypeError as exc:
        raise ValueError(f"condition operands are not comparable: {expr}") from exc
    raise ValueError(f"unsupported condition: {expr}")


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
