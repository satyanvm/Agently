# Product direction: from signals to approved actions

*Written 2026-10-18. Competitor details come from public sources listed at the
end; check them again before quoting them externally.*

## The bet

Market intelligence and marketing execution are sold as two separate product
categories, to two different budgets. Intelligence tools stop at a dashboard or
a battlecard. Execution tools start from a blank brief. The work in between is
noticing a change, deciding what it means and getting the right response out.
That work is done by hand, in meetings and documents, and for small teams it
mostly doesn't happen.

Agently runs that middle step as a durable workflow:

```text
detect a signal → decide what it means → draft the response
        → a person approves → publish → record what happened
```

"Intelligence tools tell you what happened. Agently does something about it."

## Why us, why now

- **The engine already exists.** Agently compiles a plain-English prompt into a
  typed workflow graph and runs it on Temporal, with 700+ Activepieces
  integrations, credentials and OAuth sign-in, schedules and triggers, and
  Langfuse tracing with per-run cost. A marketing playbook is a workflow graph
  with a signal at the start, AI drafting in the middle and publishing at the
  end. What's missing is the domain layer: snapshots and diffs, approvals,
  brand context and ready-made playbooks.
- **Durability is a product requirement here, not plumbing.** An approval can
  take days. A Temporal workflow waiting on a signal survives restarts and
  deploys without holding a worker, and resumes exactly where it stopped.
- **Traceability is a trust requirement.** A marketer will only let AI draft
  public content if every published item can be traced back: signal → run →
  draft → approver → publish time → cost. We already record per-node outputs,
  run logs, cost and model traces; the approval record links them together.
- **Signals are cheap to collect ourselves.** Competitor pricing, changelog,
  blog and careers pages; GDELT; RSS; Reddit and Hacker News; review sites.
  None of these need a paid vendor, which keeps the price within reach of a
  team of three.

## The landscape

### Intelligence tools: they tell you what happened

| Product | What it does | Where it stops |
|---|---|---|
| **Klue** | Competitive enablement for sales: competitor tracking, battlecards, win/loss. Bought the agentic AI platform Ignition in Sept 2025. | Quote-only annual contracts (third-party estimates put mid-market deals around $20k–$40k a year). Its output is enablement content for sales, not published marketing. |
| **Crayon** | Competitive intelligence: tracks competitor websites, news and reviews, then surfaces and summarises changes. | Same sales-led, quote-only model at a similar price. It hands off at "here's what changed". |
| **Meltwater** | Media and social monitoring for PR and comms teams. | Built around a licensed media feed and PR workflows. Not competitor-specific, not drafting, and expensive. |
| **Brandwatch** (Cision) | Consumer intelligence and social listening at scale. | An analytics and listening product with no loop into approved responses. Enterprise-priced. |
| **AlphaSense** | Market-intelligence search over filings, broker research and expert-call transcripts (it added Tegus in 2024). | Built for finance and strategy research, not marketing execution. |

### Execution tools: they write the response, without the signal

| Product | What it does | Where it stops |
|---|---|---|
| **Jasper** | Marketing-specific AI agents (launched June 2025), with Jasper IQ carrying brand voice and governance, plus Canvas and Grid for scaled workflows. | Starts from a brief a person writes and doesn't watch competitors. Its brand-context layer is the closest thing to our brand context. |
| **HubSpot Breeze** | AI agents inside HubSpot (customer, prospecting, data and content-style agents, paid with HubSpot credits). The lineup changed during 2026. | Strong if you live in HubSpot. It works from your CRM data, not from competitor or market signals. |
| **Typeface** | Enterprise on-brand content generation. In March 2026 it launched a Marketing Orchestration Engine (Arc Graph for brand intelligence, Arc Agents for governed workflows). | Enterprise-first. Governed content production, not a signal-triggered response loop. |
| **Hightouch** | A warehouse-native customer data platform with AI Decisioning: reinforcement-learning agents that pick the message, channel and timing per customer. | Optimises lifecycle messaging to *your* customers from *your* data. No external signals, and it needs a data warehouse. |

### The closest all-in-one: Sprinklr

Sprinklr combines listening, publishing, approval workflows and governance in
one enterprise customer-experience platform. It is the nearest thing to
"signal → approved action" today. It is also built and priced for large
enterprises with dedicated social and care teams, and its signals are mainly
social and customer conversations, not competitor websites.

## Our angle

1. **Signals → approved actions, in one loop.** Others stop at the signal or
   start at the brief. We connect them with an approval step in the middle.
2. **Traceability by default.** Every published item answers which signal
   caused it, which run drafted it, what it cost, who approved it and when it
   went out. Most tools in either category can't connect those facts across
   their boundary.
3. **Built for one to five marketers.** Self-collected signals, ready-made
   playbooks and plain-English customisation, with no data warehouse, no
   licensed media feed and no implementation project.
4. **Honest automation.** AI drafts and a person approves. The only automatic
   actions are explicitly safe ones, such as pausing posts. A failure is
   reported as a failure, never as fake output.

## Risks and open questions

- **Signal quality.** Page diffs are noisy (rotating banners, timestamps, A/B
  tests). If most alerts are noise, users stop reading them. Signal precision
  is a first-class metric (see `product.md`).
- **Collection limits.** Some sites block scrapers or forbid scraping in their
  terms of service. We respect robots.txt and terms, fetch over plain HTTP
  first, and use Browserbase only where JavaScript rendering is needed.
- **Publishing access.** LinkedIn and X require approved developer apps, and X
  write access may need a paid tier. Where a direct integration isn't usable,
  we use the closest working one (Buffer, Typefully, a HubSpot blog draft or a
  Google Doc) and say so. We never fake a publish.
- **Incumbents moving down-market or across.** Klue is adding agents, and
  Jasper and Typeface are adding orchestration. Our defence is the end-to-end
  loop, the traceability, and pricing a small team can pay.
- **Draft quality.** Generic drafts get rejected. Brand context and the
  approval-rate metric are how we find out.

## Sources

- Klue and Crayon pricing and Klue's Ignition acquisition:
  [SaaS Hero, best competitive intelligence tools 2026](https://www.saashero.net/competitor/best-competitive-intelligence-tools-2026/),
  [Toolradar, Klue vs Crayon](https://toolradar.com/compare/klue-vs-crayon)
- Jasper agents, Jasper IQ, Grid:
  [Built In, Jasper](https://builtin.com/company/jasper/faq/innovation-technology-agility)
- HubSpot Breeze agents:
  [HubSpot Knowledge Base](https://knowledge.hubspot.com/ai/use-breeze?tab=suite),
  [INSIDEA](https://insidea.com/blog/hubspot/which-hubspot-breeze-agent)
- Typeface Marketing Orchestration Engine:
  [Typeface profile](https://yespress.io/typeface.md)
- Hightouch AI Decisioning:
  [Hightouch](https://hightouch.com/join/hightouch-ai-decisioning),
  [AdExchanger](https://adexchanger.com/ai/cdp-turned-ai-marketing-platform-hightouch-raises-80-million-in-series-c-funding/)
- Meltwater, Brandwatch, AlphaSense and Sprinklr are described from their
  public product positioning. Check current packaging and pricing on their
  sites.
