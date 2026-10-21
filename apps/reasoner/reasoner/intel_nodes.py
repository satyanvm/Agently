"""intel.* node handlers: snapshot pages, diff them, watch competitors, and
record signals (docs/phase-2-plan.md step 2).

Rows land in page_snapshots / signals (migration 0015) scoped to the run's
workspace, with run_id set — that is what lets an approval later trace a
draft back to the change that caused it.

  intel.snapshotPage      fetch one page; store it if it's a new version
  intel.diffPage          compare a snapshot with the version before it;
                          record a page_change signal when it changed
  intel.watchCompetitors  snapshot + diff every registered page of the chosen
                          competitors (the usual first node of a playbook)
  intel.recordSignal      turn news / RSS / Reddit / HN items into signals

A fetch failure fails the node with its reason (robots.txt, HTTP status,
Browserbase). watchCompetitors reports per-page failures in its output and
only fails when every page failed, so one dead careers page doesn't hide a
pricing change.
"""
from __future__ import annotations

import json
import re
import urllib.parse
from typing import Any

from . import browser, db, intel
from .nodes import (
    HTTP_USER_AGENT, NodeContext, NodeResult, _cfg, _clip, _resolve_ref, handles, render, walk_path,
)

SIGNAL_SOURCES = {"page_change", "news", "rss", "reddit", "hackernews", "review", "other"}


def _split(raw: str) -> list[str]:
    return [p.strip() for p in re.split(r"[\n,]+", raw or "") if p.strip()]


def _list_cfg(ctx: NodeContext, key: str) -> list[str]:
    """A list-ish config value: a JSON list (what the planner often writes) or
    a comma / newline separated string (what the builder field holds)."""
    raw = (ctx.node.get("config") or {}).get(key)
    if isinstance(raw, list):
        return [str(render(v, ctx)).strip() for v in raw if str(v).strip()]
    return _split(render(raw or "", ctx))


def _ignore_patterns(ctx: NodeContext) -> list[str]:
    patterns = [p.strip() for p in _cfg(ctx, "ignore").splitlines() if p.strip()]
    for p in patterns:
        try:
            re.compile(p)
        except re.error as exc:
            raise ValueError(f"ignore pattern {p!r} is not a valid regular expression: {exc}") from exc
    return patterns


def _host(url: str) -> str:
    return urllib.parse.urlsplit(url).netloc or url


def _jsonable(row: dict[str, Any]) -> dict[str, Any]:
    return {k: (v.isoformat() if hasattr(v, "isoformat") else v) for k, v in row.items()}


async def _fetch(ctx: NodeContext, url: str, render_mode: str, ignore: list[str]) -> intel.Fetched:
    if render_mode == "browser":
        pages = await browser.browse_pages(ctx.run_id, ctx.label or "Snapshot", [url],
                                           max_chars=intel.MAX_CONTENT_CHARS)
        final_url, title, text = pages[0]
        return intel.Fetched(url=final_url, status=0, title=title, text=intel.normalize_text(text, ignore))
    if render_mode != "http":
        raise ValueError(f"render must be http or browser, not {render_mode!r}")
    return await intel.fetch_http(url, HTTP_USER_AGENT, ignore)


async def snapshot_page(ctx: NodeContext, workspace_id: str, url: str, *, competitor_id: str | None,
                        page_kind: str | None, render_mode: str, ignore: list[str]) -> dict[str, Any]:
    """Fetch `url` and store it when it differs from the latest stored version.

    The page's identity is the URL as configured (not the post-redirect URL),
    so a redirect on the competitor's side doesn't restart its history."""
    fetched = await _fetch(ctx, url, render_mode, ignore)
    if not fetched.text.strip():
        raise RuntimeError(f"{url} has no visible text after normalization; "
                           "if it only renders with JavaScript, set render to browser")
    digest = intel.content_hash(fetched.text)
    latest = (await db.latest_snapshots(workspace_id, url, 1) or [None])[0]
    if latest and latest["content_hash"] == digest:
        await db.touch_snapshot(latest["id"])
        snap, new_version = latest, False
    else:
        snap = await db.insert_snapshot(
            workspace_id, competitor_id=competitor_id, page_kind=page_kind, url=url, run_id=ctx.run_id,
            fetched_via=render_mode, status_code=fetched.status or None, title=fetched.title,
            content_hash=digest, content=fetched.text,
        )
        new_version = True
    await db.append_log(
        ctx.run_id, "info", "system", ctx.node["key"],
        f"Snapshot {url}: {'new version' if new_version and latest else 'baseline' if new_version else 'unchanged'}",
    )
    return {
        "snapshotId": snap["id"], "url": url, "title": fetched.title, "fetchedVia": render_mode,
        "newVersion": new_version, "first": latest is None, "contentHash": digest,
        "text": fetched.text[:20_000],
    }


