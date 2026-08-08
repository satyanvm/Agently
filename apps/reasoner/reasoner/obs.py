"""Langfuse tracing for the reasoner.

Each LangGraph node runs as its own Temporal activity (often its own process), so a
single in-process trace can't span all nodes. We therefore group every node's LLM
spans under one Langfuse *session* keyed by the run id, and store that handle on the
run row so the UI can deep-link.

Tracing is REQUIRED, not best-effort. It used to degrade to a no-op whenever
Langfuse was unconfigured or the client failed to construct, which meant the one
tool for answering "what did this run actually do?" could be silently absent
exactly when a run misbehaved. config.py now requires the keys at boot, and the
two silent `except: return None/{}` paths that used to swallow construction and
callback failures here now raise.
"""
from __future__ import annotations

from typing import Any

from .config import CONFIG

_client = None


def _langfuse():
    """Build the Langfuse client once, or raise with the reason it could not."""
    global _client
    if _client is not None:
        return _client
    try:
        from langfuse import Langfuse  # type: ignore

        _client = Langfuse(
            public_key=CONFIG.langfuse_public_key,
            secret_key=CONFIG.langfuse_secret_key,
            host=CONFIG.langfuse_host,
        )
    except Exception as exc:  # noqa: BLE001 — surface the wiring problem
        raise RuntimeError(
            f"Langfuse client could not be constructed for {CONFIG.langfuse_host}: {exc}"
        ) from exc
    return _client


def langchain_config(run_id: str, node: str, *, model: str | None = None) -> dict[str, Any]:
    """Build a LangChain `config` dict that routes a node's LLM call into Langfuse."""
    _langfuse()  # constructed for its side effect: the handler reads the global client
    try:
        from langfuse.langchain import CallbackHandler  # type: ignore

        handler = CallbackHandler()
    except Exception as exc:  # noqa: BLE001 — an untraced run is not an acceptable run
        raise RuntimeError(f"Langfuse LangChain callback unavailable: {exc}") from exc
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


def session_handle(run_id: str) -> str:
    """The value stored in runs.langfuse_trace_id — the Langfuse session id."""
    return run_id


def flush() -> None:
    """Flush buffered spans. A failure here loses the trace, so it is not swallowed."""
    _langfuse().flush()
