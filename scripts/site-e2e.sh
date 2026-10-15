#!/usr/bin/env bash
# examples/fullstack (SQLite, the starter fixture imported) served by one binary, checked by the adapter
# conformance suite (packages/adapter-test) including the browser checks in reload mode. The CMS and the website
# are the same server. Run from the repo root: make site-e2e. E2E_KEEP=1 leaves the server running afterwards.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/site-e2e"
manage_token=e2e-token
pids=()

cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
fail() {
  echo "FAIL: $*" >&2
  [ -s "$work/site.log" ] && { echo "--- site.log (tail)"; tail -n 20 "$work/site.log"; } || true
  exit 1
}
trap cleanup EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }

rm -rf "$work" && mkdir -p "$work"
p=$(port)
url=http://127.0.0.1:$p

echo "== build"
(cd "$root/examples/fullstack" && go build -o "$work/fullstack" .)
"$root/scripts/npm-ci.sh" "$root/packages" >/dev/null
(cd "$root/packages" && npm run build >/dev/null)
"$root/scripts/npm-ci.sh" "$root/admin/ui" >/dev/null # Playwright for the browser checks

echo "== site:check, then serve on :$p"
export DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$p" MANAGE_TOKEN="$manage_token" SITE_URL="$url"
cd "$root/examples/fullstack"
"$work/fullstack" migrate >"$work/site.log" 2>&1 && "$work/fullstack" import ../starter/fixture >>"$work/site.log" 2>&1 || fail "migrate/import"
"$work/fullstack" site:check -db >>"$work/site.log" 2>&1 || fail "site:check"
"$work/fullstack" serve >>"$work/site.log" 2>&1 & pids+=($!)
for _ in $(seq 200); do curl -fsS -o /dev/null "$url/healthz" 2>/dev/null && break; sleep 0.1; done
curl -fsS -o /dev/null "$url/healthz" || fail "the server did not come up"

echo "== conformance"
status=0
node "$root/packages/adapter-test/dist/cli.js" --cms "$url" --token "$manage_token" --frontend "$url" \
  --browser --playwright-from "$root/admin/ui" || status=$?
if [ -n "${E2E_KEEP:-}" ]; then
  echo "E2E_KEEP: server stays up at $url (ctrl-c to stop)"
  sleep 3600
fi
[ "$status" = 0 ] || fail "the conformance suite reported failures"
echo "site-e2e ok"
