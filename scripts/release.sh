#!/usr/bin/env bash
# Prepare a release: verify versions, run the gates, and (with --tag) create the annotated tags locally.
# Never pushes, never publishes: the push and npm publish commands are printed for you to run.
#
#   scripts/release.sh v0.1.0                 dry run: checks everything, prints the tags and commands
#   scripts/release.sh v0.1.0 --tag           also creates the tags locally
#   --skip-gates                              skip `make check` and the e2e gates
#   --allow-dirty                             dry runs only: tolerate a dirty tree (refused with --tag)
set -euo pipefail

cd "$(dirname "$0")/.."

# npm packages in dependency order (checked below against their @hydrogencms dependencies).
# shellcheck source=scripts/npm-packages.sh
. scripts/npm-packages.sh
GATES=(conformance admin-e2e next-e2e site-export-e2e wasm-e2e)
CORE=github.com/hydrogencms/hydrogen

usage() {
	sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
}

version=""
tag=0
skip_gates=0
allow_dirty=0
for arg in "$@"; do
	case "$arg" in
	--tag) tag=1 ;;
	--skip-gates) skip_gates=1 ;;
	--allow-dirty) allow_dirty=1 ;;
	-h | --help) usage ;;
	-*) echo "release: unknown flag $arg" >&2; usage ;;
	*) [ -z "$version" ] || usage; version="$arg" ;;
	esac
done
[ -n "$version" ] || usage
version="v${version#v}"
if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
	echo "release: $version is not a semantic version" >&2
	exit 1
fi
npmver="${version#v}"

fail=0
bad() { echo "release: $*" >&2; fail=1; }

if [ "$tag" = 1 ] && [ "$allow_dirty" = 1 ]; then
	echo "release: --allow-dirty is for dry runs; it cannot be combined with --tag" >&2
	exit 1
fi

branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = main ] || bad "on branch $branch, releases are cut from main"
if [ -n "$(git status --porcelain)" ]; then
	if [ "$allow_dirty" = 1 ]; then
		echo "release: warning: dirty tree (--allow-dirty, dry run)" >&2
	else
		bad "the working tree is not clean"
	fi
fi

# Go modules to tag: the root and every module under modules/ with its own go.mod.
tags=("$version")
mods=()
while IFS= read -r f; do
	dir=$(dirname "$f")
	mods+=("$dir")
	tags+=("$dir/$version")
done < <(git ls-files 'modules/*/go.mod' | sort)

for t in "${tags[@]}"; do
	if git rev-parse -q --verify "refs/tags/$t" >/dev/null; then
		[ "$tag" = 0 ] || bad "tag $t already exists"
	fi
done

# Every require of a hydrogen module (examples and tests included) is the release version.
while IFS= read -r f; do
	case "$f" in */testdata/*) continue ;; esac
	while IFS= read -r line; do
		got=${line##* }
		[ "$got" = "$version" ] || bad "$f: $line (want $version)"
	done < <(grep -E "^[[:space:]]*(require[[:space:]]+)?$CORE[^ ]*[[:space:]]+v[^ ]+( // indirect)?$" "$f" | sed 's|[[:space:]]*// indirect||; s|^[[:space:]]*||; s|^require ||')
done < <(git ls-files '*go.mod')

# Every npm package carries the version, and internal ranges are ^version.
for p in "${NPM_ORDER[@]}"; do
	f=packages/$p/package.json
	[ -f "$f" ] || { bad "$f missing"; continue; }
	got=$(sed -n 's/^  "version": "\(.*\)",\{0,1\}$/\1/p' "$f" | head -1)
	[ "$got" = "$npmver" ] || bad "$f: version $got (want $npmver)"
	name=$(sed -n 's/^  "name": "\(.*\)",\{0,1\}$/\1/p' "$f" | head -1)
	[ "$name" = "@hydrogencms/$p" ] || bad "$f: name $name"
	grep -q '"directory": "packages/'"$p"'"' "$f" || bad "$f: repository.directory must be packages/$p (npm provenance checks it)"
	while IFS= read -r dep; do
		d=${dep%%\"*}
		range=${dep#*: \"}; range=${range%%\"*}
		[ "$range" = "^$npmver" ] || bad "$f: @hydrogencms/$d range $range (want ^$npmver)"
		seen=0
		for q in "${NPM_ORDER[@]}"; do
			[ "$q" = "$p" ] && break
			[ "$q" = "$d" ] && seen=1
		done
		[ "$seen" = 1 ] || bad "$f: depends on $d, which is not published before it (fix NPM_ORDER)"
	done < <(grep -oE '"@hydrogencms/[a-z-]+": "[^"]*"' "$f" | sed 's|^"@hydrogencms/||; s|": "|": "|' | sed 's|^\([a-z-]*\)": "\(.*\)"$|\1": "\2"|')
done
for f in packages/*/package.json; do
	p=$(basename "$(dirname "$f")")
	printf '%s\n' "${NPM_ORDER[@]}" | grep -qx "$p" || bad "$f: package $p is not in NPM_ORDER"
done

# The scaffold requires the release version.
rel=$(sed -n 's/^const ReleaseVersion = "\(.*\)"$/\1/p' internal/scaffold/scaffold.go)
[ "$rel" = "$version" ] || bad "internal/scaffold/scaffold.go: ReleaseVersion $rel (want $version)"

# The changelog has a section for the version.
grep -q "^## \[$npmver\]" CHANGELOG.md || bad "CHANGELOG.md has no [$npmver] section"

if [ "$fail" != 0 ]; then
	echo "release: checks failed" >&2
	exit 1
fi
echo "Versions consistent: $version (go modules), $npmver (npm)."

if [ "$skip_gates" = 1 ]; then
	echo "Skipping gates (--skip-gates)."
else
	make check
	make "${GATES[@]}"
fi

echo
if [ "$tag" = 1 ]; then
	for t in "${tags[@]}"; do
		git tag -a "$t" -m "Hydrogen $t"
		echo "tagged $t"
	done
else
	echo "Dry run: tags that --tag would create at $(git rev-parse --short HEAD):"
	printf '  %s\n' "${tags[@]}"
fi

echo
echo "Push (not run; the root tag triggers the GitHub release workflow):"
echo "  git push origin main ${tags[*]}"
echo
echo "Publish to npm (not run): the release workflow does it with provenance when the repository variable"
echo "NPM_PUBLISH is true; by hand, after 'npm login' and a build (cd packages && npm ci && npm run build):"
echo "  scripts/npm-publish.sh            (order: ${NPM_ORDER[*]})"
