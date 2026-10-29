//go:build !(js && wasm)

// This package targets WebAssembly. This stub exists only so host toolchains
// (go build ./..., go vet ./...) have something to compile; the real bindings
// live in main.go behind the `js && wasm` build tag.
package main

func main() {
	println("build for the browser with: GOOS=js GOARCH=wasm go build -o site/public/snowflake.wasm ./wasm")
}
