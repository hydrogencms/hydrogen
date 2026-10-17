#!/usr/bin/env bash
# Admin end to end: builds admin/dist, starts examples/multisite on a scratch SQLite database with a seeded
# administrator, runs the Playwright tests in admin/ui against it and stops the server. Then does the same with
# examples/content (its extensions spec only, on another port and database). E2E_KEEP=1 leaves the last server
# running afterwards. A third run uses examples/starter with its fixture imported and a tiny preview host standing
# in for the frontend (admin/ui/e2e/fixtures/preview-host): the block editor and the live and reload previews.
# Then the H6 exit scenarios on the real frontends: examples/starter with packages/starters/next (live preview of a page and a
# post) and examples/fullstack (the site module, reload mode). ADMIN_E2E_ONLY=multisite|starter|next|fullstack runs just that one
# (starter uses the admin/dist already built); E2E_GREP passes --grep to Playwright.
# Run from the repo root: make admin-e2e.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/admin-e2e"
port=${ADMIN_E2E_PORT:-18434}
login=${ADMIN_LOGIN:-admin}
password=${ADMIN_PASSWORD:-e2e-admin-password-1}
pid=
hpid=
npid=

cleanup() {
  [ -z "$pid" ] || kill "$pid" 2>/dev/null || true
  [ -z "$hpid" ] || kill "$hpid" 2>/dev/null || true
  [ -z "$npid" ] || kill "$npid" 2>/dev/null || true
  wait 2>/dev/null || true
}
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
  (cd "$root/admin/ui" && env "$@" BASE_URL="http://127.0.0.1:$p" MANAGE_TOKEN="$MANAGE_TOKEN" ADMIN_LOGIN="$login" ADMIN_PASSWORD="$password" npx playwright test ${E2E_GREP:+--grep "$E2E_GREP"})
}
trap cleanup EXIT

only=${ADMIN_E2E_ONLY:-}
port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
stop_servers() {
  [ -z "$pid" ] || { kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; pid=; }
  [ -z "$hpid" ] || { kill "$hpid" 2>/dev/null || true; wait "$hpid" 2>/dev/null || true; hpid=; }
  [ -z "$npid" ] || { kill "$npid" 2>/dev/null || true; wait "$npid" 2>/dev/null || true; npid=; }
}
[ -n "$only" ] || rm -rf "$work"
mkdir -p "$work"
export MANAGE_TOKEN=e2e-manage-token PREVIEW_SECRET=e2e-preview-secret-0123456789abcdef
if [ -z "$only" ] || [ "$only" = multisite ]; then
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

# Articles need an approved review before they publish (H8b: review.spec.ts, history.spec.ts). The principals the
# other specs publish with (the management token, the administrator) hold review.bypass.
python3 - "$work/schema/article.yaml" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
old = "  article:\n"
assert old in s
open(p, "w").write(s.replace(old, old + "    review: true\n", 1).rstrip("\n") + "\n      hero: { type: asset, to: [image] }\n")
PY

# H9a media: an upload type (media.spec.ts); the article's asset field is added above.
cat >"$work/schema/image.yaml" <<'YAML'
types:
  image:
    upload: { accept: ["image/*", application/pdf], max_size: 5MB }
    fields:
      alt: { type: string, translatable: true, max: 300 }
      caption: { type: text, translatable: true }
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
# Assets show in delivery only where the channel delivers their type (media.spec.ts).
s, n = re.subn(r"(    collections: \[page, landing, article, product)\]", r"\1, image]", s)
assert n == 1, "corporate_web collections not found"
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
if [ -n "${E2E_KEEP:-}" ] && [ "$only" = multisite ]; then
  echo "E2E_KEEP: server stays up at http://127.0.0.1:$port/admin/ (login $login; ctrl-c to stop)"
  sleep 86400 || true
fi
kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; pid=
[ "$status" -eq 0 ] || exit "$status"

fi

if [ -z "$only" ]; then
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
kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; pid=
[ "$status" -eq 0 ] || exit "$status"
fi

