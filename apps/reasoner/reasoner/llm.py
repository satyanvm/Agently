"""Gemini completions for the reasoner, instrumented with Langfuse.

There is one provider and one credential: GEMINI_API_KEY. Errors are raised
deliberately — an agent that could not call its model must never be marked
successful carrying a mock answer. The predecessor of this module returned a
deterministic `[mock:<node>] <echo of the prompt>` completion both when no key was
set AND on any exception, which meant a broken install produced runs that looked
exactly like working ones.

This goes through LangChain rather than the raw generateContent API because
obs.langchain_config() hands back a LangChain callback config, and that is how a
node's spans reach Langfuse.
"""
from __future__ import annotations

from dataclasses import dataclass

from . import obs
from .config import CONFIG, is_ollama_model

# USD per 1K tokens, by model. Gemini list prices are quoted per million, so these
# are that figure / 1000: 2.5 Pro $1.25/$10 (≤200k prompt), 2.5 Flash $0.30/$2.50,
# 2.5 Flash-Lite $0.10/$0.40.
_RATES: dict[str, tuple[float, float]] = {
    "gemini-2.5-pro": (0.00125, 0.010),
    "gemini-2.5-flash": (0.0003, 0.0025),
    "gemini-2.5-flash-lite": (0.0001, 0.0004),
    # 3.x list prices are not tabulated here yet; priced at the flash tier so the
    # estimate stays in the right order of magnitude rather than over-reporting
    # at the pro fallback.
    "gemini-3.5-flash": (0.0003, 0.0025),
    "gemini-3.5-flash-lite": (0.0001, 0.0004),
    "gemini-3.1-flash-lite": (0.0001, 0.0004),
}
# An unknown model is priced at the most expensive tier we know of, so a mis-set
# REASONER_MODEL over-reports cost rather than under-reporting it.
_FALLBACK_RATE = (0.00125, 0.010)

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
    if is_ollama_model(model):
        return 0.0  # local inference has no per-token price
    rate_in, rate_out = _RATES.get(model, _FALLBACK_RATE)
    return (tin / 1000.0) * rate_in + (tout / 1000.0) * rate_out


def _content_text(content: object) -> str:
    """Flatten a LangChain message content to plain text.

    Gemini 3 replies arrive as a list of content blocks ({"type": "text", ...},
    plus thinking/signature blocks), not a bare string. str() on that list put a
    Python repr into every downstream node and report, so only the text blocks
    are kept, in order.
    """
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for block in content:
            if isinstance(block, str):
                parts.append(block)
            elif isinstance(block, dict) and block.get("type") == "text":
                parts.append(str(block.get("text", "")))
        return "".join(parts)
    return str(content)


def _chat_model(model: str):
    """Build the LangChain chat model for `model`, choosing the provider by name."""
    if is_ollama_model(model):
        from langchain_ollama import ChatOllama  # imported lazily

        # reasoning=False: qwen3-style thinking multiplies latency on a laptop
        # and its trace would otherwise leak into the node's text output.
        return ChatOllama(
            model=model.removeprefix("ollama/"),
            base_url=CONFIG.ollama_host,
            num_ctx=CONFIG.ollama_num_ctx,
            num_predict=_MAX_TOKENS,
            reasoning=False,
        )
    if not CONFIG.gemini_api_key:
        raise RuntimeError(
            f"GEMINI_API_KEY is required for model {model!r}; set it in .env "
            "or use a local ollama/<model>"
        )
    from langchain_google_genai import ChatGoogleGenerativeAI  # imported lazily

    return ChatGoogleGenerativeAI(
        model=model,
        google_api_key=CONFIG.gemini_api_key,
        max_output_tokens=_MAX_TOKENS,
    )


async def complete(
    run_id: str, node: str, system: str, user: str, *, model: str | None = None
) -> Completion:
    model = model or CONFIG.model

    from langchain_core.messages import HumanMessage, SystemMessage

    llm = _chat_model(model)
    provider = "Ollama" if is_ollama_model(model) else "Gemini"
    config = obs.langchain_config(run_id, node, model=model)
    try:
        resp = await llm.ainvoke(
            [SystemMessage(content=system), HumanMessage(content=user)], config=config
        )
    except Exception as exc:  # noqa: BLE001 — re-raise carrying the provider's reason
        raise RuntimeError(f"{provider} {model} request failed: {exc}") from exc

    text = _content_text(resp.content)
    if not text.strip():
        raise RuntimeError(f"{provider} {model} returned empty text")
    usage = getattr(resp, "usage_metadata", None) or {}
    tin = int(usage.get("input_tokens", 0))
    tout = int(usage.get("output_tokens", 0))
    return Completion(
        text=text, tokens_in=tin, tokens_out=tout, cost_usd=_estimate_cost(model, tin, tout)
    )
