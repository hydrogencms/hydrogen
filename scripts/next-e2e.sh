#!/usr/bin/env bash
# Next.js sample end to end: examples/multisite (SQLite) with seeded content, a webhook through a relay to the
# sample's revalidate route, `next build` + `next start`, then curl assertions on rendering, metadata, redirects,
# 404s, draft preview and revalidation. Run from the repo root: make next-e2e.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.work/next-e2e"
site="$root/examples/next-site"
secret=e2e-secret-0123456789abcdef
manage_token=e2e-token
pids=()

cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
fail() {
  echo "FAIL: $*" >&2
  [ -s "${body:-/nonexistent}" ] && { echo "--- last response (${status:-?})"; head -c 1500 "$body" | sed 's/<script.*//'; echo; } || true
  for f in cms next relay; do [ -s "$work/$f.log" ] && { echo "--- $f.log (tail)"; tail -n 15 "$work/$f.log"; } || true; done
  [ -z "${E2E_KEEP:-}" ] || { echo "E2E_KEEP: servers stay up: cms=$cms next=$next relay=$relay (ctrl-c to stop)" >&2; sleep 3600; }
  exit 1
}
trap cleanup EXIT

port() { node -e "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"; }
wait_http() { # url, seconds
  for _ in $(seq $(($2 * 10))); do curl -fsS -o /dev/null "$1" 2>/dev/null && return 0; sleep 0.1; done
  fail "$1 did not come up"
}

rm -rf "$work" && mkdir -p "$work/cfg" "$work/db"
cms_port=$(port); relay_port=$(port); next_port=$(port); count_port=$(port)
cms=http://127.0.0.1:$cms_port; next=http://127.0.0.1:$next_port; relay=http://127.0.0.1:$relay_port; counter=http://127.0.0.1:$count_port

echo "== build"
(cd "$root/examples/multisite" && go build -o "$work/multisite" .)
(cd "$root/packages" && npm install --no-audit --no-fund >/dev/null && npm run build >/dev/null)
(cd "$site" && rm -rf node_modules/@hydrogencms && npm install --no-audit --no-fund >/dev/null)