if [ -z "$only" ] || [ "$only" = starter ]; then
echo "== build examples/starter and the preview host"
test -f "$root/packages/bridge/dist/bridge.iife.js" || (cd "$root/packages" && npm install --no-audit --no-fund >/dev/null && npm run build -w bridge >/dev/null)
(cd "$root/examples/starter" && go build -o "$work/starter" .)
sport=${ADMIN_E2E_STARTER_PORT:-18436}
hport=${ADMIN_E2E_HOST_PORT:-18601}
swork="$work/starter-run"
rm -rf "$swork" && mkdir -p "$swork"
if curl -fsS -o /dev/null "http://127.0.0.1:$hport/bridge.js" 2>/dev/null; then fail "port $hport already answers (a leftover preview host?)"; fi
CMS_URL="http://127.0.0.1:$sport" ADMIN_ORIGIN="http://127.0.0.1:$sport" PORT="$hport" node "$root/admin/ui/e2e/fixtures/preview-host/server.mjs" >"$work/host.log" 2>&1 & hpid=$!
export DATABASE_URL="sqlite:$swork/h.db" LISTEN_ADDR="127.0.0.1:$sport" STARTER_FRONTEND="http://127.0.0.1:$hport"
unset SCHEMA_DIR CONFIG_DIR
cd "$root/examples/starter"
"$work/starter" migrate >"$work/server.log" 2>&1 || fail "starter migrate"
"$work/starter" import fixture >>"$work/server.log" 2>&1 || fail "starter import"
start_server "$work/starter" "$sport"

echo "== playwright (starter: block editor, preview and share links)"
run_playwright "$sport" E2E_PROJECT=starter PREVIEW_HOST="http://127.0.0.1:$hport" || exit $?
stop_servers
fi

# The same scratch CMS setup for the real frontends: a database with the fixture, an administrator, a server.
# new_cms NAME BIN PORT FIXTURE_DIR: runs in the example's directory.
new_cms() {
  local name=$1 bin=$2 p=$3 fixture=$4
  rm -rf "$work/run-$name" && mkdir -p "$work/run-$name"
  export DATABASE_URL="sqlite:$work/run-$name/h.db" LISTEN_ADDR="127.0.0.1:$p"
  "$bin" migrate >"$work/server.log" 2>&1 || fail "$name migrate"
  "$bin" import "$fixture" >>"$work/server.log" 2>&1 || fail "$name import"
  start_server "$bin" "$p"
}
wait_http() { # url, seconds
  for _ in $(seq $(($2 * 10))); do curl -fsS -o /dev/null "$1" 2>/dev/null && return 0; sleep 0.1; done
  fail "$1 did not come up"
}

if [ -z "$only" ] || [ "$only" = next ]; then
  echo "== build the Next.js starter"
  (cd "$root/examples/starter" && go build -o "$work/starter" .)
  "$root/scripts/npm-ci.sh" "$root/packages" >/dev/null
  (cd "$root/packages" && npm run build >/dev/null)
  (cd "$root/packages/starters/next" && rm -rf node_modules/@hydrogencms && npm install --no-audit --no-fund >/dev/null)
  cport=$(port); nport=$(port)
  cms=http://127.0.0.1:$cport; nxt=http://127.0.0.1:$nport
  unset SCHEMA_DIR CONFIG_DIR
  export STARTER_FRONTEND="$nxt"
  cd "$root/examples/starter"
  new_cms next "$work/starter" "$cport" fixture
  echo "== next build + start"
  export HYDROGEN_URL=$cms HYDROGEN_ORIGIN=$nxt
  (cd "$root/packages/starters/next" && rm -rf .next && npm run build >"$work/next-build.log" 2>&1) || { tail -n 30 "$work/next-build.log"; fail "next build"; }
  (cd "$root/packages/starters/next" && exec node_modules/.bin/next start -p "$nport" >"$work/next.log" 2>&1) & npid=$!
  wait_http "$nxt/en" 30
  echo "== playwright (the starter's Next.js frontend: live preview of a page and a post)"
  status=0
  run_playwright "$cport" E2E_PROJECT=next FRONTEND_URL="$nxt" || status=$?
  [ "$status" -eq 0 ] || { [ ! -s "$work/next.log" ] || { echo "--- next.log (tail)"; tail -n 15 "$work/next.log"; }; exit "$status"; }
  stop_servers
fi

if [ -z "$only" ] || [ "$only" = fullstack ]; then
  echo "== build examples/fullstack"
  (cd "$root/examples/fullstack" && go build -o "$work/fullstack" .)
  fport=$(port)
  site=http://127.0.0.1:$fport
  unset SCHEMA_DIR CONFIG_DIR
  export SITE_URL="$site" ADMIN_URL="$site"
  cd "$root/examples/fullstack"
  new_cms fullstack "$work/fullstack" "$fport" ../starter/fixture
  echo "== playwright (the site module: reload mode and share links)"
  status=0
  run_playwright "$fport" E2E_PROJECT=fullstack || status=$?
  if [ -n "${E2E_KEEP:-}" ]; then
    echo "E2E_KEEP: server stays up at $site/admin/ (login $login; ctrl-c to stop)"
    sleep 86400 || true
  fi
  exit $status
fi
