//go:build js && wasm

// Command wasm exposes the Snowflake generator to JavaScript. The *same*
// snowflake package that powers every Go caller is compiled to WebAssembly here,
// so the browser demo runs the real algorithm — no server, no reimplementation.
//
// Build:
//
//	GOOS=js GOARCH=wasm go build -o site/public/snowflake.wasm ./wasm
package main

import (
	"fmt"
	"strconv"
	"syscall/js"
	"time"

	"github.com/ysinghc/snowflake-lite/snowflake"
)

var gen = snowflake.MustNew(1)

func main() {
	js.Global().Set("snowNew", js.FuncOf(snowNew))
	js.Global().Set("snowNext", js.FuncOf(snowNext))
	js.Global().Set("snowNextBatch", js.FuncOf(snowNextBatch))
	js.Global().Set("snowBench", js.FuncOf(snowBench))
	js.Global().Set("snowDecode", js.FuncOf(snowDecode))

	// Let the page know the runtime is up and the functions are bound.
	if cb := js.Global().Get("onSnowReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {} // block forever so the exported functions stay callable
}

// snowNew re-seeds the generator with a machine ID (0-1023).
func snowNew(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsErr("machine ID required")
	}
	m := int64(args[0].Int())
	g, err := snowflake.New(m)
	if err != nil {
		return jsErr(err.Error())
	}
	gen = g
	return obj("machineID", float64(m))
}

// snowNext returns one ID as a STRING — a Snowflake exceeds 2^53 and a JS
// number (float64) would silently drop its low bits.
func snowNext(_ js.Value, _ []js.Value) any {
	return js.ValueOf(gen.Next().String())
}

// snowNextBatch returns up to 200k IDs as an array of strings.
func snowNextBatch(_ js.Value, args []js.Value) any {
	n := 0
	if len(args) > 0 {
		n = args[0].Int()
	}
	switch {
	case n < 0:
		n = 0
	case n > 200_000:
		n = 200_000
	}
	out := make([]any, n)
	for i := range n {
		out[i] = gen.Next().String()
	}
	return js.ValueOf(out)
}

// snowBench mints n IDs (default 1,000,000) and reports the measured rate,
// running the real generator loop inside the browser.
func snowBench(_ js.Value, args []js.Value) any {
	n := 1_000_000
	if len(args) > 0 && args[0].Int() > 0 {
		n = args[0].Int()
	}
	prev := snowflake.ID(-1)
	monotonic := true
	var last snowflake.ID
	start := time.Now()
	for range n {
		id := gen.Next()
		if id <= prev {
			monotonic = false
		}
		prev, last = id, id
	}
	elapsed := time.Since(start)
	elapsedMs := float64(elapsed.Microseconds()) / 1000.0
	rate := float64(n) / elapsed.Seconds()
	return js.ValueOf(map[string]any{
		"count":     float64(n),
		"elapsedMs": elapsedMs,
		"rate":      rate,
		"monotonic": monotonic,
		"lastID":    last.String(),
	})
}

// snowDecode splits an ID (given as a string) into its fields plus the raw
// 64-bit pattern for the visualizer.
func snowDecode(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsErr("id required")
	}
	raw, err := strconv.ParseInt(args[0].String(), 10, 64)
	if err != nil {
		return jsErr("invalid id")
	}
	p := snowflake.Decode(snowflake.ID(raw))
	return js.ValueOf(map[string]any{
		"id":               strconv.FormatInt(raw, 10),
		"timestamp":        p.Timestamp.Format(time.RFC3339Nano),
		"millisSinceEpoch": float64(p.MillisSinceEpoch),
		"machineID":        float64(p.MachineID),
		"sequence":         float64(p.Sequence),
		"bits":             fmt.Sprintf("%064b", uint64(raw)),
	})
}

func obj(k string, v any) any { return js.ValueOf(map[string]any{k: v}) }

func jsErr(msg string) any { return js.ValueOf(map[string]any{"error": msg}) }
