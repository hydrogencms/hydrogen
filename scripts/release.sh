#!/usr/bin/env bash
# Prepare a release: verify versions, run the gates, and (with --tag) create the annotated tags locally.
# Never pushes, never publishes: the push and npm publish commands are printed for you to run.
# Releases live on the `release` branch. Cut one from main (--tag fast-forwards release to it) or, for a hotfix,
# from release itself (merge release back into main afterwards).
#
#   scripts/release.sh v0.2.0 --bump          set every version to v0.2.0 (go.mod, package.json, lockfile, bridge,
#                                             scaffold, a dated CHANGELOG section), check, and stop: review and commit
#   scripts/release.sh v0.2.0                 dry run: checks everything, prints the tags and commands
#   scripts/release.sh v0.2.0 --tag           also creates the tags locally
#   --skip-gates                              skip `make check` and the e2e gates
#   --allow-dirty                             dry runs only: tolerate a dirty tree (refused with --tag)
set -euo pipefail

cd "$(dirname "$0")/.."

# npm packages in dependency order (checked below against their @hydrogencms dependencies).
# shellcheck source=scripts/npm-packages.sh
. scripts/npm-packages.sh
GATES=(sdk conformance admin-e2e next-e2e site-export-e2e wasm-e2e)
CORE=github.com/hydrogencms/hydrogen

usage() {
	sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
}

version=""
tag=0
bump=0
skip_gates=0
allow_dirty=0
for arg in "$@"; do
	case "$arg" in
	--tag) tag=1 ;;
	--bump) bump=1 ;;
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
if [ "$bump" = 1 ] && [ "$tag" = 1 ]; then
	echo "release: --bump changes files; commit them and pass CI before --tag" >&2
	exit 1
fi

# Every version the checks below expect, rewritten in place (sed -i.bak works with BSD and GNU sed).
bump_versions() {
	local f
	edit() { f=$1; shift; sed -E -i.bak "$@" "$f" && rm "$f.bak"; }
	while IFS= read -r f; do
		case "$f" in */testdata/*) continue ;; esac
		edit "$f" -e "s#^([[:space:]]*(require[[:space:]]+)?$CORE[^ ]*[[:space:]]+)v[^ ]+( // indirect)?\$#\1$version\3#"
	done < <(git ls-files '*go.mod')
	for p in "${NPM_ORDER[@]}"; do
		edit "packages/$p/package.json" -e "s#^(  \"version\": \")[^\"]*#\1$npmver#" \
			-e "s#(\"@hydrogencms/[a-z-]+\": \")\^[^\"]*#\1^$npmver#g"
	done
	edit packages/bridge/src/index.ts -e "s#^(export const SDK = '@hydrogencms/bridge@)[^']*#\1$npmver#"
	edit internal/scaffold/scaffold.go -e "s#^(const ReleaseVersion = \")[^\"]*#\1$version#" \
		-e "s#^(const npmRange = \"\^)[^\"]*#\1$npmver#"
	if ! grep -q "^## \[$npmver\]" CHANGELOG.md; then
		grep -q '^## \[Unreleased\]' CHANGELOG.md || { echo "release: CHANGELOG.md has no [Unreleased] section to release" >&2; exit 1; }
		awk -v v="$npmver" -v d="$(date +%Y-%m-%d)" \
			'!done && /^## \[Unreleased\]/ { print; print ""; print "## [" v "] - " d; done = 1; next } { print }' \
			CHANGELOG.md >CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md
	fi
	(cd packages && npm install --package-lock-only --ignore-scripts --no-audit --no-fund >/dev/null)
	make site-bridge >/dev/null
}

branch=$(git rev-parse --abbrev-ref HEAD)
case "$branch" in
main)
	if git rev-parse -q --verify refs/heads/release >/dev/null; then
		git merge-base --is-ancestor release HEAD || bad "release has commits main lacks: merge release into main first"
	else
		git rev-parse -q --verify refs/remotes/origin/release >/dev/null || bad "no release branch (git fetch origin release)"
		git merge-base --is-ancestor origin/release HEAD || bad "origin/release has commits main lacks: merge it into main first"
	fi
	;;
release) ;;
*) bad "on branch $branch, releases are cut from main (or from release for a hotfix)" ;;
esac
if [ -n "$(git status --porcelain)" ]; then
	if [ "$allow_dirty" = 1 ]; then
		echo "release: warning: dirty tree (--allow-dirty, dry run)" >&2
	else
		bad "the working tree is not clean"
	fi
fi
if [ "$bump" = 1 ]; then
	[ "$fail" = 0 ] || { echo "release: nothing changed" >&2; exit 1; }
	bump_versions
	echo "Set every version to $version; checking."
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
# The bridge announces its version to the admin (ready.sdk); it must match the package.
grep -q "^export const SDK = '@hydrogencms/bridge@$npmver'" packages/bridge/src/index.ts ||
	bad "packages/bridge/src/index.ts: SDK constant is not @hydrogencms/bridge@$npmver"
for f in packages/*/package.json; do
	p=$(basename "$(dirname "$f")")
	printf '%s\n' "${NPM_ORDER[@]}" | grep -qx "$p" || bad "$f: package $p is not in NPM_ORDER"
done

# The scaffold requires the release version.
rel=$(sed -n 's/^const ReleaseVersion = "\(.*\)"$/\1/p' internal/scaffold/scaffold.go)
[ "$rel" = "$version" ] || bad "internal/scaffold/scaffold.go: ReleaseVersion $rel (want $version)"
rng=$(sed -n 's/^const npmRange = "\(.*\)"$/\1/p' internal/scaffold/scaffold.go)
[ "$rng" = "^$npmver" ] || bad "internal/scaffold/scaffold.go: npmRange $rng (want ^$npmver)"

# The changelog has a section for the version.
grep -q "^## \[$npmver\]" CHANGELOG.md || bad "CHANGELOG.md has no [$npmver] section"

if [ "$fail" != 0 ]; then
	echo "release: checks failed" >&2
	exit 1
fi
echo "Versions consistent: $version (go modules), $npmver (npm)."
if [ "$bump" = 1 ]; then
	cat <<-EOF

	Next (nothing is committed):
	  git diff                                  review; write the CHANGELOG section's summary if it needs one
	  git commit -am "Release $version" && git push origin main
	  wait for CI (ci-ok) on that commit, then: scripts/release.sh $version --tag
	EOF
	exit 0
fi

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
	if [ "$branch" = main ]; then
		git branch -f release HEAD
		echo "release fast-forwarded to $(git rev-parse --short HEAD)"
	fi
else
	echo "Dry run: tags that --tag would create at $(git rev-parse --short HEAD):"
	printf '  %s\n' "${tags[@]}"
fi

echo
echo "Push (not run). GitHub accepts the release branch only once CI passed on the commit, so push main and wait for"
echo "CI first. The root tag goes alone, after the branch: it triggers the release workflow (which requires the tag on"
echo "release), and GitHub starts no workflow for tags pushed more than three at a time."
heads=main
[ "$branch" = main ] || heads=""
echo "  git push --atomic origin $heads release ${tags[*]:1} && git push origin ${tags[0]}"
[ "$branch" = main ] || echo "then bring the hotfix to main: git switch main && git merge release"
echo
echo "Publish to npm (not run): the release workflow does it with provenance when the repository variable"
echo "NPM_PUBLISH is true; by hand, after 'npm login' and a build (cd packages && npm ci && npm run build):"
echo "  scripts/npm-publish.sh            (order: ${NPM_ORDER[*]})"
