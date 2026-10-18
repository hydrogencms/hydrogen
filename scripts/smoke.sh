#!/usr/bin/env bash
# Skeleton smoke test: hydrogen new → add images → config:check → migrate → build → serve, plus the D0 budgets H0 introduces
# (binary size, cold start, idle memory). Run from the repo root: make smoke.
set -euo pipefail

root=$(pwd)
work="$root/.work/smoke"
port=${SMOKE_PORT:-18431}
max_binary_mb=${MAX_BINARY_MB:-25}
max_start_ms=${MAX_START_MS:-200}
max_rss_mb=${MAX_RSS_MB:-40}

export GOWORK=off
go build -o "$root/.work/bin/hydrogen" ./cmd/hydrogen
hydrogen="$root/.work/bin/hydrogen"

rm -rf "$work"
"$hydrogen" new -module example.com/smoke -core "$root" "$work" >/dev/null
cd "$work"

grep -q '  - sqlite' hydrogen.yaml || { echo "FAIL: scaffold lacks its module list"; exit 1; }
"$hydrogen" add images >/dev/null
grep -q '  - images' hydrogen.yaml && grep -q 'images.Module()' modules_gen.go || { echo "FAIL: hydrogen add images did not wire the module"; exit 1; }
for f in admin/package.json admin/vite.config.ts admin/src/elements.ts admin/src/Hello.svelte admin/logo.svg admin/theme.css admin/strings.json; do
  [ -f "$f" ] || { echo "FAIL: scaffold lacks $f"; exit 1; }
done
"$hydrogen" config:check
"$hydrogen" migrate
"$hydrogen" migrate | grep -q "up to date"

# Production-style binary for the budgets, built the way a user builds it (images included).
"$hydrogen" build -o .work/bin/smoke -ldflags="-s -w" | sed 's/^/build: /'
size_kb=$(du -k .work/bin/smoke | cut -f1)
echo "binary: $((size_kb / 1024)) MB (budget $max_binary_mb MB)"
[ "$size_kb" -le $((max_binary_mb * 1024)) ] || { echo "FAIL: binary over budget"; exit 1; }

# Run once first: macOS scans a newly built binary on its first launch, which isn't Hydrogen's start-up time.
.work/bin/smoke version >/dev/null
if .work/bin/smoke modules >.work/modules.txt 2>&1; then
  grep -q images .work/modules.txt || { cat .work/modules.txt; echo "FAIL: modules does not list images"; exit 1; }
else
  grep -q 'unknown command' .work/modules.txt || { cat .work/modules.txt; echo "FAIL: modules command failed"; exit 1; }
fi

start=$(perl -MTime::HiRes=time -e 'printf "%d", time*1000')
LISTEN_ADDR="127.0.0.1:$port" .work/bin/smoke serve 2>.work/serve.log &
pid=$!
trap 'kill $pid 2>/dev/null || true' EXIT
until curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; do
  kill -0 $pid 2>/dev/null || { cat .work/serve.log; echo "FAIL: server exited"; exit 1; }
  sleep 0.005
done
ready=$(perl -MTime::HiRes=time -e 'printf "%d", time*1000')
echo "cold start: $((ready - start)) ms (budget $max_start_ms ms)"
curl -fsS "http://127.0.0.1:$port/readyz" >/dev/null || { echo "FAIL: not ready"; exit 1; }

sleep 1
rss_kb=$(ps -o rss= -p $pid | tr -d ' ')
echo "idle memory: $((rss_kb / 1024)) MB (budget $max_rss_mb MB)"

kill -INT $pid
wait $pid
trap - EXIT

[ $((ready - start)) -le "$max_start_ms" ] || { echo "FAIL: cold start over budget"; exit 1; }
[ "$rss_kb" -le $((max_rss_mb * 1024)) ] || { echo "FAIL: idle memory over budget"; exit 1; }
echo "smoke ok"
