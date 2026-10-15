#!/usr/bin/env bash
# Admin end to end: builds admin/dist, starts examples/multisite on a scratch SQLite database with a seeded
# administrator, runs the Playwright tests in admin/ui against it and stops the server. E2E_KEEP=1 leaves the
# server running afterwards. Run from the repo root: make admin-e2e.
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
trap cleanup EXIT

rm -rf "$work" && mkdir -p "$work"
echo "== build"
(cd "$root/admin/ui" && npm ci --no-audit --no-fund >/dev/null && npm run build >/dev/null)
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
"$work/multisite" migrate >"$work/server.log" 2>&1
HYDROGEN_ADMIN_PASSWORD="$password" "$work/multisite" users:create-admin "$login" >>"$work/server.log" 2>&1 || fail "users:create-admin"
"$work/multisite" serve >>"$work/server.log" 2>&1 & pid=$!
for _ in $(seq 200); do curl -fsS -o /dev/null "http://127.0.0.1:$port/healthz" 2>/dev/null && break; sleep 0.1; done
curl -fsS -o /dev/null "http://127.0.0.1:$port/healthz" || fail "server did not come up"

echo "== playwright"
status=0
(cd "$root/admin/ui" && BASE_URL="http://127.0.0.1:$port" MANAGE_TOKEN="$MANAGE_TOKEN" ADMIN_LOGIN="$login" ADMIN_PASSWORD="$password" npx playwright test) || status=$?
if [ -n "${E2E_KEEP:-}" ]; then
  echo "E2E_KEEP: server stays up at http://127.0.0.1:$port/admin/ (login $login; ctrl-c to stop)"
  sleep 86400 || true
fi
exit $status
