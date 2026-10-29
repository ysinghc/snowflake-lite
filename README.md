# snowflake-lite

[![Go Reference](https://pkg.go.dev/badge/github.com/ysinghc/snowflake-lite/snowflake.svg)](https://pkg.go.dev/github.com/ysinghc/snowflake-lite/snowflake)
[![CI](https://github.com/ysinghc/snowflake-lite/actions/workflows/ci.yml/badge.svg)](https://github.com/ysinghc/snowflake-lite/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A small Go library that generates 64-bit, time-sortable unique IDs without any central coordination. It is a lock-free take on Twitter's Snowflake: no mutex, no sleeping, and no error return, even when the system clock moves backward.

**Documentation, live demo and code map:** https://snowflake.ysinghc.in

## Install

```sh
go get github.com/ysinghc/snowflake-lite@latest
```

Requires Go 1.23 or newer. There are no dependencies outside the standard library.

## Quick start

```go
package main

import (
	"fmt"

	"github.com/ysinghc/snowflake-lite/snowflake"
)

func main() {
	g := snowflake.MustNew(7) // machine ID, 0..1023

	id := g.Next()
	fmt.Println(id)        // 374208188427300864
	fmt.Println(id.Time()) // when it was minted (UTC)

	p := id.Decode()
	fmt.Println(p.MachineID, p.Sequence)
}
```

`Generator` is safe for concurrent use. Create one per process and share it.

## API

| Call | Purpose |
|---|---|
| `New(machineID int64) (*Generator, error)` | Create a generator. Errors if the ID is outside 0..1023. |
| `MustNew(machineID int64) *Generator` | Same, but panics on an invalid ID. |
| `NewFromEnv() (*Generator, error)` | Read `MACHINE_ID`, or fall back to a hash of the hostname. |
| `(*Generator).Next() ID` | Next unique, strictly ascending ID. Never blocks, never fails. |
| `(*Generator).NextN(n int) []ID` | Convenience for `n` IDs. |
| `(*Generator).MachineID() int64` | The machine ID embedded in this generator's IDs. |
| `ID.Int64()`, `ID.String()`, `ID.Time()`, `ID.Decode()` | Convert and inspect an ID. |
| `Decode(id ID) Parts` | Split an ID into timestamp, machine and sequence. |

Full reference: https://pkg.go.dev/github.com/ysinghc/snowflake-lite/snowflake

## ID layout

```
| 1 unused | 41-bit timestamp | 10-bit machine | 12-bit sequence |
   bit 63     bits 62..22        bits 21..12       bits 11..0
```

- **Timestamp:** milliseconds since 2024-01-01 UTC. 41 bits last about 69 years, until roughly 2093.
- **Machine:** 0..1023, so up to 1024 independent generators.
- **Sequence:** 0..4095, so up to 4096 IDs per millisecond per machine.
- The sign bit is unused, so every ID is a positive `int64` and maps to a SQL `BIGINT`.
- IDs sort by creation time, which keeps database indexes append-friendly.

## Assigning machine IDs

Uniqueness across processes depends on every running generator having a different machine ID.

- Set `MACHINE_ID` explicitly (0..1023) per process or pod and use `NewFromEnv`. In Kubernetes, a StatefulSet ordinal works well.
- The hostname fallback in `NewFromEnv` is a convenience for development. Two hosts can hash to the same 10-bit value, so do not rely on it for a real distributed deployment.

## Using IDs in JSON and JavaScript

IDs are larger than 2^53, so a JSON number or JavaScript `number` silently loses low bits. Send them as strings:

```go
json.Marshal(map[string]string{"id": id.String()})
```

## Behavior and guarantees

- **Unique and ordered per generator:** IDs from one `Generator` are strictly ascending, including under concurrent use.
- **Clock moves backward:** the generator never regresses. It keeps its last timestamp and advances the sequence, so IDs stay unique and ascending.
- **Sequence overflow:** if more than 4096 IDs are requested in one millisecond, an internal logical clock moves forward by 1 ms instead of blocking. IDs may briefly carry a timestamp slightly ahead of the wall clock.
- **Across processes:** IDs are unique as long as machine IDs are unique. Ordering between machines is only as accurate as their clocks.
- **Restarts:** generator state is in memory. After a restart the generator relies on the wall clock moving forward. A restart combined with a large backward clock step could reuse timestamps for the same machine ID.

## Performance

One `Next()` call is a single atomic compare-and-swap with no allocations. On an AMD Ryzen 7 7735HS a single goroutine measures about 48 ns per ID (`go test -bench . ./snowflake`).

Under heavy contention from many goroutines on one generator, a compare-and-swap loop retries, and a plain mutex can be faster. The benchmarks in [snowflake_test.go](snowflake/snowflake_test.go) compare both on your hardware. If you need maximum throughput on many cores, use one generator per goroutine group with distinct machine IDs.

## Development

```sh
go vet ./...
go test -race ./...
```

## Releases

Push a semver tag to release:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

CI tests the module, publishes a GitHub release, and warms the Go module proxy. The project site reads the latest release from GitHub and updates itself.

## License

[MIT](LICENSE)
