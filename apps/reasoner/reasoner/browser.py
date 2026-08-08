"""Browserbase infrastructure for browser nodes.

Browser work is never simulated. Missing Browserbase configuration and provider
errors propagate to the node so the run fails with the real reason.
"""
from __future__ import annotations

import time
from typing import Any

import httpx

from . import db
from .config import CONFIG

_BROWSERBASE_SESSIONS = "https://api.browserbase.com/v1/sessions"


async def _create_browserbase_session() -> dict[str, Any]:
    async with httpx.AsyncClient(timeout=30) as client:
        resp = await client.post(
            _BROWSERBASE_SESSIONS,
            headers={"x-bb-api-key": CONFIG.browserbase_api_key, "Content-Type": "application/json"},
            json={"projectId": CONFIG.browserbase_project_id},
        )
        resp.raise_for_status()
        return resp.json()


async def run_browse(run_id: str, label: str, urls: list[str], *, max_chars: int = 4000) -> str:
    """Visit each url, extract visible text, persist the session, return findings."""
    if not urls:
        raise RuntimeError("browser node has no URLs to visit")
    if not CONFIG.browserbase_enabled:
        raise RuntimeError(
            "BROWSERBASE_API_KEY and BROWSERBASE_PROJECT_ID are required for browser nodes"
        )
    session_id = await db.create_browser_session(run_id, label)
    await db.record_console(session_id, "info", "session started (browserbase)")

    findings: list[str] = []
    ok = True
    try:
        findings = await _browse_real(run_id, session_id, urls, max_chars)
    except Exception as exc:  # noqa: BLE001 - persist failure, then propagate it
        ok = False
        await db.record_console(session_id, "error", f"browse failed: {exc}")
        await db.append_log(
            run_id, "error", "browser", "browser", f"Browse failed: {exc}", reasoning=False
        )
        raise RuntimeError(f"Browserbase browse failed: {exc}") from exc
    finally:
        await db.finish_browser_session(session_id, "succeeded" if ok else "failed")
    return "\n\n".join(findings)


async def _browse_real(run_id: str, session_id: str, urls: list[str], max_chars: int) -> list[str]:
    from playwright.async_api import async_playwright  # imported lazily

    session = await _create_browserbase_session()
    connect_url = session.get("connectUrl") or session.get("connect_url")
    await db.append_log(run_id, "info", "browser", "browser", f"Browserbase session {session.get('id','')}")

    out: list[str] = []
    async with async_playwright() as pw:
        browser = await pw.chromium.connect_over_cdp(connect_url)
        context = browser.contexts[0] if browser.contexts else await browser.new_context()
        page = context.pages[0] if context.pages else await context.new_page()
        for url in urls:
            t0 = time.monotonic()
            await page.goto(url, wait_until="domcontentloaded", timeout=45000)
            title = await page.title()
            dur = int((time.monotonic() - t0) * 1000)
            await db.navigate(session_id, url, title, dur)
            await db.record_shot(session_id, url, title, "after navigate")
            text = (await page.inner_text("body"))[:max_chars]
            await db.record_action(session_id, "extract", url, "body text", "ok", 0)
            out.append(f"# {title} ({url})\n{text}")
        await browser.close()
    return out