async def diff_snapshot(ctx: NodeContext, workspace_id: str, snapshot_id: str, *, record_signal: bool,
                        competitor_name: str = "") -> dict[str, Any]:
    """Compare a snapshot with the version before it.

    `changed` is true only when the snapshot was created by THIS run: an old
    change that was already reported is not reported again."""
    snap = await db.snapshot_by_id(snapshot_id)
    if not snap or snap["workspace_id"] != workspace_id:
        raise RuntimeError(f"snapshot {snapshot_id} not found in this workspace")
    prev = await db.snapshot_before(workspace_id, snap["url"], snap["fetched_at"])
    base = {
        "url": snap["url"], "pageKind": snap["page_kind"] or "", "snapshotId": snap["id"],
        "previousSnapshotId": prev["id"] if prev else "", "signalId": "",
        "added": [], "removed": [], "addedCount": 0, "removedCount": 0,
    }
    new_in_run = snap["run_id"] == ctx.run_id
    if prev is None and new_in_run:
        return {**base, "changed": False, "first": True, "summary": "First snapshot (baseline); nothing to compare yet"}
    if prev is None or not new_in_run:
        return {**base, "changed": False, "first": False, "summary": "No change since the last snapshot"}

    d = intel.diff_text(prev["content"], snap["content"])
    out = {**base, "changed": d.changed, "first": False, "summary": d.summary(), "added": d.added,
           "removed": d.removed, "addedCount": d.added_count, "removedCount": d.removed_count}
    if d.changed and record_signal:
        who = competitor_name or _host(snap["url"])
        kind = f"{snap['page_kind']} page" if snap["page_kind"] else "page"
        signal_id, _ = await db.insert_signal(
            workspace_id, source="page_change", title=f"{who} {kind} changed", run_id=ctx.run_id,
            summary=d.summary(), url=snap["url"], competitor_id=snap["competitor_id"],
            snapshot_id=snap["id"], previous_snapshot_id=prev["id"],
            detail={"added": d.added, "removed": d.removed, "addedCount": d.added_count,
                    "removedCount": d.removed_count, "pageKind": snap["page_kind"] or "",
                    "competitor": competitor_name},
            # One signal per page version, however many runs diff it.
            dedupe_key=f"page_change:{snap['id']}",
        )
        out["signalId"] = signal_id
    return out


def _find_competitor(competitors: list[dict[str, Any]], ref: str) -> dict[str, Any]:
    for c in competitors:
        if c["id"] == ref or c["name"].strip().lower() == ref.strip().lower():
            return c
    names = ", ".join(c["name"] for c in competitors) or "none registered"
    raise ValueError(f"no competitor named {ref!r} (registered: {names})")


# ─────────────────────────── handlers ───────────────────────────

@handles("intel.snapshotPage")
async def _snapshot(ctx: NodeContext) -> NodeResult:
    url = render(_cfg(ctx, "url"), ctx).strip()
    if not url:
        raise ValueError("intel.snapshotPage requires a URL")
    ws = await db.run_workspace_id(ctx.run_id)
    competitor_id: str | None = None
    if ref := render(_cfg(ctx, "competitor"), ctx).strip():
        competitor_id = _find_competitor(await db.list_competitors(ws), ref)["id"]
    out = await snapshot_page(
        ctx, ws, url, competitor_id=competitor_id, page_kind=_cfg(ctx, "pageKind") or None,
        render_mode=_cfg(ctx, "render") or "http", ignore=_ignore_patterns(ctx),
    )
    state = "baseline" if out["first"] else "new version" if out["newVersion"] else "unchanged"
    return NodeResult(output=out, summary=f"{url}: {state}")


@handles("intel.diffPage")
async def _diff(ctx: NodeContext) -> NodeResult:
    ws = await db.run_workspace_id(ctx.run_id)
    snapshot_id = render(_cfg(ctx, "snapshotId"), ctx).strip()
    if not snapshot_id:
        url = render(_cfg(ctx, "url"), ctx).strip()
        if not url:
            raise ValueError("intel.diffPage requires snapshotId or url")
        latest = await db.latest_snapshots(ws, url, 1)
        if not latest:
            raise RuntimeError(f"no snapshot of {url} yet; snapshot it first (intel.snapshotPage)")
        snapshot_id = latest[0]["id"]
    out = await diff_snapshot(ctx, ws, snapshot_id,
                              record_signal=(_cfg(ctx, "recordSignal") or "true").lower() != "false")
    return NodeResult(output=out, summary=f"{out['url']}: {out['summary']}")


