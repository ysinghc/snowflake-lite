#!/bin/sh
# Builds everything the site serves that is derived from the Go source:
# the WebAssembly playground, the code map, and the HTML pages.
set -e
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o site/public/snowflake.wasm ./wasm
go run ./site/tools/funcmap > site/public/funcmap.json
root="$(go env GOROOT)"
# wasm_exec.js moved from misc/wasm to lib/wasm in Go 1.24.
for f in "$root/lib/wasm/wasm_exec.js" "$root/misc/wasm/wasm_exec.js"; do
  if [ -f "$f" ]; then cp "$f" site/public/wasm_exec.js; break; fi
done
[ -f site/public/wasm_exec.js ] || { echo "wasm_exec.js not found under $root" >&2; exit 1; }
node site/build-pages.mjs
