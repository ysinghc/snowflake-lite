#!/bin/sh
# Compiles the Go package to WebAssembly so the site playground always runs the current code.
set -e
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o site/public/snowflake.wasm ./wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" site/public/wasm_exec.js
