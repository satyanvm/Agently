"""Refresh expired OAuth access tokens for catalog (http-runtime) nodes.

Sign-in credentials (apps/api oauth_service.go) store `expires_at` (unix
seconds), `refresh_token`, `token_url` and, for catalog types, `token_key` —
the field the node template reads (e.g. HUBSPOT_ACCESS_TOKEN). The pieces
worker refreshes its own (src/credentials.ts makeOAuthRefresher); this is the
same logic for nodes the reasoner runs itself.

Refreshed tokens are cached in memory per credential id; the stored row is
sealed by the API and left untouched. A failed refresh returns the stored
values unchanged, so the provider's own 401 becomes the run error.
"""

from __future__ import annotations

import base64
import os
import time
from typing import Any, Awaitable, Callable

import httpx

_cache: dict[str, dict[str, Any]] = {}

Post = Callable[[str, dict[str, str], dict[str, str]], Awaitable[dict[str, Any] | None]]


def oauth_client(cred_type: str, env: os._Environ[str] | dict[str, str] = os.environ) -> tuple[str, str]:
    """Same env naming as the API: hubspot / pieces.hubspot → OAUTH_HUBSPOT_CLIENT_ID."""
    slug = cred_type.removeprefix("pieces.")
    key = slug.upper().replace("-", "_").replace(".", "_")
    cid = env.get(f"OAUTH_{key}_CLIENT_ID", "")
    sec = env.get(f"OAUTH_{key}_CLIENT_SECRET", "")
    if (not cid or not sec) and (slug.startswith("google") or slug in ("gmail", "youtube")):
        cid, sec = env.get("GOOGLE_CLIENT_ID", ""), env.get("GOOGLE_CLIENT_SECRET", "")
    elif (not cid or not sec) and slug.startswith("microsoft"):
        cid, sec = env.get("MICROSOFT_CLIENT_ID", ""), env.get("MICROSOFT_CLIENT_SECRET", "")
    return cid, sec


async def _post(url: str, form: dict[str, str], headers: dict[str, str]) -> dict[str, Any] | None:
    async with httpx.AsyncClient(timeout=20) as client:
        resp = await client.post(url, data=form, headers=headers)
    if resp.status_code // 100 != 2:
        return None
    return resp.json()


async def refresh_if_expired(
    cred_id: str,
    cred_type: str,
    data: dict[str, Any],
    *,
    post: Post = _post,
    now: Callable[[], float] = time.time,
    env: os._Environ[str] | dict[str, str] = os.environ,
) -> dict[str, Any]:
    expires_at = float(data.get("expires_at") or 0)
    refresh_token = data.get("refresh_token") or ""
    token_url = data.get("token_url") or ""
    if not expires_at or not refresh_token or not token_url:
        return data
    token_key = data.get("token_key") or "access_token"

    def merged(entry: dict[str, Any]) -> dict[str, Any]:
        out = {**data, **entry["extra"], "access_token": entry["access_token"], "expires_at": entry["expires_at"]}
        out[token_key] = entry["access_token"]
        return out

    cached = _cache.get(cred_id)
    if cached and cached["expires_at"] - 60 > now():
        return merged(cached)
    if expires_at - 60 > now():
        return data

    cid, sec = oauth_client(cred_type, env)
    if not cid or not sec:
        return data
    form = {"grant_type": "refresh_token", "refresh_token": refresh_token}
    headers = {"accept": "application/json"}
    if data.get("authorization_method") == "header":
        headers["authorization"] = "Basic " + base64.b64encode(f"{cid}:{sec}".encode()).decode()
    else:
        form["client_id"], form["client_secret"] = cid, sec
    try:
        tok = await post(token_url, form, headers)
    except Exception:  # noqa: BLE001 - fall back to the stored token; the provider's 401 explains
        return data
    access = (tok or {}).get("access_token")
    if not access:
        return data
    entry = {
        "access_token": access,
        "expires_at": now() + float(tok.get("expires_in") or 3600),
        # Providers that rotate refresh tokens hand back a new one.
        "extra": {"refresh_token": tok["refresh_token"]} if tok.get("refresh_token") else {},
    }
    _cache[cred_id] = entry
    return merged(entry)
