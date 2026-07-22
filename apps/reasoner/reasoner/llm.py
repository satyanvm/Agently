"""LLM calls via LangChain ChatGoogleGenerativeAI (Gemini), instrumented with Langfuse.

Real Gemini when GEMINI_API_KEY (or GOOGLE_API_KEY) is set, otherwise a deterministic
mock so the slice runs keyless. Returns the text plus token usage + an estimated cost,
which the caller rolls up onto the run. No third-party proxy: the Gemini SDK talks
directly to Google's generativelanguage endpoint.
"""
from __future__ import annotations

from dataclasses import dataclass

from . import obs
from .config import CONFIG

# Gemini 2.5 Flash rates (USD per 1K tokens) — a rough estimate for the run's cost
# display; exact billing depends on the chosen model and context length.
_RATE_IN = 0.0003
_RATE_OUT = 0.0025


@dataclass
class Completion:
    text: str
    tokens_in: int
    tokens_out: int
    cost_usd: float


def _estimate_cost(tin: int, tout: int) -> float:
    return (tin / 1000.0) * _RATE_IN + (tout / 1000.0) * _RATE_OUT


async def complete(run_id: str, node: str, system: str, user: str, *, model: str | None = None) -> Completion:
    model = model or CONFIG.model
    if not CONFIG.gemini_api_key:
        return _mock(node, user)
    try:
        from langchain_google_genai import ChatGoogleGenerativeAI  # imported lazily
        from langchain_core.messages import HumanMessage, SystemMessage

        llm = ChatGoogleGenerativeAI(model=model, google_api_key=CONFIG.gemini_api_key, max_tokens=2048, temperature=0.3)
        config = obs.langchain_config(run_id, node, model=model)
        resp = await llm.ainvoke(
            [SystemMessage(content=system), HumanMessage(content=user)], config=config
        )
        usage = getattr(resp, "usage_metadata", None) or {}
        tin = int(usage.get("input_tokens", 0))
        tout = int(usage.get("output_tokens", 0))
        text = resp.content if isinstance(resp.content, str) else str(resp.content)
        return Completion(text=text, tokens_in=tin, tokens_out=tout, cost_usd=_estimate_cost(tin, tout))
    except Exception as exc:  # noqa: BLE001 — degrade to mock, never fail the run on LLM wiring
        return _mock(node, f"[llm error: {exc}]\n\n{user}")


def _mock(node: str, user: str) -> Completion:
    """Deterministic, keyless fallback — echoes the salient input back."""
    snippet = user.strip().replace("\n", " ")[:600]
    text = f"[mock:{node}] {snippet}"
    tin = max(1, len(user) // 4)
    tout = max(1, len(text) // 4)
    return Completion(text=text, tokens_in=tin, tokens_out=tout, cost_usd=_estimate_cost(tin, tout))
