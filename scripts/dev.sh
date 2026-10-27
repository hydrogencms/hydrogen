#!/usr/bin/env bash
# A project on this checkout with the admin on Vite's hot reload: creates it (hydrogen new -core) when the directory
# has no go.mod, then runs the admin dev server and `hydrogen dev` with ADMIN_DEV_URL. Ctrl-C stops both.
# Usage: scripts/dev.sh [dir] (default .work/dev); ADMIN_DEV_PORT (default 5173) moves Vite.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
dir=${1:-$root/.work/dev}
mkdir -p "$dir"
dir=$(cd "$dir" && pwd)
port=${ADMIN_DEV_PORT:-5173}
hydrogen="$root/.work/bin/hydrogen"

(cd "$root" && go build -o "$hydrogen" ./cmd/hydrogen)
if [ ! -f "$dir/go.mod" ]; then
  "$hydrogen" new -core "$root" "$dir" >/dev/null
  echo "dev: created $dir"
elif ! grep -q 'github.com/hydrogencms/hydrogen =>' "$dir/go.mod"; then
  echo "dev: warning: $dir/go.mod has no replace directive to this checkout; it builds against published versions" >&2
fi
"$root/scripts/npm-ci.sh" "$root/admin/ui" >/dev/null

(cd "$root/admin/ui" && exec node_modules/.bin/vite --port "$port" --strictPort --clearScreen false) &
vite=$!
trap 'kill $vite 2>/dev/null || true' EXIT

# Inside this repository go.work would claim the project; its go.mod replace directives point at the checkout.
cd "$dir"
GOWORK=off ADMIN_DEV_URL="http://localhost:$port" "$hydrogen" dev
