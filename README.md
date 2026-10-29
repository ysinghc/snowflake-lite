# snowflake-lite

Lock-free, time-sortable 64-bit unique IDs for Go. A single `CompareAndSwap` per ID, no mutex, no sleeping, no errors on clock rollback.

Site: https://snowflake.ysinghc.in

```sh
go get github.com/ysinghc/snowflake-lite@latest
```

```go
import "github.com/ysinghc/snowflake-lite/snowflake"

g := snowflake.MustNew(7) // machine ID 0..1023
id := g.Next()            // strictly ascending, safe for concurrent use
fmt.Println(id, id.Time())
```

## Layout

```
| 1 unused | 41-bit timestamp | 10-bit machine | 12-bit sequence |
```

- 41-bit millisecond timestamp from a 2024-01-01 epoch (~69 years of headroom)
- up to 1024 machines, 4096 IDs per millisecond per machine
- IDs are positive `int64`; send them as strings over JSON (they exceed 2^53)

## Releases

Push a semver tag to release:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

CI tests the module, publishes a GitHub release, and warms the Go module proxy. The project site reads the latest release from GitHub, so it updates by itself.

## License

MIT
