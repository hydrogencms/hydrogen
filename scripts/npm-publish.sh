#!/usr/bin/env bash
# Publish the npm packages in dependency order. Run after `cd packages && npm ci && npm run build`.
# Used by the release workflow (with --provenance) and by hand. Versions already on the registry are skipped, so a
# failed run can be repeated; the first failure stops the run, so no package goes out before its dependencies.
#
#   scripts/npm-publish.sh                  publish every package not yet on the registry
#   --provenance                            sign with npm provenance (GitHub Actions with id-token: write only)
#   --dry-run                               npm publish --dry-run for each package; publishes nothing
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/npm-packages.sh
. scripts/npm-packages.sh

usage() {
	sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
}

flags=(--access public)
dry=0
for arg in "$@"; do
	case "$arg" in
	--provenance) flags+=(--provenance) ;;
	--dry-run) flags+=(--dry-run); dry=1 ;;
	-h | --help) usage ;;
	*) echo "npm-publish: unknown argument $arg" >&2; usage ;;
	esac
done

for p in "${NPM_ORDER[@]}"; do
	dir=packages/$p
	name=$(node -p "require('./$dir/package.json').name")
	version=$(node -p "require('./$dir/package.json').version")
	[ -d "$dir/dist" ] || { echo "npm-publish: $dir/dist is missing; build the packages first" >&2; exit 1; }
	if [ "$dry" = 0 ] && [ "$(npm view "$name@$version" version 2>/dev/null || true)" = "$version" ]; then
		echo "== $name@$version is already published; skipping"
		continue
	fi
	# Pre-releases (0.2.0-rc.1) go to the "next" dist-tag so `npm install` keeps resolving the latest release.
	tag=()
	case "$version" in *-*) tag=(--tag next) ;; esac
	echo "== $name@$version"
	(cd "$dir" && npm publish "${flags[@]}" ${tag[@]+"${tag[@]}"})
done
