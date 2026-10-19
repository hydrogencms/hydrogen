#!/usr/bin/env bash
# The Astro starter against examples/starter (SQLite, fixture imported), checked by the adapter conformance suite
# (packages/adapter-test) including the browser checks. Run from the repo root: make starter-astro-e2e.
# E2E_KEEP=1 leaves both servers running afterwards. The CMS and the frontend share the host name 127.0.0.1, so the
# preview cookie (SameSite=Lax over http) reaches the frame the suite plays the admin in.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/starter-astro-e2e"
starter="$root/packages/starters/astro"
secret=e2e-revalidate-0123456789abcdef
manage_token=e2e-token
pids=()

cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
fail() {
  echo "FAIL: $*" >&2
  for f in cms astro; do [ -s "$work/$f.log" ] && { echo "--- $f.log (tail)"; tail -n 15 "$work/$f.log"; } || true; done
  exit 1
}
trap cleanup EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
wait_http() { # url, seconds
  for _ in $(seq $(($2 * 10))); do curl -fsS -o /dev/null "$1" 2>/dev/null && return 0; sleep 0.1; done
  fail "$1 did not come up"
}

rm -rf "$work" && mkdir -p "$work/cfg"
cms_port=$(port); astro_port=$(port)
cms=http://127.0.0.1:$cms_port; astro=http://127.0.0.1:$astro_port

echo "== build"
(cd "$root/examples/starter" && go build -o "$work/starter" .)
"$root/scripts/npm-ci.sh" "$root/packages" >/dev/null
(cd "$root/packages" && npm run build >/dev/null)
"$root/scripts/npm-ci.sh" "$root/admin/ui" >/dev/null # Playwright for the browser checks
(cd "$starter" && rm -rf node_modules/@hydrogencms && npm install --no-audit --no-fund >/dev/null)

echo "== cms on :$cms_port (fixture imported), frontend on :$astro_port"
cp "$root"/examples/starter/config/*.yaml "$work/cfg/"
cat >> "$work/cfg/app.yaml" <<'YAML'

webhooks:
  astro:
    url: ${env:ASTRO_WEBHOOK_URL}
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}
YAML
export CONFIG_DIR="$work/cfg" DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$cms_port" MANAGE_TOKEN="$manage_token"
export STARTER_FRONTEND="$astro" ASTRO_WEBHOOK_URL="$astro/api/hydrogen/revalidate" HYDROGEN_REVALIDATE_SECRET="$secret"
(cd "$root/examples/starter" && "$work/starter" migrate >"$work/cms.log" 2>&1 && "$work/starter" import fixture >>"$work/cms.log" 2>&1) || fail "migrate/import"
(cd "$root/examples/starter" && exec "$work/starter" serve >>"$work/cms.log" 2>&1) & pids+=($!)
wait_http "$cms/healthz" 20

echo "== astro build + start"
export HYDROGEN_URL=$cms HYDROGEN_ORIGIN=$astro
(cd "$starter" && rm -rf dist && npm run build >"$work/astro-build.log" 2>&1) || { tail -n 30 "$work/astro-build.log"; fail "astro build"; }
(cd "$starter" && HOST=127.0.0.1 PORT="$astro_port" exec node dist/server/entry.mjs >"$work/astro.log" 2>&1) & pids+=($!)
wait_http "$astro/en" 30

echo "== conformance"
status=0
node "$root/packages/adapter-test/dist/cli.js" --cms "$cms" --token "$manage_token" --frontend "$astro" \
  --browser --playwright-from "$root/admin/ui" || status=$?
if [ -n "${E2E_KEEP:-}" ]; then
  echo "E2E_KEEP: servers stay up: cms=$cms frontend=$astro (ctrl-c to stop)"
  sleep 3600
fi
[ "$status" = 0 ] || fail "the conformance suite reported failures"
echo "starter-astro-e2e ok"
