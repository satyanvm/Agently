"""Claude completions for the reasoner, instrumented with Langfuse.

There is one provider and one credential: ANTHROPIC_API_KEY. Errors are raised
deliberately — an agent that could not call its model must never be marked
successful carrying a mock answer. The predecessor of this module returned a
deterministic `[mock:<node>] <echo of the prompt>` completion both when no key was
set AND on any exception, which meant a broken install produced runs that looked
exactly like working ones.

This goes through LangChain rather than the raw Messages API because
obs.langchain_config() hands back a LangChain callback config, and that is how a
node's spans reach Langfuse.
"""
from __future__ import annotations

from dataclasses import dataclass

from . import obs
from .config import CONFIG

# USD per 1K tokens, by model. Claude list prices are quoted per million, so these
# are that figure / 1000: Opus 4.8 $5/$25, Sonnet 4.6 $3/$15, Haiku 4.5 $1/$5.
_RATES: dict[str, tuple[float, float]] = {
    "claude-opus-4-8": (0.005, 0.025),
    "claude-opus-4-7": (0.005, 0.025),
    "claude-opus-4-6": (0.005, 0.025),
    "claude-sonnet-4-6": (0.003, 0.015),
    "claude-haiku-4-5": (0.001, 0.005),
}
# An unknown model is priced at the most expensive tier we know of, so a mis-set
# REASONER_MODEL over-reports cost rather than under-reporting it.
_FALLBACK_RATE = (0.005, 0.025)

# Runtime nodes are asked for prose and JSON, not essays. Generous enough that
# hitting this cap means the prompt wanted something unreasonable.
_MAX_TOKENS = 4096


@dataclass
class Completion:
    text: str
    tokens_in: int
    tokens_out: int
    cost_usd: float


def _estimate_cost(model: str, tin: int, tout: int) -> float:
    rate_in, rate_out = _RATES.get(model, _FALLBACK_RATE)
    return (tin / 1000.0) * rate_in + (tout / 1000.0) * rate_out


async def complete(
    run_id: str, node: str, system: str, user: str, *, model: str | None = None
) -> Completion:
    if not CONFIG.anthropic_api_key:
        raise RuntimeError("ANTHROPIC_API_KEY is required for reasoning; set it in .env")
    model = model or CONFIG.model

    from langchain_anthropic import ChatAnthropic  # imported lazily
    from langchain_core.messages import HumanMessage, SystemMessage

    # No temperature: Opus 4.7+ rejects temperature/top_p/top_k with a 400.
    llm = ChatAnthropic(
        model=model,
        anthropic_api_key=CONFIG.anthropic_api_key,
        max_tokens=_MAX_TOKENS,
    )
    config = obs.langchain_config(run_id, node, model=model)
    try:
        resp = await llm.ainvoke(
            [SystemMessage(content=system), HumanMessage(content=user)], config=config
        )
    except Exception as exc:  # noqa: BLE001 — re-raise carrying the provider's reason
        raise RuntimeError(f"Claude {model} request failed: {exc}") from exc

    text = resp.content if isinstance(resp.content, str) else str(resp.content)
    if not text.strip():
        raise RuntimeError(f"Claude {model} returned empty text")
    usage = getattr(resp, "usage_metadata", None) or {}
    tin = int(usage.get("input_tokens", 0))
    tout = int(usage.get("output_tokens", 0))
    return Completion(
        text=text, tokens_in=tin, tokens_out=tout, cost_usd=_estimate_cost(model, tin, tout)
    )
