"""Write-back into the shared Agently Postgres.

This is the linchpin of the slice: by writing the SAME rows the retired Go worker
wrote (runs / run_logs / run_agents / artifacts / browser_*), the existing Go API and
Next.js UI render Temporal-backed runs unchanged. Semantics deliberately mirror
archive/worker/internal/queue/{store,browser,graph}.go.

All functions are async (psycopg3) so they don't block the activity event loop.
Each call uses a short-lived autocommit connection — simple and robust for the
volume a single run produces.
"""
from __future__ import annotations

import secrets
from contextlib import asynccontextmanager
from typing import Any, AsyncIterator

import psycopg

from .config import CONFIG

_BASE36 = "0123456789abcdefghijklmnopqrstuvwxyz"


def gen_id(prefix: str) -> str:
    """Mint a prefixed id matching the app convention: prefix_<20 base36 chars>."""
    body = "".join(_BASE36[secrets.randbelow(36)] for _ in range(20))
    return f"{prefix}_{body}"


@asynccontextmanager
async def _conn() -> AsyncIterator[psycopg.AsyncConnection]:
    conn = await psycopg.AsyncConnection.connect(CONFIG.database_url, autocommit=True)
    try:
        yield conn
    finally:
        await conn.close()


# ─────────────────────────── tool.db (user database) ───────────────────────────

async def run_tool_query(url: str, query: str, max_rows: int = 200) -> tuple[list[dict[str, Any]], int]:
    """Execute SQL against the DEDICATED tool database (TOOL_DB_URL).

    This is the ONLY function in this module that connects anywhere other than the
    platform Postgres — deliberately parameterized so tool.db can never touch
    CONFIG.database_url. Row values are stringified for JSON-safety; result size is
    capped so a SELECT * on a huge table can't blow up node output.
    """
    conn = await psycopg.AsyncConnection.connect(url, autocommit=True)
    try:
        cur = await conn.execute(query)  # type: ignore[arg-type] — user SQL by design
        if cur.description is None:  # INSERT/UPDATE/DDL: no result set
            affected = cur.rowcount if cur.rowcount and cur.rowcount > 0 else 0
            return [], affected
        cols = [d.name for d in cur.description]
        rows = await cur.fetchmany(max_rows)
        out = [{c: (v if isinstance(v, (str, int, float, bool, type(None))) else str(v)) for c, v in zip(cols, r)} for r in rows]
        return out, len(out)
    finally:
        await conn.close()


# ─────────────────────────── dispatch (claim) ───────────────────────────

async def claim_queued_runs(limit: int = 10) -> list[dict[str, Any]]:
    """Return queued runs to dispatch to Temporal.

    No row-level claim is needed: dispatch is made idempotent by using the run id
    as the Temporal workflow id (duplicate starts are rejected). Once a run's plan
    node flips status to 'running', it drops out of this query.
    """
    async with _conn() as conn:
        cur = await conn.execute(
            """
            select r.id, r.workflow_id, w.slug, r.number, r.input
              from runs r join workflows w on w.id = r.workflow_id
             where r.status = 'queued'
             order by r.queued_at
             limit %s
            """,
            (limit,),
        )
        rows = await cur.fetchall()
        cols = [d.name for d in cur.description]
        return [dict(zip(cols, row)) for row in rows]


# ─────────────────────────── credentials ───────────────────────────

async def fetch_credential_data(credential_id: str) -> dict[str, Any] | None:
    """Return the secret key/values of a stored credential, or None if unknown.

    Backs `config.__credentialId` resolution (docs/credentials-contract.md §7).
    Called ONLY from inside activities — the secret values are used for template
    rendering there and never enter workflow payloads or run history.
    """
    if not credential_id:
        return None
    async with _conn() as conn:
        cur = await conn.execute(
            "select data from credentials where id = %s", (credential_id,)
        )
        row = await cur.fetchone()
        if not row:
            return None
        data = row[0]
        return dict(data) if isinstance(data, dict) else {}


# ──────────────────────────── graph fetch ────────────────────────────

async def fetch_graph_nodes(run_id: str) -> list[dict[str, Any]]:
    """Return the GraphNode list the run should execute.

    Resolves the run → its workflow → the workflow's current version → that
    version's `nodes` jsonb. This is the user-composed graph from the visual
    builder (or the prompt compiler): a list of dicts with key/role/type/config/
    dependsOn. Returns [] when the workflow has no version, so the caller can fall
    back to the static reasoning graph.

    Prefer the version the run was launched against (runs.workflow_version_id) so
    run history stays pinned to the graph it actually used; fall back to the
    workflow's current version when the run predates version pinning.
    """
    async with _conn() as conn:
        cur = await conn.execute(
            """
            select coalesce(wv_run.nodes, wv_cur.nodes, '[]'::jsonb)
              from runs r
              join workflows w on w.id = r.workflow_id
              left join workflow_versions wv_run on wv_run.id = r.workflow_version_id
              left join workflow_versions wv_cur on wv_cur.id = w.current_version_id
             where r.id = %s
            """,
            (run_id,),
        )
        row = await cur.fetchone()
        if not row or row[0] is None:
            return []
        nodes = row[0]
        # psycopg returns jsonb already decoded to Python objects.
        return list(nodes) if isinstance(nodes, list) else []


