# RUNBOOK — running Agently locally

One orchestrator script (`scripts/agently.sh`) drives the whole stack. All commands
below are `pnpm` scripts that wrap it. Run from the repo root.

## TL;DR

```bash
pnpm agently          # cold-start EVERYTHING (Docker + build + all services)
pnpm agently:status   # what's up / down
pnpm agently:logs     # tail all logs (Ctrl-C to stop tailing)
pnpm agently:stop     # stop app processes (Docker stays up)
```

Open the UI at **http://localhost:3000**. Temporal UI at **http://localhost:8080**.

---

## The 7 processes

| Process | Type | Port | Role |
|---|---|---|---|
| Postgres | Docker | 5433 | source of truth (shared by both engines) |
| Temporal (+ own PG + UI) | Docker | 7233 / UI **8080** | durability engine for the reasoner |
| API | Go | **8090** | control plane — the UI talks to this |
| Worker | Go | — | **native** execution engine |
| Reasoner | Python | — | **temporal** execution engine (dynamic graphs, real browser) |
| Pieces | Node | **7391** | Activepieces runtime — `pieces.*` nodes via the `agently-pieces` task queue, plus the HTTP surface on :7391 for dynamic-prop options + trigger webhooks/polling (optional; see apps/pieces-worker) |
| Web | Next.js | **3000** | the UI |

> **Port note:** Temporal UI owns `:8080`, so the API runs on **`:8090`** and the web
> dev server proxies `/api/*` there via `API_PROXY_TARGET`. The script sets both.

---

## When code changes — what to restart

**Only the Web app hot-reloads itself.** Everything Go or Python is a compiled/
long-lived process and must be restarted. Any `.env` change → restart the services
that read it.

| You changed… | Reloads itself? | Command |
|---|---|---|
| `apps/web/**` (components, `lib/`, pages) | ✅ Yes (Fast Refresh) | nothing |
| `apps/web/next.config.ts` or web env | ❌ | `pnpm agently:web` |
| `apps/api/**.go` | ❌ | `pnpm agently:api` (rebuilds + restarts) |
| `apps/reasoner/**.py` | ❌ | `pnpm agently:reasoner` |
| `apps/pieces-worker/**.ts` or piece packages | ❌ | `cd apps/pieces-worker && npm run build && npm run gen:index`, then `pnpm agently:pieces` (index changes also need `pnpm agently:api`) |
| **`.env`** value (e.g. `BROWSERBASE_PROJECT_ID`) | ❌ | restart the reader(s): `agently:reasoner` (and `agently:api` if it reads it) |
| `docker-compose.yml` | ❌ | `docker compose up -d` |
| new file in `packages/db/migrations/` | ❌ (fresh-DB only) | apply manually — see below |

Rule of thumb: **Web = automatic. Go/Python = restart. `.env` = restart the reader.**

---

## Command reference

| Command | Does |
|---|---|
| `pnpm agently` | cold start: Docker → build Go → (install web deps if needed) → start all 4 services → wait for API health |
| `pnpm agently:stop` | stop the 4 app processes; leave Docker running |
| `pnpm agently:down` | stop app processes **and** `docker compose down` |
| `pnpm agently:status` | per-service up/down + Docker container status |
| `pnpm agently:logs [svc]` | tail logs; `svc` = `api`\|`reasoner`\|`pieces`\|`web` (default: all) |
| `pnpm agently:api` | rebuild + restart just the API |
| `pnpm agently:reasoner` | restart just the Reasoner |
| `pnpm agently:pieces` | restart just the Pieces worker (skipped if apps/pieces-worker isn't built) |
| `pnpm agently:web` | restart just the Web |

Logs live in `.agently/logs/*.log`; PIDs in `.agently/*.pid` (git-ignored).

---

## Credentials & env

- **`.env`** (auto-loaded by the services): `GEMINI_API_KEY`, `BROWSERBASE_API_KEY`,
  `BROWSERBASE_PROJECT_ID`, `OPENAI_API_KEY`, `RESEND_API_KEY`, `SMTP_*`.
- **LLM** — the reasoner (run-time synthesis) and the API planner both run on **Google
  Gemini**. Add one line to `.env` and every start is real, no proxy, no exporting:
  ```
  GEMINI_API_KEY=...
  ```
  Without it they fall back to `OPENAI_API_KEY` (planner only) or the deterministic
  **mock**. Override the models with `REASONER_MODEL` / `REASONER_SYNTHESIS_MODEL`
  (defaults `gemini-2.5-flash` / `gemini-2.5-pro`) and `GEMINI_MODEL` (planner).

### Which engine runs my graph?
Every run executes on the **Temporal reasoner** (the native Go worker was retired in
v1-11). It runs the composed graph **literally** — a browser node actually visits its
URL, an `agent.llm` node runs its prompt on Gemini. The visual builder's **Test run**
button uses this engine.

---

## Prerequisites (one-time)

- **Docker Desktop** running.
- **Reasoner venv:** `cd apps/reasoner && python3 -m venv .venv && ./.venv/bin/pip install -e .`
- **Web deps:** `pnpm install` (the script runs this automatically if `next` is missing).
- Go toolchain + pnpm on PATH.

---

## Migrations & data

Apply a new migration without wiping data:
```bash
docker exec -i agently-postgres psql -U agently -d agently < packages/db/migrations/00XX_name.sql
```
Full reset (**destroys all data**, re-applies mounted migrations on next boot):
```bash
docker compose down -v && docker compose up -d
```

---

## Health checks
```bash
curl -s -o /dev/null -w "api %{http_code}\n" http://localhost:8090/api/workflows   # want 200
grep -i browserbase .agently/logs/reasoner.log    # want browserbase=True after project-id set
pnpm agently:status
```
