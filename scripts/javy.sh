#!/usr/bin/env bash
# Prints the path of the pinned Javy binary (compiles JavaScript to a WebAssembly hook), downloading the official
# release for this machine into .work/tools on first use. The download is checked against the sha256 pinned below
# before it is unpacked or run. Used by `make wasm-e2e`; users can install Javy any way they like and set JAVY.
set -euo pipefail

version=v9.1.0
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) asset=javy-arm-macos-$version.gz sha=99e9ec6a8e8c98e119d137c08a921d2443d3b873c675a5571e1800f4451e6294 ;;
  Darwin-x86_64) asset=javy-x86_64-macos-$version.gz sha=6eed2927575dc2b3fb5a1563eee1ce0874e6da91c9cec39a728c9377a8cc7b5a ;;
  Linux-x86_64) asset=javy-x86_64-linux-$version.gz sha=a68b122d48eb3dfc1b801d4e14c39271fde3638243d3272d206e376ac9189e39 ;;
  Linux-aarch64 | Linux-arm64) asset=javy-arm-linux-$version.gz sha=826962f0e82354cf97d6e928ea36777872bbae76c626e326948e0367c4b55cb5 ;;
  *) echo "javy.sh: no pinned Javy release for $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

root=$(cd "$(dirname "$0")/.." && pwd)
bin="$root/.work/tools/javy-$version"
if [ -x "$bin" ]; then echo "$bin"; exit 0; fi

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

mkdir -p "$root/.work/tools"
tmp=$(mktemp -d "$root/.work/tools/dl.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$asset" "https://github.com/bytecodealliance/javy/releases/download/$version/$asset" >&2
got=$(sha256 "$tmp/$asset")
if [ "$got" != "$sha" ]; then
  echo "javy.sh: checksum mismatch for $asset: got $got, want $sha" >&2
  exit 1
fi
gunzip -c "$tmp/$asset" >"$tmp/javy"
chmod +x "$tmp/javy"
mv "$tmp/javy" "$bin"
echo "$bin"
