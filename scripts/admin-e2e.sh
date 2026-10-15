#!/usr/bin/env bash
# Admin end to end: builds admin/dist, starts examples/multisite on a scratch SQLite database with a seeded
# administrator, runs the Playwright tests in admin/ui against it and stops the server. Then does the same with
# examples/content (its extensions spec only, on another port and database). E2E_KEEP=1 leaves the content server
# running afterwards. Run from the repo root: make admin-e2e.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/admin-e2e"
port=${ADMIN_E2E_PORT:-18434}
login=${ADMIN_LOGIN:-admin}
password=${ADMIN_PASSWORD:-e2e-admin-password-1}
pid=

cleanup() { [ -z "$pid" ] || kill "$pid" 2>/dev/null || true; wait 2>/dev/null || true; }
fail() {
  echo "FAIL: $*" >&2
  [ -s "$work/server.log" ] && { echo "--- server.log (tail)"; tail -n 20 "$work/server.log"; } || true
  exit 1
}
# start_server BIN PORT: migrate, seed the administrator and serve from the current directory (env already set);
# refuses to run when something already answers on the port (a leftover E2E_KEEP server would take the run).
start_server() {
  local bin=$1 p=$2
  if curl -fsS -o /dev/null "http://127.0.0.1:$p/healthz" 2>/dev/null; then fail "port $p already answers /healthz (a leftover server?)"; fi
  "$bin" migrate >"$work/server.log" 2>&1 || fail "migrate"
  HYDROGEN_ADMIN_PASSWORD="$password" "$bin" users:create-admin "$login" >>"$work/server.log" 2>&1 || fail "users:create-admin"
  "$bin" serve >>"$work/server.log" 2>&1 & pid=$!
  for _ in $(seq 200); do curl -fsS -o /dev/null "http://127.0.0.1:$p/healthz" 2>/dev/null && return 0; sleep 0.1; done
  fail "server on port $p did not come up"
}

# run_playwright PORT [ENV=VALUE...]
run_playwright() {
  local p=$1; shift
  (cd "$root/admin/ui" && env "$@" BASE_URL="http://127.0.0.1:$p" MANAGE_TOKEN="$MANAGE_TOKEN" ADMIN_LOGIN="$login" ADMIN_PASSWORD="$password" npx playwright test)
}
trap cleanup EXIT

rm -rf "$work" && mkdir -p "$work"
echo "== build"
"$root/scripts/npm-ci.sh" "$root/admin/ui" >/dev/null && (cd "$root/admin/ui" && npm run build >/dev/null)
(cd "$root/examples/multisite" && go build -o "$work/multisite" .)

# The multisite schema plus the field kinds the editor scenario needs (enum, reference that blocks deletes,
# list of groups); the example's own schema stays as it is.
mkdir -p "$work/schema" && cp "$root/examples/multisite/schema/"*.yaml "$work/schema/"
cat >"$work/schema/page.yaml" <<'YAML'
types:
  page:
    page: true
    children: [page, landing]
    fields:
      title: { type: string, translatable: true, required: true }
      intro: { type: text, translatable: true }
      kind: { type: enum, values: [general, team, location], default: general }
      related: { type: ref, to: [page], on_delete: block }
      sections:
        type: list
        of: { type: group, fields: { heading: { type: string }, body: { type: text } } }
  landing:
    page: true
    children: []
    fields:
      title: { type: string, translatable: true, required: true }
YAML

# The careers channel also delivers German, so an editor confined to careers_en can translate and publish two
# languages.
mkdir -p "$work/config" && cp "$root/examples/multisite/config/"*.yaml "$work/config/"
python3 - "$work/config/channels.yaml" <<'PY'
import re, sys
p = sys.argv[1]
s = open(p).read()
s, n = re.subn(r"(  careers_en:\n    site: careers\n    languages: )\[en\]", r"\1[en, de]", s)
assert n == 1, "careers_en languages not found"
open(p, "w").write(s)
PY

export SCHEMA_DIR="$work/schema" CONFIG_DIR="$work/config"
export DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$port" PREVIEW_SECRET=e2e-preview-secret-0123456789abcdef
export MANAGE_TOKEN=e2e-manage-token
cd "$root/examples/multisite"
start_server "$work/multisite" "$port"

echo "== playwright (multisite)"
status=0
run_playwright "$port" E2E_PROJECT= || status=$?
kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; pid=
[ "$status" -eq 0 ] || exit "$status"

echo "== build examples/content"
(cd "$root/examples/content/admin" && test -d node_modules || { (cd "$root/packages" && npm install --no-audit --no-fund >/dev/null && npm run build -w admin-sdk >/dev/null) && "$root/scripts/npm-ci.sh" . >/dev/null; })
(cd "$root/examples/content" && go build -o "$work/content" .)
cport=${ADMIN_E2E_CONTENT_PORT:-18435}
cwork="$work/content-run"
mkdir -p "$cwork/config"
cp "$root/examples/content/config/"*.yaml "$cwork/config/"
# A module that throws on import when the test sets localStorage "x-broken": the loader must toast once and
# leave the built-in UI working.
cat >"$cwork/broken.js" <<'JS'
if (localStorage.getItem('x-broken')) throw new Error('broken extension module');
JS
python3 - "$cwork/config/app.yaml" "$cwork/broken.js" <<'PY'
import sys
p, broken = sys.argv[1:]
s = open(p).read()
old = "elements: [./admin/dist/elements.js]"
assert old in s
open(p, "w").write(s.replace(old, "elements: [./admin/dist/elements.js, " + broken + "]"))
PY
export CONFIG_DIR="$cwork/config" DATABASE_URL="sqlite:$cwork/h.db" LISTEN_ADDR="127.0.0.1:$cport"
unset SCHEMA_DIR
cd "$root/examples/content"
start_server "$work/content" "$cport"

echo "== playwright (content extensions)"
status=0
run_playwright "$cport" E2E_PROJECT=content || status=$?
if [ -n "${E2E_KEEP:-}" ]; then
  echo "E2E_KEEP: server stays up at http://127.0.0.1:$cport/admin/ (login $login; ctrl-c to stop)"
  sleep 86400 || true
fi
exit $status