@handles("intel.watchCompetitors")
async def _watch(ctx: NodeContext) -> NodeResult:
    ws = await db.run_workspace_id(ctx.run_id)
    registered = await db.list_competitors(ws)
    if not registered:
        raise RuntimeError("no competitors registered; add them on the Competitors page first")
    refs = _list_cfg(ctx, "competitors")
    chosen = [_find_competitor(registered, r) for r in refs] if refs else registered
    kinds = {k.lower() for k in _list_cfg(ctx, "pageKinds")}
    ignore = _ignore_patterns(ctx)

    changes: list[dict[str, Any]] = []
    failed: list[dict[str, Any]] = []
    checked = baselines = 0
    for comp in chosen:
        pages = comp["pages"] if isinstance(comp["pages"], list) else json.loads(comp["pages"] or "[]")
        for page in pages:
            kind, url = str(page.get("kind") or "other"), str(page.get("url") or "")
            if not url or (kinds and kind not in kinds):
                continue
            checked += 1
            try:
                snap = await snapshot_page(ctx, ws, url, competitor_id=comp["id"], page_kind=kind,
                                           render_mode=str(page.get("render") or "http"), ignore=ignore)
                d = await diff_snapshot(ctx, ws, snap["snapshotId"], record_signal=True,
                                        competitor_name=comp["name"])
            except Exception as exc:  # noqa: BLE001 - reported per page, below
                failed.append({"competitor": comp["name"], "pageKind": kind, "url": url, "error": str(exc)})
                await db.append_log(ctx.run_id, "warn", "system", ctx.node["key"],
                                    f"Could not check {comp['name']} {kind} ({url}): {exc}")
                continue
            if d["first"]:
                baselines += 1
            if d["changed"]:
                changes.append({"competitor": comp["name"], "competitorId": comp["id"], **d})

    if checked == 0:
        raise RuntimeError("the chosen competitors have no pages"
                           + (f" of kind {', '.join(sorted(kinds))}" if kinds else ""))
    if len(failed) == checked:
        raise RuntimeError("every page failed: " + "; ".join(f"{f['url']}: {f['error']}" for f in failed))

    lines = [f"## {c['competitor']} — {c['pageKind'] or 'page'} ({c['url']})\n{c['summary']}\n"
             + "".join(f"+ {a}\n" for a in c["added"][:40]) + "".join(f"- {r}\n" for r in c["removed"][:40])
             for c in changes]
    digest = "\n".join(lines) if lines else "No competitor page changed since the last check."
    summary = (f"{len(changes)} change(s) across {checked} page(s)"
               + (f", {baselines} baseline(s)" if baselines else "")
               + (f", {len(failed)} failed" if failed else ""))
    return NodeResult(
        output={"changed": bool(changes), "changes": changes, "checked": checked, "baselines": baselines,
                "failed": failed, "text": digest, "signalIds": [c["signalId"] for c in changes if c["signalId"]]},
        summary=summary,
    )


@handles("intel.recordSignal")
async def _record_signal(ctx: NodeContext) -> NodeResult:
    ws = await db.run_workspace_id(ctx.run_id)
    source = _cfg(ctx, "source") or "other"
    if source not in SIGNAL_SOURCES:
        raise ValueError(f"source must be one of {', '.join(sorted(SIGNAL_SOURCES))}")
    competitor_id = None
    if ref := render(_cfg(ctx, "competitor"), ctx).strip():
        competitor_id = _find_competitor(await db.list_competitors(ws), ref)["id"]

    entries: list[tuple[str, str, str, dict[str, Any]]] = []  # title, url, summary, detail
    if expr := _cfg(ctx, "items").strip():
        items = _resolve_ref(expr, ctx)
        if not isinstance(items, list):
            raise ValueError(f"items {expr!r} is not a list")
        fields = {f: _cfg(ctx, f"{f}Field") for f in ("title", "url", "summary")}
        if not fields["title"]:
            raise ValueError("titleField is required with items (the path to each item's title)")

        def pick(item: Any, path: str) -> str:
            if not path:
                return ""
            val = walk_path(item, path, "item field")
            return val if isinstance(val, str) else json.dumps(val)

        for item in items:
            detail = item if isinstance(item, dict) else {"value": item}
            entries.append((pick(item, fields["title"]), pick(item, fields["url"]),
                            pick(item, fields["summary"]), detail))
    else:
        title = render(_cfg(ctx, "title"), ctx).strip()
        if not title:
            raise ValueError("intel.recordSignal requires a title (or items + titleField)")
        entries.append((title, render(_cfg(ctx, "url"), ctx).strip(), render(_cfg(ctx, "summary"), ctx), {}))

    ids: list[str] = []
    created = 0
    for title, url, summary, detail in entries:
        if not title.strip():
            continue
        # The same article or post seen on two runs is one signal.
        key = f"{source}:{url or title.strip().lower()}"
        sid, is_new = await db.insert_signal(ws, source=source, title=_clip(title, 300), run_id=ctx.run_id,
                                             summary=summary, url=url, detail=detail,
                                             competitor_id=competitor_id, dedupe_key=key)
        ids.append(sid)
        created += int(is_new)
    return NodeResult(
        output={"signalIds": ids, "count": len(ids), "created": created, "duplicates": len(ids) - created},
        summary=f"{created} new signal(s), {len(ids) - created} already recorded",
    )