# ─────────────────────────────── runs ───────────────────────────────

async def next_log_seq(run_id: str) -> int:
    async with _conn() as conn:
        cur = await conn.execute(
            "select coalesce(max(seq), -1) + 1 from run_logs where run_id = %s",
            (run_id,),
        )
        row = await cur.fetchone()
        return int(row[0])


async def append_log(
    run_id: str,
    level: str,
    channel: str,
    source: str,
    message: str,
    *,
    detail: str | None = None,
    reasoning: bool = False,
) -> None:
    """Append one append-only log line (seq allocated max+1, like the Go worker)."""
    async with _conn() as conn:
        # Allocate seq and insert in one connection to keep it monotonic.
        cur = await conn.execute(
            "select coalesce(max(seq), -1) + 1 from run_logs where run_id = %s",
            (run_id,),
        )
        seq = int((await cur.fetchone())[0])
        await conn.execute(
            """
            insert into run_logs (id, run_id, seq, offset_ms, level, channel, source, message, detail, reasoning)
            values (%s, %s, %s, 0, %s, %s, %s, %s, %s, %s)
            """,
            (gen_id("log"), run_id, seq, level, channel, source, message, detail, reasoning),
        )


async def set_run_running(run_id: str, current_step: str) -> None:
    async with _conn() as conn:
        await conn.execute(
            """
            update runs
               set status = 'running',
                   started_at = coalesce(started_at, now()),
                   current_step = %s
             where id = %s
            """,
            (current_step, run_id),
        )


async def set_run_progress(run_id: str, steps_done: int, steps_total: int, current_step: str) -> None:
    async with _conn() as conn:
        await conn.execute(
            "update runs set steps_done = %s, steps_total = %s, current_step = %s where id = %s",
            (steps_done, steps_total, current_step, run_id),
        )


async def finish_run(run_id: str, status: str, current_step: str, error: str | None = None) -> None:
    async with _conn() as conn:
        await conn.execute(
            """
            update runs
               set status = %s, finished_at = now(), current_step = %s, error = %s
             where id = %s
            """,
            (status, current_step, error, run_id),
        )


async def add_usage(run_id: str, tokens_in: int, tokens_out: int, cost_usd: float) -> None:
    async with _conn() as conn:
        await conn.execute(
            """
            update runs
               set tokens_in = tokens_in + %s,
                   tokens_out = tokens_out + %s,
                   cost_usd = cost_usd + %s
             where id = %s
            """,
            (tokens_in, tokens_out, cost_usd, run_id),
        )


async def set_langfuse_trace(run_id: str, trace_id: str) -> None:
    async with _conn() as conn:
        await conn.execute(
            "update runs set langfuse_trace_id = %s where id = %s",
            (trace_id, run_id),
        )


# ──────────────────────────── run agents ────────────────────────────

async def upsert_agent(
    run_id: str,
    key: str,
    name: str,
    role: str,
    model: str,
    depends_on_keys: list[str],
    col: int,
    row: int,
) -> str:
    """Insert a run_agent (idempotent on (run_id, key)) and return its id.

    Resume-safe: if the agent already exists (workflow replay re-runs a node), the
    existing id is returned and status is preserved.
    depends_on stores the resolved run-agent ids of the named keys.
    """
    agent_id = _stable_agent_id(run_id, key)
    dep_ids = [_stable_agent_id(run_id, k) for k in depends_on_keys]
    async with _conn() as conn:
        await conn.execute(
            """
            insert into run_agents (id, run_id, name, role, model, status, depends_on, col, "row", summary, metrics)
            values (%s, %s, %s, %s, %s, 'idle', %s, %s, %s, '', '{}'::jsonb)
            on conflict (id) do nothing
            """,
            (agent_id, run_id, name, role, model, dep_ids, col, row),
        )
    return agent_id


async def fetch_agent_statuses(run_id: str) -> dict[str, str]:
    """Return {agent_id: status} for a run's agents.

    Used on resume so an orchestrator can trust nodes already marked `succeeded`
    and avoid redoing their side effects (LLM/browser/http/artifact writes).
    """
    async with _conn() as conn:
        cur = await conn.execute(
            "select id, status from run_agents where run_id = %s", (run_id,)
        )
        rows = await cur.fetchall()
        return {row[0]: row[1] for row in rows}


