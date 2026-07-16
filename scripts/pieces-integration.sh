#!/usr/bin/env bash
# One-shot integration: install pieces-worker deps, build, test, generate the
# piece index, then run the reasoner + api suites. Writes a full transcript to
# .agently/pieces-integration.log so progress is inspectable afterwards.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG="$ROOT/.agently/pieces-integration.log"
mkdir -p "$ROOT/.agently"
: > "$LOG"

step() { echo "══ $* ══" | tee -a "$LOG"; }
run()  { echo "\$ $*" >>"$LOG"; "$@" >>"$LOG" 2>&1; local rc=$?; [ $rc -eq 0 ] && echo "   ok" | tee -a "$LOG" >/dev/null || echo "   FAILED (rc=$rc)" | tee -a "$LOG"; return $rc; }

overall=0

step "pieces-worker: npm install"
( cd "$ROOT/apps/pieces-worker" && run npm install --no-audit --no-fund ) || overall=1

step "pieces-worker: build"
( cd "$ROOT/apps/pieces-worker" && run npm run build ) || overall=1

step "pieces-worker: tests"
( cd "$ROOT/apps/pieces-worker" && run npm test ) || overall=1

step "pieces-worker: gen:index"
( cd "$ROOT/apps/pieces-worker" && run npm run gen:index ) || overall=1

step "reasoner: unittest"
( cd "$ROOT/apps/reasoner" && run ./.venv/bin/python -m unittest discover -s tests ) || overall=1

step "api: go build + test"
( cd "$ROOT/apps/api" && run go build ./... && run go test ./... ) || overall=1

step "result"
if [ $overall -eq 0 ]; then echo "ALL GREEN" | tee -a "$LOG"; else echo "FAILURES — see $LOG" | tee -a "$LOG"; fi
tail -5 "$LOG" >/dev/null
exit $overall
