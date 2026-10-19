#!/usr/bin/env bash
# examples/fullstack (SQLite, the starter fixture imported) exported to static files by the site-export module:
# a full export (files, manifest, redirect formats, the dir publisher's release), then the running server
# re-exports incrementally after a publish, an unpublish and a new redirect made through the management API.
# Run from the repo root: make site-export-e2e. E2E_KEEP=1 leaves the work directory.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/site-export-e2e"
manage_token=e2e-token
pids=()

cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
fail() {
  echo "FAIL: $*" >&2
  [ -s "$work/server.log" ] && { echo "--- server.log (tail)"; tail -n 20 "$work/server.log"; } || true
  exit 1
}
trap cleanup EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
# js <expression over j> <<< json: prints the value
js() { node -e "let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{const j=d?JSON.parse(d):null;const v=($1);if(v!==undefined)console.log(typeof v==='object'?JSON.stringify(v):v)})"; }
api() { curl -fsS -H "Authorization: Bearer $manage_token" -H 'Content-Type: application/json' "$@"; }
wait_for() { # wait_for "description" command...
  local what=$1; shift
  for _ in $(seq 200); do "$@" 2>/dev/null && return 0; sleep 0.1; done
  fail "timed out waiting for $what"
}

rm -rf "$work" && mkdir -p "$work"
p=$(port)
url=http://127.0.0.1:$p
out="$work/out"
pub="$work/public"

echo "== build"
fs="$root/examples/fullstack"
build_args=()
if ! grep -q siteexport "$fs/main.go"; then
  # The example may not list the module yet: build it with a patched main.go through an overlay.
  sed -e 's#"github.com/hydrogencms/hydrogen/modules/site"#&\n\t"github.com/hydrogencms/hydrogen/modules/siteexport"#' \
      -e 's#site.Module())#site.Module(), siteexport.Module())#' "$fs/main.go" >"$work/main.go"
  printf '{"Replace":{"%s":"%s"}}' "$fs/main.go" "$work/main.go" >"$work/overlay.json"
  build_args=(-overlay "$work/overlay.json")
fi
(cd "$fs" && go build ${build_args[@]+"${build_args[@]}"} -o "$work/fullstack" .)

# The example's config plus the module's, as a dev overlay.
cp -R "$fs/config" "$work/config"
mkdir -p "$work/config/env"
if grep -q 'site-export' "$work/config/app.yaml"; then fail "the example configures site-export itself: adjust this script"; fi
cat >"$work/config/env/dev.yaml" <<YAML
modules:
  site-export:
    out: $out
    auto: true
    concurrency: 4
    redirects: [netlify, nginx, s3, html]
    publisher: { dir: { path: $pub, keep: 2 } }
YAML

export DATABASE_URL="sqlite:$work/h.db" LISTEN_ADDR="127.0.0.1:$p" MANAGE_TOKEN="$manage_token" SITE_URL="$url" CONFIG_DIR="$work/config"
cd "$fs"
"$work/fullstack" migrate >"$work/server.log" 2>&1 && "$work/fullstack" import ../starter/fixture >>"$work/server.log" 2>&1 || fail "migrate/import"
"$work/fullstack" config:check >>"$work/server.log" 2>&1 || fail "config:check"

echo "== full export"
"$work/fullstack" site:export >"$work/export.log" 2>&1 || { cat "$work/export.log"; fail "site:export"; }
cat "$work/export.log"
for f in manifest.json en/en/index.html de/de/index.html en/en/404.html de/de/404.html en/_redirects redirects.nginx.conf \
         en/s3-routing-rules.json en/en/colophon/index.html; do
  [ -s "$out/$f" ] || fail "missing $f"
