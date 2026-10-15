#!/usr/bin/env bash
# Regenerate admin/ui/src/lib/api/schema.d.ts from the management API's OpenAPI document: starts
# examples/multisite on a scratch SQLite database, fetches /api/manage/v1/openapi.json with the dev token from
# loopback and runs `npm run types -- <file>` in admin/ui. Run from the repo root: make admin-types.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/admin-types"
port=${ADMIN_TYPES_PORT:-18433}
pid=

cleanup() { [ -z "$pid" ] || kill "$pid" 2>/dev/null || true; wait 2>/dev/null || true; }
trap cleanup EXIT

rm -rf "$work" && mkdir -p "$work"
(cd "$root/examples/multisite" && go build -o "$work/multisite" .)
export DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$port"
unset MANAGE_TOKEN
(cd "$root/examples/multisite" && "$work/multisite" migrate >"$work/server.log" 2>&1)
(cd "$root/examples/multisite" && exec "$work/multisite" serve >>"$work/server.log" 2>&1) & pid=$!
for _ in $(seq 200); do curl -fsS -o /dev/null "http://127.0.0.1:$port/healthz" 2>/dev/null && break; sleep 0.1; done
curl -fsS -H 'Authorization: Bearer dev-token' "http://127.0.0.1:$port/api/manage/v1/openapi.json" -o "$work/openapi.json" \
  || { cat "$work/server.log"; echo "FAIL: openapi.json"; exit 1; }
(cd "$root/admin/ui" && npm run types -- "$work/openapi.json")
echo "admin-types ok"
