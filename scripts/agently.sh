#!/usr/bin/env bash
#
# agently.sh — one-command orchestrator for the whole local stack.
#
#   pnpm agently            # cold start EVERYTHING (infra + build + all services)
#   pnpm agently:stop       # stop the app processes (leave Docker running)
#   pnpm agently:down       # stop app processes AND Docker
#   pnpm agently:status     # what's up / down
#   pnpm agently:logs [svc] # tail logs (svc = api|reasoner|pieces|web; default all)
#   pnpm agently:api        # rebuild+restart just the API   (after apps/api/**.go change)
#   pnpm agently:reasoner   # restart just the Reasoner       (after apps/reasoner/**.py change)
#   pnpm agently:web        # restart just the Web            (after next.config/env change)
#
# The Web app hot-reloads on its own (Next Fast Refresh) — you only restart it for
# config/env changes. Go and Python are long-lived processes: restart on change.
#
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# ---------- config (override by exporting before you call) ----------
export DATABASE_URL="${DATABASE_URL:-postgres://agently:agently@localhost:5433/agently}"
API_ADDR="${API_ADDR:-:8090}"                 # API on :8090 — :8080 is taken by Temporal UI
API_PORT="${API_ADDR##*:}"
export API_PROXY_TARGET="${API_PROXY_TARGET:-http://localhost:${API_PORT}}"
export TEMPORAL_HOSTPORT="${TEMPORAL_HOSTPORT:-localhost:7233}"
export TEMPORAL_TASK_QUEUE="${TEMPORAL_TASK_QUEUE:-agently-reasoner}"
export PIECES_TASK_QUEUE="${PIECES_TASK_QUEUE:-agently-pieces}"

# LLM/browser creds are auto-loaded from .env by the services themselves:
# GEMINI_API_KEY (run-time synthesis + planner), BROWSERBASE_* / OPENAI_* / SMTP_*.
# No proxy base URL — the Gemini SDK talks straight to Google.

API_BIN=/tmp/agently-api
RUNDIR="$ROOT/.agently"
LOGS="$RUNDIR/logs"
mkdir -p "$LOGS"

c_grn=$'\033[32m'; c_red=$'\033[31m'; c_dim=$'\033[2m'; c_rst=$'\033[0m'
say() { printf "%s\n" "$*"; }
ok()  { printf "  ${c_grn}✓${c_rst} %s\n" "$*"; }
bad() { printf "  ${c_red}✗${c_rst} %s\n" "$*"; }

# ---------- process helpers ----------
pidfile() { echo "$RUNDIR/$1.pid"; }
is_up()   { local f; f="$(pidfile "$1")"; [ -f "$f" ] && kill -0 "$(cat "$f")" 2>/dev/null; }

stop_one() {
  local n="$1" f; f="$(pidfile "$n")"
  [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null
  rm -f "$f"
  case "$n" in
    api)      pkill -f agently-api 2>/dev/null ;;
    reasoner) pkill -f "reasoner.worker" 2>/dev/null ;;
    pieces)   pkill -f "pieces-worker/dist/worker.js" 2>/dev/null ;;
    web)      pkill -f "next dev" 2>/dev/null ;;
  esac
  return 0
}

wait_http() { # url tries
  local url="$1" tries="${2:-40}" i
  for i in $(seq 1 "$tries"); do curl -sf -o /dev/null "$url" && return 0; sleep 1; done
  return 1
}

# ---------- build ----------
build_go() {
  say "Building Go binaries…"
  ( cd apps/api    && go build -o "$API_BIN"    ./cmd/server ) && ok "api built"    || { bad "api build failed";    return 1; }
}

ensure_web_deps() {
  [ -x "$ROOT/node_modules/.bin/next" ] || { say "Installing web deps (pnpm install)…"; pnpm install >/dev/null; }
}

ensure_reasoner_venv() {
  [ -x "$ROOT/apps/reasoner/.venv/bin/python" ] || {
    bad "reasoner venv missing at apps/reasoner/.venv — create it:"
    say "    cd apps/reasoner && python3 -m venv .venv && ./.venv/bin/pip install -e ."
    return 1
  }
}

# The pieces worker is OPTIONAL: without it, pieces.* nodes record intent and the
# rest of the platform is unaffected. We start it only when it has been built.
pieces_built() { [ -f "$ROOT/apps/pieces-worker/dist/worker.js" ]; }

# ---------- infra ----------
start_infra() {
  say "Starting Docker infra (Postgres + Temporal)…"
  docker info >/dev/null 2>&1 || { bad "Docker daemon not running — start Docker Desktop first."; return 1; }
  docker compose up -d >/dev/null 2>&1 || { bad "docker compose up failed"; return 1; }
  printf "  waiting for Postgres"
  for i in $(seq 1 40); do docker exec agently-postgres pg_isready -U agently >/dev/null 2>&1 && break; printf "."; sleep 1; done
  echo; ok "infra up (postgres :5433, temporal :7233, temporal-ui :8080)"
}

