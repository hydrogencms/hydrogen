#!/usr/bin/env bash
# Build a release commit and its tags in this clone. The release commit has the source commit's tree plus the admin
# built from it (admin/dist, not committed on main), parents (source, previous origin/release) so `release` only
# fast-forwards, and the message "Release vX" with a "Source: <sha>" trailer. The Go module tags point at it.
# The release workflow runs this with --push; run by hand it stops before the push and prints the commands.
#
#   scripts/release-commit.sh v0.2.0 <source-ref>            build the commit and the tags (checks out the source, detached)
#   scripts/release-commit.sh v0.2.0 <source-ref> --push     also push release and the tags to origin
set -euo pipefail

cd "$(dirname "$0")/.."

push=0
args=()
for arg in "$@"; do
	case "$arg" in
	--push) push=1 ;;
	-h | --help) sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	-*) echo "release-commit: unknown flag $arg" >&2; exit 2 ;;
	*) args+=("$arg") ;;
	esac
done
[ "${#args[@]}" = 2 ] || { echo "usage: scripts/release-commit.sh <version> <source-ref> [--push]" >&2; exit 2; }
version="v${args[0]#v}"
fail() { echo "release-commit: $*" >&2; exit 1; }

[ -z "$(git status --porcelain)" ] || fail "the working tree is not clean"
src=$(git rev-parse --verify -q "${args[1]}^{commit}") || fail "${args[1]} is not a commit"
[ "$(git rev-parse HEAD)" = "$src" ] || git checkout -q --detach "$src"

scripts/release.sh "$version" --verify

tags=("$version")
while IFS= read -r f; do
	tags+=("$(dirname "$f")/$version")
done < <(git ls-files 'modules/*/go.mod' | sort)

has_origin=0
git remote get-url origin >/dev/null 2>&1 && has_origin=1
if [ "$has_origin" = 1 ] && git ls-remote --exit-code --heads origin release >/dev/null 2>&1; then
	git fetch -q origin release:refs/remotes/origin/release
fi
for t in "${tags[@]}"; do
	! git rev-parse -q --verify "refs/tags/$t" >/dev/null || fail "tag $t already exists"
	[ "$has_origin" = 0 ] || [ -z "$(git ls-remote --tags origin "refs/tags/$t")" ] || fail "tag $t already exists on origin"
done
prev=""
git rev-parse -q --verify refs/remotes/origin/release >/dev/null && prev=$(git rev-parse origin/release)

make admin
[ -f admin/dist/index.html ] || fail "make admin left no admin/dist/index.html"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
GIT_INDEX_FILE="$tmp/index" git read-tree "$src"
tree=$(GIT_INDEX_FILE="$tmp/index" sh -c 'git add -f admin/dist && git write-tree')
parents=(-p "$src")
[ -z "$prev" ] || [ "$prev" = "$src" ] || parents+=(-p "$prev")
rel=$(printf 'Release %s\n\nSource: %s\n' "$version" "$src" | git commit-tree "$tree" "${parents[@]}")

git cat-file -e "$rel:admin/dist/index.html" || fail "the release commit lacks admin/dist/index.html"
# Everything but admin/dist is the source's, byte for byte.
outside() { git ls-tree -r "$1" | awk -F'\t' '$2 !~ /^admin\/dist\//'; }
[ "$(outside "$rel")" = "$(outside "$src")" ] || fail "the release commit differs from the source outside admin/dist"
[ -z "$prev" ] || git merge-base --is-ancestor "$prev" "$rel" || fail "the previous release is not an ancestor of the release commit"

# The module as a user gets it: no Node, the admin embedded from the commit.
mkdir "$tmp/export"
git archive "$rel" | tar -x -C "$tmp/export"
(
	cd "$tmp/export"
	GOWORK=off go build ./...
	GOWORK=off go list -f '{{join .EmbedFiles " "}}' ./admin | tr ' ' '\n' | grep -qx 'dist/index.html'
) || fail "the release commit does not build with the admin embedded"

for t in "${tags[@]}"; do
	git tag -a "$t" -m "Hydrogen $t" "$rel"
done
echo "release commit $rel (source $src, previous ${prev:-none}), tagged: ${tags[*]}"

refs=()
for t in "${tags[@]:1}"; do refs+=("refs/tags/$t"); done
if [ "$push" = 1 ]; then
	git push --atomic origin "$rel:refs/heads/release" "${refs[@]}"
	# The root tag alone, after the branch and the module tags.
	git push origin "refs/tags/$version"
else
	echo "Not pushed. To publish:"
	echo "  git push --atomic origin $rel:refs/heads/release ${refs[*]} && git push origin refs/tags/$version"
fi
