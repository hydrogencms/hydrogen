#!/usr/bin/env bash
# npm ci only when node_modules is missing or older than package-lock.json: a reinstall deletes node_modules,
# which breaks a running `npm run dev` (Vite) in the same directory. Usage: scripts/npm-ci.sh [dir]
set -euo pipefail
dir=${1:-.}
cd "$dir"
if [ -f node_modules/.package-lock.json ] && [ node_modules/.package-lock.json -nt package-lock.json ]; then
  echo "node_modules up to date in $dir, skipping npm ci"
else
  npm ci --no-audit --no-fund
fi