echo "== cms on :$cms_port, webhook relay on :$relay_port"
cp "$root"/examples/multisite/config/*.yaml "$work/cfg/"
cat >> "$work/cfg/app.yaml" <<'YAML'

webhooks:
  next:
    url: ${env:NEXT_WEBHOOK_URL}
    events: ["*"]
    secret: ${env:HYDROGEN_REVALIDATE_SECRET}
YAML
node "$site/e2e/relay.mjs" "$relay_port" "$next/api/hydrogen/revalidate" >"$work/relay.log" 2>&1 & pids+=($!)
export CONFIG_DIR="$work/cfg" DATABASE_URL="sqlite:$work/db/h.db" LISTEN_ADDR="127.0.0.1:$cms_port" MANAGE_TOKEN="$manage_token"
export NEXT_WEBHOOK_URL="$relay/hook" HYDROGEN_REVALIDATE_SECRET="$secret"
(cd "$root/examples/multisite" && "$work/multisite" migrate >"$work/cms.log" 2>&1)
(cd "$root/examples/multisite" && exec "$work/multisite" serve >>"$work/cms.log" 2>&1) & pids+=($!)
wait_http "$cms/healthz" 20
wait_http "$relay/queued" 5
node "$site/e2e/counter.mjs" "$count_port" "$cms" >"$work/counter.log" 2>&1 & pids+=($!)
wait_http "$counter/__count" 5

cmsjs() { CMS_URL=$cms MANAGE_TOKEN=$manage_token node "$site/e2e/cms.mjs" "$@"; }
echo "== seed"
ids=$(cmsjs seed)
id() { echo "$ids" | jq -r ".$1"; }
sleep 1; curl -fsS "$relay/clear" >/dev/null

echo "== next build + start on :$next_port"
export HYDROGEN_URL=$counter HYDROGEN_REVALIDATE_SECRET=$secret
(cd "$site" && rm -rf .next && npm run build >"$work/next-build.log" 2>&1) || { tail -n 30 "$work/next-build.log"; fail "next build"; }
[ -f "$site/hydrogen-templates.json" ] || fail "hydrogen-templates.json was not written"
(cd "$site" && exec node_modules/.bin/next start -p "$next_port" >"$work/next.log" 2>&1) & pids+=($!)
wait_http "$next/hydrogen-templates.json" 30

checks=0
# get HOST PATH [curl args]: fills $status, $headers (file) and $body (file); the host picks the channel.
get() {
  local host=$1 path=$2; shift 2
  status=$(curl -sS -o "$work/body" -D "$work/headers" -w '%{http_code}' --resolve "$host:$next_port:127.0.0.1" "$@" "http://$host:$next_port$path") || fail "curl $host$path"
  body=$work/body; headers=$work/headers
}
ok() { checks=$((checks + 1)); }
expect_status() { [ "$status" = "$1" ] || fail "$2: status $status, want $1"; ok; }
expect_body() { grep -qiE -- "$1" "$body" || fail "$2: body lacks /$1/"; ok; }
expect_no_body() { ! grep -qiE -- "$1" "$body" || fail "$2: body has /$1/"; ok; }
expect_header() { grep -qiE -- "^$1" "$headers" || fail "$2: header lacks /$1/"; ok; }
page() { get "$@"; }

echo "== rendering on several channels"
get www.acme.com /en/about
expect_status 200 "en about"
expect_body '<title>About</title>' "en about title"
expect_body '<h1>About</h1>' "en about h1"
expect_body 'data-template="page"' "en about template"
expect_body '<link rel="canonical" href="https://www.acme.com/en/about"' "canonical"
expect_body 'hreflang="de" href="https://www.acme.com/de/uber-uns"' "hreflang de"
expect_body 'hreflang="de-CH" href="https://www.acme.com/de-ch/uber-eus"' "hreflang de-CH"
expect_body '<nav aria-label="Main"><ul>.*<a href="https://www.acme.com/en/about">About</a>' "nav lists the page"
expect_body '<a href="https://www.acme.com/en/about/team">Team</a>' "nav depth 2"

get www.acme.com /de/uber-uns
expect_status 200 "de about"
expect_body '<title>Uber uns</title>' "de title"
expect_body 'lang="de"' "de lang"
expect_body '<a href="https://www.acme.com/de/uber-uns">Uber uns</a>' "de nav is German"

get www.acme.com /de-ch/about/team
expect_status 200 "de-ch fallback"
expect_body '<title>Team</title>' "fallback to en content"

get jobs.acme.com /
expect_status 200 "careers root"
expect_body '<title>Jobs</title>' "careers home"
expect_body '<a href="https://jobs.acme.com/open-roles">Open roles</a>' "careers nav (other site)"
expect_no_body 'acme.com/en/about' "careers nav does not list corporate pages"
get jobs.acme.com /open-roles
expect_body '<title>Open roles</title>' "careers page"

get www.acme.sg /about
expect_status 200 "market channel"
expect_body '<title>About</title>' "market channel title"

get www.acme.com /en/news/hello
expect_status 200 "mounted article"
expect_body 'data-template="article"' "article template"
expect_body '<h1>Hello</h1>' "article h1"
expect_body 'More news' "article list through the cached client"

echo "== not found"
get www.acme.com /en/nothing;       expect_status 404 "unknown path"; expect_body 'Page not found' "404 page"
get www.acme.com /fr/about;         expect_status 404 "unknown prefix"
get other.test /en/about;           expect_status 404 "unknown host"
get jobs.acme.com /about;           expect_status 404 "corporate path on the careers site"

echo "== forged hand-off headers are ignored"
# /en/x.html skips the proxy (dotted path), so the page itself decides; the forged headers name a real page.
get www.acme.com /en/x.html -H 'x-hydrogen-url: https://www.acme.com/en/about' -H 'x-hydrogen-channel: corporate_de' -H 'x-hydrogen-path: /de/uber-uns'
expect_body 'Page not found' "forged x-hydrogen-url (status is 200 here: the proxy did not run)"
expect_no_body '<h1>About</h1>' "forged url did not pick the page"

echo "== CMS calls per page view"
count() { curl -fsS "$counter/__count?prefix=$1" | jq .n; }
get www.acme.com /en/about; get www.acme.com /en/about
before=$(count /)
get www.acme.com /en/about
[ "$(( $(count /) - before ))" = 1 ] || fail "a warm page view made $(( $(count /) - before )) CMS calls, want 1 (the proxy's /route)"; ok
get www.acme.sg /about; get www.acme.sg /about
before=$(count /)
get www.acme.sg /about
[ "$(( $(count /) - before ))" = 1 ] || fail "a warm market page view made $(( $(count /) - before )) CMS calls, want 1"; ok

echo "== template list for check-frontend"
[ "$(jq -c '.templates' "$site/hydrogen-templates.json")" = '["article","landing","page"]' ] || fail "hydrogen-templates.json: $(cat "$site/hydrogen-templates.json")"
ok

echo "== webhook endpoint"
post() { # signature header value, body
  curl -sS -o "$work/body" -w '%{http_code}' -X POST -H "Hydrogen-Signature: $1" -d "$2" "$next/api/hydrogen/revalidate"
}
sign() { # timestamp, body
  printf '%s.%s' "$1" "$2" | openssl dgst -sha256 -hmac "$secret" -hex | sed 's/^.* //'
}
b='{"id":"1","topic":"item.published","data":{},"tags":[],"createdAt":"2026-01-01T00:00:00Z"}'
now=$(date +%s)
[ "$(post "t=$now,v1=$(printf 0%.0s {1..64})" "$b")" = 401 ] || fail "bad signature accepted"; ok
[ "$(post "garbage" "$b")" = 401 ] || fail "malformed signature accepted"; ok
[ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST -d "$b" "$next/api/hydrogen/revalidate")" = 401 ] || fail "unsigned body accepted"; ok
old=$((now - 3600))
[ "$(post "t=$old,v1=$(sign "$old" "$b")" "$b")" = 401 ] || fail "expired signature accepted"; ok
[ "$(post "t=$now,v1=$(sign "$now" "$b")" "$b")" = 200 ] || fail "valid signature rejected"; ok

big=$(head -c 2000000 /dev/zero | curl -sS -o /dev/null -w '%{http_code}' -X POST --data-binary @- "$next/api/hydrogen/revalidate")
[ "$big" = 413 ] || fail "oversized webhook body: $big"; ok
chunked=$(head -c 2000000 /dev/zero | curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Transfer-Encoding: chunked' -H 'Content-Length:' --data-binary @- "$next/api/hydrogen/revalidate")
[ "$chunked" = 413 ] || fail "oversized chunked webhook body: $chunked"; ok

echo "== CMS redirect"
cmsjs rename "$(id contact)" reach-us >/dev/null
get www.acme.com "/en/contact?utm=1"
expect_status 301 "renamed page"
expect_header 'location: .*/en/reach-us\?utm=1' "redirect target keeps the query"
get www.acme.com /en/reach-us
expect_status 200 "new url"
expect_body '<title>Contact</title>' "renamed page title"

