#!/usr/bin/env bash
# Publish the v1-9 pieces-runtime work: push the branch and open the PR.
#   bash scripts/publish-v1-9.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

git push -u origin v1-9

gh pr create \
  --base main \
  --head v1-9 \
  --title "feat: Activepieces pieces as Temporal activities — pieces-worker, reasoner routing, planner index" \
  --body-file .agently/pr-body-v1-9.md

gh pr view --web || true
