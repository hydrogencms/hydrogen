#!/usr/bin/env bash
# `hydrogen new -static` end to end: scaffold a project against this checkout, start its CMS the way the deploy workflow
# does (production, content/ loaded into a fresh SQLite database), build the Astro site under a base path and check the
# output; then edit through the management API, run content:save, and check that content/ changed and a rebuild shows
# the edit. Run from the repo root: make static-e2e. E2E_KEEP=1 keeps the project in .work/static-e2e.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/static-e2e"
site_url=https://example.github.io
site_base=/demo
pid=
export GOWORK=off

fail() {
  echo "FAIL: $*" >&2
  [ -s "$work/cms.log" ] && { echo "--- cms.log (tail)"; tail -n 20 "$work/cms.log"; } || true
  exit 1
}
stop() {
  [ -n "$pid" ] && kill "$pid" 2>/dev/null && wait "$pid" 2>/dev/null || true
  pid=
}
trap stop EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
check() { grep -q -- "$2" "$1" || fail "$1 lacks: $2"; }

rm -rf "$work" && mkdir -p "$work"
cms_port=$(port)
cms=http://127.0.0.1:$cms_port

echo "== scaffold"
go build -o "$work/hydrogen" "$root/cmd/hydrogen"
("$root/scripts/npm-ci.sh" "$root/packages" && cd "$root/packages" && npm run build) >"$work/packages.log" 2>&1 || { tail -n 20 "$work/packages.log"; fail "building packages/"; }
(cd "$work" && ./hydrogen new -static -core "$root" demo >"$work/new.log" 2>&1) || { cat "$work/new.log"; fail "hydrogen new -static"; }
demo="$work/demo"
for f in content/manifest.json gen/types.ts site/package.json .github/workflows/deploy.yml; do
  [ -f "$demo/$f" ] || fail "the project lacks $f"
done
(cd "$demo" && "$work/hydrogen" build -o "$work/cms-bin" >/dev/null) || fail "hydrogen build"
(cd "$demo/site" && npm install --no-audit --no-fund >"$work/npm.log" 2>&1) || { tail -n 20 "$work/npm.log"; fail "npm install"; }

# start the CMS in the demo project with the given environment; waits until it answers
start() {
  (cd "$demo" && exec env LISTEN_ADDR="127.0.0.1:$cms_port" SITE_URL="$site_url" "$@" "$work/cms-bin" serve >"$work/cms.log" 2>&1) &
  pid=$!
  for _ in $(seq 200); do
    curl -fsS -o /dev/null "$cms/healthz" 2>/dev/null && return 0
    kill -0 "$pid" 2>/dev/null || fail "the CMS exited"
    sleep 0.1
  done
  fail "the CMS did not answer"
}
build_site() {
  (cd "$demo/site" && rm -rf dist && env HYDROGEN_URL="$cms" SITE_URL="$site_url" SITE_BASE="$site_base" npm run build >"$work/site-build.log" 2>&1) ||
    { tail -n 30 "$work/site-build.log"; fail "astro build"; }
}

echo "== the workflow's way: production, fresh database"
start APP_ENV=production DATABASE_URL=sqlite:.work/hydrogen.db MANAGE_TOKEN=e2e-manage-token-0123456789abcdef PREVIEW_SECRET=e2e-preview-secret-0123456789abcdef
check "$work/cms.log" "open this link to create the first one"
check "$work/cms.log" "login#setup="
curl -fsS "$cms/api/deliver/v1/singletons/siteSettings?channel=web" | grep -q 'The content of this site lives in git' || fail "content/ was not loaded"
build_site
dist="$demo/site/dist"
for f in index.html about/index.html 404.html favicon.svg; do [ -f "$dist/$f" ] || fail "dist lacks $f"; done
check "$dist/index.html" 'Welcome to your new site'
check "$dist/index.html" 'The content of this site lives in git'
check "$dist/index.html" 'href="/demo/about"'
check "$dist/index.html" 'rel="canonical" href="https://example.github.io/demo/"'
check "$dist/index.html" 'href="/demo/favicon.svg"'
check "$dist/about/index.html" 'How this site is made'
check "$dist/about/index.html" 'href="/demo/"'
check "$dist/about/index.html" 'rel="canonical" href="https://example.github.io/demo/about'
check "$dist/404.html" 'Page not found'
if grep -rhoE '(href|src)="/[^"]*"' "$dist" --include='*.html' | grep -vE '="/demo(/|")'; then fail "a link misses the base path"; fi
stop

echo "== edit, content:save, rebuild (dev, the dev token)"
cp -R "$demo/content" "$work/content-before"
start
B=$cms/api/manage/v1
curl -fsS -X PATCH "$B/singletons/siteSettings" -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' \
  -d '{"locale":"en","fields":{"footerText":"Edited through the management API"}}' >/dev/null || fail "PATCH siteSettings"
curl -fsS -X POST "$B/singletons/siteSettings/publish" -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' -d '{}' >/dev/null || fail "publish siteSettings"
(cd "$demo" && "$work/hydrogen" content:save) >"$work/save.log" 2>&1 || { cat "$work/save.log"; fail "content:save"; }
diff -rq "$work/content-before" "$demo/content" >/dev/null && fail "content/ did not change"
grep -rq 'Edited through the management API' "$demo/content" || fail "content/ lacks the edit"
build_site
check "$dist/index.html" 'Edited through the management API'
stop

echo "== restart on the saved content"
start APP_ENV=production DATABASE_URL=sqlite:.work/hydrogen.db MANAGE_TOKEN=e2e-manage-token-0123456789abcdef PREVIEW_SECRET=e2e-preview-secret-0123456789abcdef
check "$work/cms.log" "serving"
stop
[ -n "${E2E_KEEP:-}" ] || rm -rf "$work"
echo "static-e2e ok"