echo "== draft preview through the CMS's preview URLs (?preview=<token>)"
# open_preview URL: follows the redirects by hand (draft mode's cookie is Secure, which curl will not send over http)
# and leaves the final response in $status/$body/$headers and the cookies in $cookie.
open_preview() {
  local u=$1 host pathq loc
  host=$(echo "$u" | sed -E 's#^https?://([^/:]+).*#\1#'); pathq=$(echo "$u" | sed -E 's#^https?://[^/]+##')
  get "$host" "$pathq"
  expect_status 307 "preview URL redirects to the preview route"
  loc=$(grep -i '^location' "$headers" | sed 's/^[^:]*: //' | tr -d '\r' | sed -E 's#^https?://[^/]+##')
  case $loc in /api/hydrogen/preview\?*) ok ;; *) fail "preview URL went to $loc" ;; esac
  get "$host" "$loc"
  expect_status 307 "preview route redirects"
  grep -i '^set-cookie: hydrogen-preview=.*HttpOnly' "$headers" >/dev/null || fail "preview cookie is not httpOnly"; ok
  cookie=$(grep -i '^set-cookie' "$headers" | sed 's/^[^:]*: //; s/;.*//' | tr -d '\r' | paste -sd';' -)
  loc=$(grep -i '^location' "$headers" | sed 's/^[^:]*: //' | tr -d '\r' | sed -E 's#^https?://[^/]+##')
  get "$host" "$loc" -H "Cookie: $cookie"
}