def _stable_agent_id(run_id: str, key: str) -> str:
    """Deterministic per-(run,node) agent id so replays don't duplicate rows."""
    # run ids look like run_<20>; derive a stable, schema-shaped agent id.
    suffix = run_id.split("_", 1)[-1][:12]
    safe_key = "".join(c for c in key if c in _BASE36)[:6] or "node"
    return f"ra_{suffix}{safe_key}".ljust(23, "0")[:23]


async def set_agent_status(agent_id: str, status: str, summary: str | None = None) -> None:
    async with _conn() as conn:
        if status == "running":
            await conn.execute(
                "update run_agents set status = %s, started_at = coalesce(started_at, now()) where id = %s",
                (status, agent_id),
            )
        elif status in ("succeeded", "failed"):
            await conn.execute(
                "update run_agents set status = %s, finished_at = now(), summary = coalesce(%s, summary) where id = %s",
                (status, summary, agent_id),
            )
        else:
            await conn.execute(
                "update run_agents set status = %s where id = %s", (status, agent_id)
            )


async def add_message(run_id: str, from_agent_id: str, to_agent_id: str, label: str) -> None:
    async with _conn() as conn:
        await conn.execute(
            """
            insert into agent_messages (id, run_id, from_agent_id, to_agent_id, label, at)
            values (%s, %s, %s, %s, %s, now())
            on conflict do nothing
            """,
            (gen_id("msg"), run_id, from_agent_id, to_agent_id, label),
        )


# ───────────────────────────── artifacts ─────────────────────────────

async def add_artifact(run_id: str, name: str, kind: str, produced_by_name: str, preview: str) -> str:
    artifact_id = gen_id("art")
    async with _conn() as conn:
        await conn.execute(
            """
            insert into artifacts (id, run_id, name, kind, produced_by_name, preview, created_at)
            values (%s, %s, %s, %s, %s, %s, now())
            """,
            (artifact_id, run_id, name, kind, produced_by_name, preview),
        )
    return artifact_id


# ────────────────────────── browser sessions ──────────────────────────

async def create_browser_session(run_id: str, agent_name: str, vw: int = 1440, vh: int = 900) -> str:
    sid = gen_id("bs")
    async with _conn() as conn:
        await conn.execute(
            """
            insert into browser_sessions (id, run_id, agent_name, status, current_url, page_title,
                viewport_w, viewport_h, pages_visited, actions_count, started_at)
            values (%s, %s, %s, 'running', '', '', %s, %s, 0, 0, now())
            """,
            (sid, run_id, agent_name, vw, vh),
        )
        await conn.execute("update runs set browser_session_id = %s where id = %s", (sid, run_id))
    return sid


async def record_action(session_id: str, act_type: str, target: str, value: str, status: str, duration_ms: int) -> None:
    async with _conn() as conn:
        await conn.execute(
            """
            insert into browser_actions (id, session_id, ts, type, target, value, status, duration_ms)
            values (%s, %s, now(), %s, %s, %s, %s, %s)
            """,
            (gen_id("ba"), session_id, act_type, target, (value or None), status, duration_ms),
        )
        await conn.execute(
            "update browser_sessions set actions_count = actions_count + 1 where id = %s", (session_id,)
        )


async def navigate(session_id: str, url: str, title: str, duration_ms: int) -> None:
    await record_action(session_id, "navigate", url, "", "ok", duration_ms)
    async with _conn() as conn:
        await conn.execute(
            "update browser_sessions set current_url = %s, page_title = %s, pages_visited = pages_visited + 1 where id = %s",
            (url, title, session_id),
        )


async def record_shot(session_id: str, url: str, title: str, label: str, storage_key: str = "") -> None:
    async with _conn() as conn:
        await conn.execute(
            """
            insert into browser_shots (id, session_id, ts, url, title, label, storage_key)
            values (%s, %s, now(), %s, %s, %s, %s)
            """,
            (gen_id("shot"), session_id, url, title, label, (storage_key or None)),
        )


async def record_console(session_id: str, level: str, text: str) -> None:
    async with _conn() as conn:
        await conn.execute(
            "insert into browser_console (session_id, ts, level, text) values (%s, now(), %s, %s)",
            (session_id, level, text),
        )


async def finish_browser_session(session_id: str, status: str) -> None:
    async with _conn() as conn:
        await conn.execute(
            "update browser_sessions set status = %s, finished_at = now() where id = %s",
            (status, session_id),
        )