done
ls "$out"/en/__hydrogen/assets/*.css >/dev/null || fail "no fingerprinted css"
grep -q '<html' "$out/en/en/index.html" || fail "home is not HTML"
grep -q '__hydrogen/bridge.js' "$out/en/en/index.html" && fail "the bridge was exported"
pages=$(js 'Object.keys(j.files).filter(f=>f.endsWith("/index.html")).length' <"$out/manifest.json")
[ "$pages" -gt 10 ] || fail "only $pages pages in the manifest"
js 'Object.values(j.files).every(e=>e.hash.length===64)' <"$out/manifest.json" | grep -q true || fail "manifest hashes"
js 'Object.values(j.files).filter(e=>e.kind==="page"||e.kind==="item").every(e=>e.tags&&e.tags.length>0)' <"$out/manifest.json" | grep -q true || fail "pages without tags"
[ -s "$pub/current/en/en/index.html" ] || fail "the dir publisher published nothing"
cmp "$out/en/en/index.html" "$pub/current/en/en/index.html" || fail "published home differs"
find "$out" -type f ! -name manifest.json -print0 | sort -z | xargs -0 shasum >"$work/full.sums"

echo "== repeat is a no-op"
"$work/fullstack" site:export >"$work/export2.log" 2>&1 || fail "second site:export"
grep -q 'wrote 0,' "$work/export2.log" || { cat "$work/export2.log"; fail "a repeated export wrote files"; }

echo "== serve (auto export), then change content through the management API"
"$work/fullstack" serve >>"$work/server.log" 2>&1 & pids+=($!)
wait_for "the server" curl -fsS -o /dev/null "$url/healthz"

list=$(api "$url/api/manage/v1/collections/post/items?locale=en&limit=100")
ids=$(echo "$list" | js '(j.items||j.data||j).map(i=>i.id).slice(0,2).join(" ")')
read -r changed gone <<<"$ids"
[ -n "$changed" ] && [ -n "$gone" ] || fail "need two posts: $list"
file_of() { js "Object.entries(j.files).filter(([f,e])=>e.kind==='item'&&e.tags.includes('i:$1')&&f.startsWith('en/')).map(([f])=>f)[0]" <"$out/manifest.json"; }
changed_file=$(file_of "$changed"); gone_file=$(file_of "$gone")
[ -n "$changed_file" ] && [ -n "$gone_file" ] || fail "posts have no files ($changed_file, $gone_file)"
echo "   changing $changed_file, unpublishing $gone_file"
home_hash=$(shasum "$out/en/en/colophon/index.html")

api -X PATCH "$url/api/manage/v1/items/$changed" -d '{"locale":"en","fields":{"title":"Exported again"}}' >/dev/null || fail "update"
api -X POST "$url/api/manage/v1/items/$changed/publish" -d '{"locales":["en"]}' >/dev/null || fail "publish"
wait_for "the re-rendered post" grep -q 'Exported again' "$out/$changed_file"
wait_for "the release" grep -q 'Exported again' "$pub/current/$changed_file"
[ "$home_hash" = "$(shasum "$out/en/en/colophon/index.html")" ] || fail "an unrelated page was rewritten"

api -X POST "$url/api/manage/v1/items/$gone/unpublish" >/dev/null || fail "unpublish"
wait_for "the removed file" test ! -e "$out/$gone_file"
wait_for "the removed file in the release" test ! -e "$pub/current/$gone_file"
js "'$gone_file' in j.files" <"$out/manifest.json" | grep -q false || fail "removed file still in the manifest"

api -X POST "$url/api/manage/v1/collections/redirect/items" -d '{"locale":"en","fields":{"from":"/old-e2e","to":"/colophon"}}' >"$work/redirect.json" || fail "create redirect"
rid=$(js 'j.id' <"$work/redirect.json")
api -X POST "$url/api/manage/v1/items/$rid/publish" -d '{"locales":["en"]}' >/dev/null || fail "publish redirect"
wait_for "the redirect file" grep -q '/en/old-e2e' "$out/en/_redirects"
grep -q '/en/colophon' "$out/en/_redirects" || fail "_redirects target"
grep -q 'url=/en/colophon' "$out/en/en/old-e2e/index.html" || fail "refresh page"
grep -q '/en/old-e2e' "$out/redirects.nginx.conf" || fail "nginx map"
grep -q 'en/old-e2e' "$out/en/s3-routing-rules.json" || fail "s3 rules"

echo "== full export after the changes matches the incremental tree"
find "$out" -type f ! -name manifest.json -print0 | sort -z | xargs -0 shasum >"$work/incremental.sums"
kill "${pids[0]}" 2>/dev/null || true; wait "${pids[0]}" 2>/dev/null || true; pids=()
"$work/fullstack" site:export -full >"$work/export3.log" 2>&1 || { cat "$work/export3.log"; fail "site:export -full"; }
find "$out" -type f ! -name manifest.json -print0 | sort -z | xargs -0 shasum >"$work/rebuilt.sums"
diff "$work/incremental.sums" "$work/rebuilt.sums" >"$work/sums.diff" || { head -20 "$work/sums.diff"; fail "incremental and full exports differ"; }
echo "site-export-e2e ok"
