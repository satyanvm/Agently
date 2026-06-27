"""Langfuse tracing for the reasoner.

Each LangGraph node runs as its own Temporal activity (often its own process), so a
single in-process trace can't span all nodes. We therefore group every node's LLM
spans under one Langfuse *session* keyed by the run id, and store that handle on the
run row so the UI can deep-link. Tracing degrades to a no-op when Langfuse isn't
configured — the run still executes and still writes to Postgres.
"""
from __future__ import annotations

from typing import Any

from .config import CONFIG

_client = None
_init_failed = False


def _langfuse():
    """Lazily build a Langfuse client, or return None if unavailable/unconfigured."""
    global _client, _init_failed
    if _client is not None or _init_failed:
        return _client
    if not CONFIG.langfuse_enabled:
        _init_failed = True
        return None
    try:
        from langfuse import Langfuse  # type: ignore

        _client = Langfuse(
            public_key=CONFIG.langfuse_public_key,
            secret_key=CONFIG.langfuse_secret_key,
            host=CONFIG.langfuse_host,
        )
        return _client
    except Exception:
        _init_failed = True
        return None


def langchain_config(run_id: str, node: str, *, model: str | None = None) -> dict[str, Any]:
    """Build a LangChain `config` dict that routes a node's LLM call into Langfuse.

    Returns an empty config (no callbacks) when Langfuse is disabled, so callers can
    pass it unconditionally.
    """
    if _langfuse() is None:
        return {}
    try:
        from langfuse.langchain import CallbackHandler  # type: ignore

        handler = CallbackHandler()
        return {
            "callbacks": [handler],
            "run_name": f"{node}",
            "metadata": {
                "langfuse_session_id": run_id,
                "langfuse_tags": ["agently", "reasoner", node],
                "run_id": run_id,
                **({"ls_model_name": model} if model else {}),
            },
        }
    except Exception:
        return {}


def enabled() -> bool:
    """True only when a Langfuse client is configured and constructed."""
    return _langfuse() is not None


def session_handle(run_id: str) -> str:
    """The value stored in runs.langfuse_trace_id — the Langfuse session id."""
    return run_id


def flush() -> None:
    """Flush buffered spans (best-effort; safe to call when disabled)."""
    client = _langfuse()
    if client is not None:
        try:
            client.flush()
        except Exception:
            pass
