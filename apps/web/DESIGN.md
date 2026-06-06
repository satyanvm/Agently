# Agently — Frontend Design System & Architecture

The control plane for long-running, autonomous AI agents. **Light-mode, white
UI with an indigo-purple accent** (inspired by Relevance AI). This document
covers the design system, information architecture, and frontend architecture
that the `@agently/web` app implements.

> The product is **the platform agents run on** — not an agent. Every screen is
> built to *observe and operate* autonomous work, not to chat with a model.

---

## 1. Design principles

Premium · Minimal · Professional · AI-native · Light-mode (clean white) ·
Fast · Information-dense without feeling crowded.

Reference bar: Claude.ai, Linear, Vercel, Cursor, Raycast.

Concretely this means: hairline borders over heavy cards, one electric-iris
accent used sparingly, a precise functional-status palette, tabular numerals for
all metrics, monospace for logs/URLs/IDs, generous-but-tight spacing, and motion
reserved for *live* state (breathing dots, flowing edges, shimmering progress).

## 2. Color system (`app/globals.css`, Tailwind v4 `@theme`)

Light-first: white canvas, soft hairline borders, one vivid indigo-purple accent
(Relevance-inspired), and contrast-tuned functional status colors.

| Token | Hex | Role |
|---|---|---|
| `bg` | `#f8f8fb` | app canvas (near-white) |
| `surface` | `#ffffff` | cards / panels |
| `surface-2` | `#f3f3f8` | elevated / hover |
| `surface-3` | `#e9e9f2` | active / popover |
| `inset` | `#f5f5f9` | log & terminal wells |
| `border` | `rgba(17,18,38,.09)` | hairlines |
| `border-strong` | `rgba(17,18,38,.16)` | hover hairlines |
| `fg / muted / faint / ghost` | `#0b0e1a → #b6bac8` | text ramp |
| `accent` | `#4f39e6` | indigo-purple (buttons, brand) |
| `accent-soft` | `#6f5cf5` | lighter purple (hover, highlight text) |
| `success` | `#15a34a` | succeeded |
| `running` | `#2563eb` | running / live |
| `warn` | `#c2740a` | paused / warnings |
| `danger` | `#dc2649` | failed / errors |

Each status also has a `-bg` soft tint. A barely-there indigo top wash
(`body::before`) adds depth on white. Status → color is centralized in
`components/ui/status.tsx`. Colored icon chips (agent roles, artifacts, browser
actions) use `-600` shades over pale `/10` tints; log/terminal syntax uses `-600`
shades on the light inset surface.

## 3. Typography

- **Geist Sans** (UI) and **Geist Mono** (logs, URLs, IDs, metrics) via `next/font`.
- Scale: display 44–64px (landing) · page title 20–22px · section 13px semibold ·
  body 13–14px · meta 11–12px · code 11–12px.
- Tight tracking (`-0.011em`) on UI; `tabular-nums` everywhere numbers update.

## 4. Layout system

- **Fixed 244px sidebar** + fluid content (`app/(app)/layout.tsx`).
- **Sticky 56px top bar** with breadcrumbs, ⌘K search, notifications, avatar.
- Content max-width `1240px`, 24px gutters (`PageContainer`).
- Dense grids: KPI strips use `gap-px` over a `bg-border` to fake hairline
  dividers; detail screens use a `1.6–1.7fr / 1fr` main+rail split.

## 5. Component inventory

**Primitives** (`components/ui/`): Button (6 variants), Card, Badge, StatusBadge
/ StatusDot, Avatar, Progress, Sparkline / BarChart / HealthBar, Segmented,
Input / SearchInput, Switch, Kbd, Stat.

**Domain components** (`components/`):
- `agent-glyph` — role → icon + tinted ring
- `agent-graph` — SVG dependency graph w/ animated hand-off edges
- `agent-visualization` — graph + communication feed + dependency view (Page 6)
- `log-viewer` — realtime, searchable, filterable streaming log (Page 5)
- `browser-session` — viewport + filmstrip scrubber + actions/console (Page 7)
- `run-detail` — the hero run screen w/ tabs (Page 4)
- `workflow-detail` — graph + statuses + execution timeline (Page 3)
- `run-row` / `workflow-row` — list & dashboard rows
- `artifact-list`, `live` (LiveTimer / LiveCost / LivePill)

**Shell** (`components/shell/`): Sidebar, TopBar, CommandPalette (⌘K), PageContainer.

## 6. Navigation & information architecture

```
/                         Landing
/dashboard                Overview: active runs, health, runtime, results, activity
/workflows                Workflow library (filter/search)
  /workflows/[slug]       Workflow view: graph, agent statuses, execution timeline
/runs                     Run history (filter/search)
  /runs/[id]              ★ Run view (hero) — Overview · Logs · Agents · Browser · Artifacts
/agents                   Agent library
/browser                  Browser sessions index
  /browser/[id]           Browser session replay
/notifications            Notification center + delivery preferences
/settings                 General · Billing · Members · API keys · Compute
```

The **Run view** is the center of gravity; Pages 5 (logs), 6 (agent graph) and 7
(browser) are delivered both as standalone routes and as tabs within it.

## 7. Frontend architecture

- **Next.js 15 App Router**, TypeScript (strict), Tailwind v4, shadcn-style
  hand-authored primitives. No runtime data layer — everything is mock.
- **Server components by default**; client components only where there is
  interaction (filters, tabs, command palette, live tickers, graph selection).
- **Mock data** lives in `lib/mock-data.ts`, typed by `lib/types.ts` (which
  extends `@agently/core`). A fixed `NOW` keeps relative times deterministic
  across SSR/CSR; "live" values only advance after mount to avoid hydration drift.
- **Realtime is simulated** — the log viewer appends synthetic lines on an
  interval; `LiveTimer`/`LiveCost` tick client-side; progress bars shimmer.

## 8. Run / build

```bash
pnpm --filter @agently/web dev     # local dev
pnpm --filter @agently/web build   # production build (25 static/SSG routes)
```

Flagship demo data centers on **Competitive Intelligence Sweep #142** — a live
7-agent run with full logs, an active browser session, and artifacts.
