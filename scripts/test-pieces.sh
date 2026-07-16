#!/usr/bin/env bash
# Run every test suite touched by the pieces-runtime work:
#   bash scripts/test-pieces.sh
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
rc=0

echo "── reasoner (python) ──"
(cd "$ROOT/apps/reasoner" && ./.venv/bin/python -m unittest discover -s tests) || rc=1

echo "── api (go) ──"
(cd "$ROOT/apps/api" && go build ./... && go test ./...) || rc=1

echo "── pieces-worker (node) ──"
if [ -d "$ROOT/apps/pieces-worker/node_modules" ]; then
  (cd "$ROOT/apps/pieces-worker" && npm run build && npm test) || rc=1
else
  echo "skipped (npm install not run yet)"
fi

exit $rc
