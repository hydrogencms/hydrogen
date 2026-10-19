#!/usr/bin/env bash
# The Nuxt starter against examples/starter (SQLite, fixture imported), checked by the adapter conformance suite
# (packages/adapter-test) including the browser checks. Run from the repo root: make starter-nuxt-e2e.
# E2E_KEEP=1 leaves both servers running afterwards. The CMS and the frontend share the host name 127.0.0.1, so the
# preview cookie (SameSite=Lax over http) reaches the frame the suite plays the admin in.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/starter-nuxt-e2e"
starter="$root/packages/starters/nuxt"
secret=e2e-revalidate-0123456789abcdef
manage_token=e2e-token
pids=()

cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
fail() {
  echo "FAIL: $*" >&2
  for f in cms nuxt; do [ -s "$work/$f.log" ] && { echo "--- $f.log (tail)"; tail -n 15 "$work/$f.log"; } || true; done
  exit 1
}
trap cleanup EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
wait_http() { # url, seconds
  for _ in $(seq $(($2 * 10))); do curl -fsS -o /dev/null "$1" 2>/dev/null && return 0; sleep 0.1; done
  fail "$1 did not come up"
}

rm -rf "$work" && mkdir -p "$work/cfg"
cms_port=$(port); nuxt_port=$(port)
cms=http://127.0.0.1:$cms_port; nuxt=http://127.0.0.1:$nuxt_port

echo "== build"
(cd "$root/examples/starter" && go build -o "$work/starter" .)
"$root/scripts/npm-ci.sh" "$root/packages" >/dev/null
(cd "$root/packages" && npm run build >/dev/null)
"$root/scripts/npm-ci.sh" "$root/admin/ui" >/dev/null # Playwright for the browser checks
(cd "$starter" && rm -rf node_modules/@hydrogencms && npm install --no-audit --no-fund >/dev/null 2>&1)

echo "== cms on :$cms_port (fixture imported), frontend on :$nuxt_port"
cp "$root"/examples/starter/config/*.yaml "$work/cfg/"
cat >> "$work/cfg/app.yaml" <<'YAML'

webhooks:
  nuxt:
    url: ${env:NUXT_WEBHOOK_URL}
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}
YAML
export CONFIG_DIR="$work/cfg" DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$cms_port" MANAGE_TOKEN="$manage_token"
export STARTER_FRONTEND="$nuxt" NUXT_WEBHOOK_URL="$nuxt/api/hydrogen/revalidate" HYDROGEN_REVALIDATE_SECRET="$secret"
(cd "$root/examples/starter" && "$work/starter" migrate >"$work/cms.log" 2>&1 && "$work/starter" import fixture >>"$work/cms.log" 2>&1) || fail "migrate/import"
(cd "$root/examples/starter" && exec "$work/starter" serve >>"$work/cms.log" 2>&1) & pids+=($!)
wait_http "$cms/healthz" 20

echo "== nuxt build + start"
(cd "$starter" && rm -rf .nuxt .output && NUXT_TELEMETRY_DISABLED=1 npm run build >"$work/nuxt-build.log" 2>&1) || { tail -n 30 "$work/nuxt-build.log"; fail "nuxt build"; }
(cd "$starter" && exec env NITRO_HOST=127.0.0.1 NITRO_PORT="$nuxt_port" NUXT_HYDROGEN_BASE_URL="$cms" NUXT_HYDROGEN_ORIGIN="$nuxt" \
  NUXT_HYDROGEN_REVALIDATE_SECRET="$secret" node .output/server/index.mjs >"$work/nuxt.log" 2>&1) & pids+=($!)
wait_http "$nuxt/en" 30

echo "== conformance"
status=0
node "$root/packages/adapter-test/dist/cli.js" --cms "$cms" --token "$manage_token" --frontend "$nuxt" \
  --browser --playwright-from "$root/admin/ui" || status=$?
if [ -n "${E2E_KEEP:-}" ]; then
  echo "E2E_KEEP: servers stay up: cms=$cms frontend=$nuxt (ctrl-c to stop)"
  sleep 3600
fi
[ "$status" = 0 ] || fail "the conformance suite reported failures"
echo "starter-nuxt-e2e ok"
