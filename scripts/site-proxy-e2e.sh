#!/usr/bin/env bash
# The Next.js starter's standalone build served behind the site-proxy module (ssr mode): one binary on one port
# answers the admin, the APIs and the website (Hydrogen starts and supervises the Next server and proxies to it),
# checked by the adapter conformance suite (packages/adapter-test) including the browser checks.
# The CMS is a scratch project under .work (examples/starter's schema and fixture plus siteproxy.Module()); the
# Next starter is copied there too, with output: 'standalone'. Run from the repo root: make site-proxy-e2e.
# E2E_KEEP=1 leaves the server running afterwards.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/site-proxy-e2e"
manage_token=e2e-token
secret=e2e-revalidate-0123456789abcdef
pids=()

cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
fail() {
  echo "FAIL: $*" >&2
  [ -s "$work/cms.log" ] && { echo "--- cms.log (tail)"; tail -n 25 "$work/cms.log"; } || true
  exit 1
}
trap cleanup EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
wait_http() { # url, seconds
  for _ in $(seq $(($2 * 10))); do curl -fsS -o /dev/null "$1" 2>/dev/null && return 0; sleep 0.1; done
  fail "$1 did not come up"
}

rm -rf "$work" && mkdir -p "$work/cms"
cms_port=$(port)
cms=http://127.0.0.1:$cms_port

echo "== build the packages and the Next starter (standalone)"
"$root/scripts/admin-built.sh"
"$root/scripts/npm-ci.sh" "$root/packages" >/dev/null
(cd "$root/packages" && npm run build -w client -w router -w bridge -w react -w next -w adapter-test >/dev/null)
"$root/scripts/npm-ci.sh" "$root/admin/ui" >/dev/null # Playwright for the browser checks
# A copy at the same depth as the original keeps the starter's relative imports (lib/types.ts) valid.
next="$work/next"
mkdir -p "$next"
(cd "$root/packages/starters/next" && tar --exclude=node_modules --exclude=.next -cf - .) | (cd "$next" && tar xf -)
# The lockfile pins the Next version the starter is tested with; only the file: links move.
sed -i.bak "s#file:\.\./\.\./#file:$root/packages/#" "$next/package.json" "$next/package-lock.json" && rm "$next"/package*.json.bak
cat > "$next/next.config.ts" <<'TS'
import type { NextConfig } from 'next'

const config: NextConfig = {
  output: 'standalone',
  cacheComponents: true,
  partialPrefetching: true,
}
export default config
TS
(cd "$next" && npm install --no-audit --no-fund >"$work/npm.log" 2>&1) || { tail -n 20 "$work/npm.log"; fail "npm install"; }
export HYDROGEN_URL=$cms HYDROGEN_ORIGIN=$cms
(cd "$next" && npm run build >"$work/next-build.log" 2>&1) || { tail -n 30 "$work/next-build.log"; fail "next build"; }
server=$(cd "$next/.next/standalone" && find . -name server.js -not -path '*/node_modules/*' | head -n 1)
[ -n "$server" ] || fail "no standalone server.js"
standalone="$next/.next/standalone/$(dirname "$server")"
mkdir -p "$standalone/.next"
cp -R "$next/.next/static" "$standalone/.next/static"
[ -d "$next/public" ] && cp -R "$next/public" "$standalone/public"

echo "== build the CMS (examples/starter + siteproxy) on :$cms_port"
cms_dir="$work/cms"
cp -R "$root/examples/starter/schema" "$root/examples/starter/fixture" "$root/examples/starter/config" "$cms_dir/"
sed -e 's#^module .*#module example.com/siteproxy-e2e#' \
  -e "s#=> \.\./\.\.#=> $root#" -e "s#=> \.\./\.\./modules/#=> $root/modules/#" \
  "$root/examples/starter/go.mod" > "$cms_dir/go.mod"
cp "$root/examples/starter/go.sum" "$cms_dir/go.sum"
cat > "$cms_dir/main.go" <<'GO'
package main

import (
	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/siteproxy"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func main() { hydrogen.Main(configure) }

func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), postgres.Module(), pages.Module(), siteproxy.Module())
}
GO
awk -v dir="$standalone" -v srv="$(basename "$server")" '
  { print }
  /^modules:/ {
    print "  site-proxy:"
    print "    ssr:"
    print "      command: [node, " srv "]"
    print "      dir: " dir
    print "      host: localhost # Next.js needs a name here: with 127.0.0.1 its 404 rewrites loop"
    print "      env:"
    print "        PORT: \"{port}\""
    print "        HOSTNAME: \"{host}\""
    print "        HYDROGEN_URL: ${env:HYDROGEN_URL}"
    print "        HYDROGEN_ORIGIN: ${env:HYDROGEN_ORIGIN}"
    print "        HYDROGEN_REVALIDATE_SECRET: ${env:HYDROGEN_REVALIDATE_SECRET}"
    print "      health: /en"
    print "      start_timeout: 90s"
  }' "$root/examples/starter/config/app.yaml" > "$cms_dir/config/app.yaml"
cat >> "$cms_dir/config/app.yaml" <<'YAML'

webhooks:
  next:
    url: ${env:NEXT_WEBHOOK_URL}
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}
YAML
(cd "$cms_dir" && GOWORK=off GOFLAGS=-mod=mod go build -o "$work/cms-bin" .) || fail "go build"

echo "== serve (the CMS starts and supervises the Next server)"
export DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$cms_port" MANAGE_TOKEN="$manage_token"
export STARTER_FRONTEND="$cms" NEXT_WEBHOOK_URL="$cms/api/hydrogen/revalidate" HYDROGEN_REVALIDATE_SECRET="$secret"
cd "$cms_dir"
"$work/cms-bin" config:check >"$work/cms.log" 2>&1 || fail "config:check"
"$work/cms-bin" migrate >>"$work/cms.log" 2>&1 && "$work/cms-bin" import fixture >>"$work/cms.log" 2>&1 || fail "migrate/import"
"$work/cms-bin" serve >>"$work/cms.log" 2>&1 & pids+=($!)
wait_http "$cms/healthz" 20
wait_http "$cms/en" 120

echo "== precedence"
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
curl -sS -D - -o /dev/null "$cms/api/deliver/v1/tree?channel=en" | grep -qi '^content-type: application/json' || fail "the delivery API is shadowed"
[ "$(code "$cms/api/manage/v1/system")" = 401 ] || fail "the management API is shadowed"
curl -fsS "$cms/admin/" | grep -qi "<title>" || fail "the admin is shadowed"
curl -fsS "$cms/en" | grep -q "__next\|<html" || fail "the website is not served by the frontend"

echo "== conformance"
status=0
node "$root/packages/adapter-test/dist/cli.js" --cms "$cms" --token "$manage_token" --frontend "$cms" \
  --browser --playwright-from "$root/admin/ui" || status=$?
if [ -n "${E2E_KEEP:-}" ]; then
  echo "E2E_KEEP: server stays up at $cms (ctrl-c to stop)"
  sleep 3600
fi
[ "$status" = 0 ] || fail "the conformance suite reported failures"

echo "== shutdown stops the frontend"
child=$(grep -o 'frontend process started" pid=[0-9]*' "$work/cms.log" | tail -n 1 | grep -o '[0-9]*$')
[ -n "$child" ] && kill -0 "$child" 2>/dev/null || fail "no running frontend process to stop"
kill "${pids[0]}"; wait "${pids[0]}" 2>/dev/null || true; pids=()
sleep 1
if kill -0 "$child" 2>/dev/null; then fail "the frontend outlived the CMS"; fi
echo "site-proxy-e2e ok"
