#!/bin/sh
# Compiles the Go package to WebAssembly so the site playground always runs the current code.
set -e
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o site/public/snowflake.wasm ./wasm
root="$(go env GOROOT)"
# wasm_exec.js moved from misc/wasm to lib/wasm in Go 1.24.
for f in "$root/lib/wasm/wasm_exec.js" "$root/misc/wasm/wasm_exec.js"; do
  [ -f "$f" ] && cp "$f" site/public/wasm_exec.js && exit 0
done
echo "wasm_exec.js not found under $root" >&2
exit 1