# ---------- per-service start ----------
start_api() {
  API_ADDR="$API_ADDR" DATABASE_URL="$DATABASE_URL" \
    nohup "$API_BIN" >"$LOGS/api.log" 2>&1 & echo $! >"$(pidfile api)"
  ok "api      → http://localhost:${API_PORT}   (pid $!)"
}
start_reasoner() {
  ( cd "$ROOT/apps/reasoner" && \
    DATABASE_URL="$DATABASE_URL" TEMPORAL_HOSTPORT="$TEMPORAL_HOSTPORT" TEMPORAL_TASK_QUEUE="$TEMPORAL_TASK_QUEUE" \
    PIECES_TASK_QUEUE="$PIECES_TASK_QUEUE" \
    exec ./.venv/bin/python -m reasoner.worker ) >"$LOGS/reasoner.log" 2>&1 & echo $! >"$(pidfile reasoner)"
  ok "reasoner → temporal engine          (pid $!)"
}
start_pieces() {
  pieces_built || {
    say "  ${c_dim}pieces-worker not built (apps/pieces-worker: npm install && npm run build) — pieces.* nodes will record intent${c_rst}"
    return 0
  }
  ( cd "$ROOT/apps/pieces-worker" && \
    TEMPORAL_HOSTPORT="$TEMPORAL_HOSTPORT" PIECES_TASK_QUEUE="$PIECES_TASK_QUEUE" \
    exec node dist/worker.js ) >"$LOGS/pieces.log" 2>&1 & echo $! >"$(pidfile pieces)"
  ok "pieces   → activepieces runtime     (pid $!)"
}
start_web() {
  API_PROXY_TARGET="$API_PROXY_TARGET" \
    nohup pnpm --filter @agently/web dev >"$LOGS/web.log" 2>&1 & echo $! >"$(pidfile web)"
  ok "web      → http://localhost:3000    (pid $!)"
}

# ---------- commands ----------
cmd_start() {
  start_infra || return 1
  ensure_web_deps
  ensure_reasoner_venv || return 1
  build_go || return 1
  say "Stopping any previous app processes…"
  for n in web pieces reasoner api; do stop_one "$n"; done
  say "Starting services…"
  start_api; start_reasoner; start_pieces; start_web
  printf "  waiting for API health"
  if wait_http "http://localhost:${API_PORT}/api/workflows" 40; then echo; ok "API healthy"; else echo; bad "API didn't answer — see $LOGS/api.log"; fi
  grep -qE '^GEMINI_API_KEY=.+' "$ROOT/.env" 2>/dev/null || say "  ${c_dim}note: GEMINI_API_KEY not set in .env → reasoner/planner fall back to OpenAI(.env) or mock${c_rst}"
  echo
  say "${c_grn}Agently is up.${c_rst}  UI: http://localhost:3000   Temporal UI: http://localhost:8080"
  say "  logs: pnpm agently:logs   status: pnpm agently:status   stop: pnpm agently:stop"
}

cmd_stop() { say "Stopping app processes…"; for n in web pieces reasoner api; do stop_one "$n" && ok "$n stopped"; done; }
cmd_down() { cmd_stop; say "Stopping Docker…"; docker compose down >/dev/null 2>&1 && ok "docker down"; }

cmd_restart() { # svc
  local svc="${1:-all}"
  case "$svc" in
    api)      build_go || return 1; stop_one api;      start_api ;;
    reasoner) ensure_reasoner_venv || return 1; stop_one reasoner; start_reasoner ;;
    pieces)   stop_one pieces; start_pieces ;;
    web)      stop_one web; start_web ;;
    all)      cmd_start ;;
    *)        bad "unknown service '$svc' (api|reasoner|pieces|web|all)"; return 1 ;;
  esac
}

cmd_status() {
  say "Services:"
  for n in api reasoner pieces web; do
    if is_up "$n"; then ok "$(printf '%-9s up   (pid %s)' "$n" "$(cat "$(pidfile "$n")")")"; else bad "$(printf '%-9s down' "$n")"; fi
  done
  say "Docker:"; docker compose ps --format '  {{.Name}}  {{.Status}}' 2>/dev/null || bad "docker not reachable"
}

cmd_logs() {
  local svc="${1:-}"
  if [ -n "$svc" ]; then tail -f "$LOGS/$svc.log"; else tail -f "$LOGS"/*.log; fi
}

case "${1:-start}" in
  start|up)   cmd_start ;;
  stop)       cmd_stop ;;
  down)       cmd_down ;;
  restart)    shift; cmd_restart "${1:-all}" ;;
  build)      build_go ;;
  status)     cmd_status ;;
  logs)       shift; cmd_logs "${1:-}" ;;
  *)          say "usage: agently.sh {start|stop|down|restart <svc>|build|status|logs [svc]}" ;;
esac
