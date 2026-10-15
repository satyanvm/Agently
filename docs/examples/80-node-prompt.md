# Large-graph demo prompt (~80 nodes, no credentials)

Paste this into **New workflow**. Every source is a keyless public API, so no
credentials are needed. Use a Gemini `PLANNER_MODEL` (for example
`gemini-3.5-flash`) — it is written section by section.

```text
Build a detailed "AI agent frameworks landscape" research report. Run it manually.

Track these six frameworks: LangGraph, CrewAI, AutoGen, LlamaIndex, Haystack, Semantic Kernel.

For EACH framework, as its own parallel branch, fetch all of the following (use the keyless
integration nodes, never anything that needs an API key or a browser):
1. its PyPI package info (langgraph, crewai, pyautogen, llama-index, haystack-ai, semantic-kernel),
2. Hacker News search results for the framework name,
3. Reddit search results for the framework name,
4. Stack Overflow questions about it (stackexchange, site stackoverflow),
5. arXiv papers mentioning it (5 results),
6. Semantic Scholar papers mentioning it (5 results),
7. Google News RSS headlines for it.
Then, for each framework, one agent.llm node that writes a profile from those seven results
(latest version and release activity, community buzz, research mentions, news), and a second
agent.llm node that scores community sentiment from the HN, Reddit and Stack Overflow results
as positive / mixed / negative with three supporting quotes.

In parallel, gather market-wide context: Wikipedia summary of "Intelligent agent",
OpenAlex works on "LLM agents" (10), Crossref works on "multi-agent LLM" (10), GDELT news on
"AI agents", the current Lobsters hottest stories and the Hacker News top stories. Give each of
those six sources its own agent.llm node that extracts what is relevant to AI agent frameworks.

Then add five agent.llm comparison nodes, each reading every framework profile and sentiment
score: adoption and release cadence, research momentum, community sentiment, maturity and
documentation, and risks. Add one agent.llm node that combines the market-wide extracts into a
trends section, and one that writes an executive summary from the comparisons and trends,
ending with a recommendation for which framework to pick for which use case.

Finish with an output.report node titled "AI Agent Frameworks Landscape" that contains the
executive summary, the trends section, the five comparisons and all six framework profiles.
```

Expected shape: 1 trigger, 42 per-framework fetches, 12 per-framework agents,
6 market fetches + 6 extract agents, 5 comparisons, trends, executive summary and
the report — 75–80 nodes.
