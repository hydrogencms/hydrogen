#!/usr/bin/env bash
# TypeScript WebAssembly hooks end to end: a scratch project (hydrogen new -core → add wasmhooks), two TypeScript hooks
# compiled by `hydrogen hooks build` with the pinned Javy (scripts/javy.sh), served, and asserted through the
# management API. Needs node/npm and network for the Javy download and esbuild. Run from the repo root: make wasm-e2e.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/wasm-e2e"
port=${WASM_E2E_PORT:-18437}
base="http://127.0.0.1:$port/api/manage/v1"
pid=

cleanup() { [ -n "$pid" ] && kill "$pid" 2>/dev/null || true; wait 2>/dev/null || true; }
fail() { echo "FAIL: $*" >&2; [ -s "$work/serve.log" ] && { echo "--- serve.log (tail)"; tail -n 20 "$work/serve.log"; } || true; exit 1; }
trap cleanup EXIT

export GOWORK=off GOFLAGS=-mod=mod
export JAVY=$("$root/scripts/javy.sh")

echo "== build"
mkdir -p "$root/.work/bin"
go build -o "$root/.work/bin/hydrogen" "$root/cmd/hydrogen"
hydrogen="$root/.work/bin/hydrogen"
"$root/scripts/npm-ci.sh" "$root/packages" >/dev/null
(cd "$root/packages" && npm run build --workspace @hydrogencms/hook >/dev/null)

echo "== project"
rm -rf "$work"
"$hydrogen" new -module example.com/hooks -core "$root" "$work" >/dev/null
cd "$work"
"$hydrogen" add wasmhooks >/dev/null
mkdir -p schema hooks
cat > schema/article.yaml <<'YAML'
types:
  article:
    name: "{title}"
    fields:
      title:          { type: string, required: true, max: 120 }
      readingMinutes: { type: number, integer: true }
YAML
"$hydrogen" generate -ts gen/types.ts >/dev/null

cat > package.json <<'JSON'
{ "name": "hooks-e2e", "private": true, "type": "module" }
JSON
npm install --no-audit --no-fund --save-dev esbuild "file:$root/packages/hook" >/dev/null 2>"$work/npm.log" || { cat "$work/npm.log"; fail "npm install"; }

cat > hooks/reading-time.ts <<'TS'
import { defineHook } from '@hydrogencms/hook'
import type { Article } from '../gen/types'

defineHook<Article, 'beforeSave'>((req) => ({ set: { readingMinutes: (req.doc?.title ?? '').length } }))
TS
cat > hooks/no-forbidden.ts <<'TS'
import { defineHook } from '@hydrogencms/hook'
import type { Article } from '../gen/types'

defineHook<Article, 'validate'>((req) =>
  (req.doc?.title ?? '').includes('forbidden') ? { errors: [{ path: 'title', message: 'The title contains a forbidden word' }] } : {},
)
TS

echo "== hooks build"
"$hydrogen" hooks build
[ -s hooks/reading-time.wasm ] && [ -s hooks/no-forbidden.wasm ] || fail "hooks build produced no wasm"

cat >> config/app.yaml <<'YAML'

modules:
  wasmhooks:
    dir: hooks
    timeout: 2s
    hooks:
      - { file: reading-time.wasm, event: beforeSave, types: [article] }
      - { file: no-forbidden.wasm, event: validate, types: [article] }
YAML
export LISTEN_ADDR="127.0.0.1:$port" MANAGE_TOKEN=e2e-token DATABASE_URL="sqlite:$work/.work/e2e.db"
"$hydrogen" config:check >/dev/null
"$hydrogen" migrate >/dev/null

if curl -sS -o /dev/null --max-time 2 "http://127.0.0.1:$port/" 2>/dev/null || (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
  fail "port $port is already in use (set WASM_E2E_PORT)"
fi
echo "== serve on :$port"
"$hydrogen" serve >"$work/serve.log" 2>&1 & pid=$!
for _ in $(seq 300); do curl -fsS -o /dev/null "http://127.0.0.1:$port/healthz" 2>/dev/null && break; kill -0 "$pid" 2>/dev/null || fail "server exited"; sleep 0.1; done

post() { # json → $status, $body
  status=$(curl -sS -o "$work/body" -w '%{http_code}' -X POST -H 'Authorization: Bearer e2e-token' -H 'Content-Type: application/json' -d "$1" "$base/collections/article/items")
  body=$(cat "$work/body")
}

post '{"locale":"en","fields":{"title":"Hello Wasm"}}'
[ "$status" = 201 ] || fail "create: status $status: $body"
echo "$body" | grep -Eq '"readingMinutes": ?10\b' || fail "beforeSave hook did not set readingMinutes=10: $body"
echo "ok: beforeSave computed readingMinutes"

post '{"locale":"en","fields":{"title":"A forbidden title"}}'
[ "$status" = 422 ] || fail "forbidden title: status $status: $body"
echo "$body" | grep -q 'forbidden word' && echo "$body" | grep -q '"title"' || fail "validate error missing: $body"
echo "ok: validate hook rejected the title (422)"

echo "wasm-e2e ok"