draft=$(cmsjs draft "$(id aboutNode)")
did=$(echo "$draft" | jq -r .id); tok=$(echo "$draft" | jq -r .token); purl=$(echo "$draft" | jq -r .url)
case $purl in https://www.acme.com/preview/page/$did\?preview=*) ok ;; *) fail "unexpected preview URL $purl" ;; esac
get www.acme.com "/preview/page/$did"
expect_status 404 "draft without preview"
get www.acme.com "/api/hydrogen/preview?token=$tok&path=//evil.example"
expect_status 400 "open redirect refused"
get www.acme.com /en/nothing -H "Cookie: hydrogen-preview=forged.token"
expect_status 404 "a forged preview cookie does not skip the 404"
open_preview "$purl"
expect_status 200 "never-published page through its preview URL"
expect_body '<title>Secret launch</title>' "draft title"
expect_body '<h1>Secret launch</h1>' "draft h1"
expect_body 'noindex' "draft is noindex"
expect_header 'cache-control: .*no-store' "draft is not cached"

# A published page with an edit that is not published yet: its route URL plus ?preview=.
team=$(id team)
pend=$(cmsjs pending "$team" "Team Draft")
turl=$(echo "$pend" | jq -r .url)
case $turl in https://www.acme.com/en/about/team\?preview=*) ok ;; *) fail "unexpected route preview URL $turl" ;; esac
get www.acme.com /en/about/team
expect_body '<title>Team</title>' "published title without preview"
open_preview "$turl"
expect_status 200 "published page with a pending edit"
expect_body '<title>Team Draft</title>' "draft edit title"
expect_body '<h1>Team Draft</h1>' "draft edit h1"
get www.acme.com /en/about/team
expect_body '<title>Team</title>' "still published for everyone else"
get www.acme.com /en/about/secret-launch
expect_status 404 "draft is not public"
short=$(cmsjs token corporate_en "$did" 1 | jq -r .token)
get www.acme.com "/api/hydrogen/preview?token=$short&path=/preview/page/$did"
cookie2=$(grep -i '^set-cookie' "$headers" | sed 's/^[^:]*: //; s/;.*//' | tr -d '\r' | paste -sd';' -)
sleep 2
get www.acme.com "/preview/page/$did" -H "Cookie: $cookie2"
expect_body 'exit=1' "an expired preview token leads to the preview exit"
expect_no_body 'Secret launch' "expired token shows no draft"

echo "== caching and revalidation through the signed webhook"
count() { curl -fsS "$counter/__count?prefix=$1" | jq .n; }
get www.acme.com /en/about
expect_body '<h1>About</h1>' "primed"
trees=$(count /api/deliver/v1/tree)
get www.acme.com /en/about; get www.acme.com /en/about
[ "$(count /api/deliver/v1/tree)" = "$trees" ] || fail "navigation was fetched again: $trees -> $(count /api/deliver/v1/tree)"; ok
curl -fsS "$relay/clear" >/dev/null
cmsjs retitle "$(id about)" "Our Story" >/dev/null
for _ in $(seq 100); do [ "$(curl -fsS "$relay/queued" | jq .queued)" -gt 0 ] && break; sleep 0.1; done
[ "$(curl -fsS "$relay/queued" | jq .queued)" -gt 0 ] || fail "the CMS delivered no webhook"; ok
get www.acme.com /en/about
expect_body '<h1>About</h1>' "still cached while the webhook is held"
expect_no_body 'Our Story' "page served from cache before the webhook"
released=$(curl -fsS "$relay/release")
[ "$(echo "$released" | jq '[.statuses[] | select(. != 200)] | length')" = 0 ] || fail "revalidate route answered $released"; ok
get www.acme.com /en/about
expect_body '<title>Our Story</title>' "title after the webhook"
expect_body '<h1>Our Story</h1>' "h1 after the webhook"
expect_body '>Our Story</a>' "navigation after the webhook"
get www.acme.com /de/uber-uns
expect_body '<title>Uber uns</title>' "untouched page still renders"

echo "next-e2e ok ($checks checks)"
[ -z "${E2E_KEEP:-}" ] || { echo "E2E_KEEP: servers stay up: cms=$cms next=$next counter=$counter (ctrl-c to stop)"; sleep 3600; }
