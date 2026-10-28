# kitx

[简体中文](README.zh-CN.md) · [API reference](https://pkg.go.dev/github.com/nostosx/kitx)

Independent Go packages for retries, concurrency, caching, and I/O.
Import the packages you need and compose them with Go functions and contexts.
Code and tests depend only on the standard library.

## Installation

Requires **Go 1.27.3 or later**. Install v1.0.0 from your application module:

```sh
go get github.com/nostosx/kitx@v1.0.0
```

## Usage

Create a cache that holds up to 1,000 entries, each with its own expiration:

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/nostosx/kitx/ttlcache"
)

func main() {
	cache, err := ttlcache.New[string, string](1000)
	if err != nil {
		log.Fatal(err)
	}
	cache.Set("language", "en", time.Minute)
	fmt.Println(cache.Get("language"))
}
```

Save this as `main.go` and run `go run .`:

```text
en true
```

When full, the cache evicts the least recently used entry. Reads do not extend
an entry’s TTL.

Packages compose at the call site. For example, use `retry` with `backoff` to
make up to three read attempts, retrying selected failures with exponential
backoff and full jitter:

```go
package example

import (
	"context"
	"time"

	"github.com/nostosx/kitx/backoff"
	"github.com/nostosx/kitx/retry"
)

func ReadWithRetry(
	ctx context.Context,
	read func(context.Context) ([]byte, error),
	isTransient func(error) bool,
) ([]byte, error) {
	delay := backoff.Exponential(100*time.Millisecond, 2*time.Second)
	return retry.Do(ctx, retry.Policy{
		MaxAttempts: 3,
		RetryIf:     isTransient,
		Backoff:     backoff.FullJitter(delay),
	}, read)
}
```

The caller supplies both the operation and the decision to retry. Set a deadline
on `ctx` to bound the whole operation; `read` must honor cancellation. Only retry
operations that are safe to repeat.

## Packages

Each package can be imported independently as `github.com/nostosx/kitx/<package>`.
All packages share the module version.

| Package | Purpose |
| --- | --- |
| [atomicfile](atomicfile) | Replace files through a temporary file, with path-based and `os.Root` variants. |
| [backoff](backoff) | Calculate capped exponential delays, with optional full jitter. |
| [boundedio](boundedio) | Read or copy data with an explicit byte limit and overflow detection. |
| [circuitbreaker](circuitbreaker) | Reject calls after repeated failures and probe for recovery. |
| [cleanup](cleanup) | Release resources in reverse acquisition order and combine errors. |
| [dataurl](dataurl) | Encode and decode inline data URLs while preserving MIME parameters. |
| [parallel](parallel) | Process slices or iterators with bounded concurrency. |
| [queue](queue) | Coordinate producers and consumers through a bounded FIFO queue. |
| [ratelimit](ratelimit) | Control admission with a token bucket. |
| [retry](retry) | Retry selected errors within attempt and time budgets. |
| [semaphore](semaphore) | Limit concurrent work with weighted permits. |
| [singleflight](singleflight) | Share overlapping calls for the same key. |
| [syncmap](syncmap) | Access a typed map concurrently and take shallow snapshots. |
| [ttlcache](ttlcache) | Cache entries with TTL expiration and LRU eviction. |
| [utf8text](utf8text) | Truncate text within a byte budget, including an optional suffix. |

Package directories contain executable `Example` functions. See the
[API reference](https://pkg.go.dev/github.com/nostosx/kitx) for signatures,
errors, zero values, and concurrency guarantees. In a source checkout, use
`go doc ./retry` to read package documentation locally.

## Usage boundaries

Applications choose deadlines, resource limits, and authorization scopes:

- **Cancellation:** callbacks must observe their context. `singleflight.Group`
  uses the first caller’s context; `SharedGroup` uses an explicit owner context
  and execution timeout. Call `Close` to cancel and join shared work.
- **Shared values:** caches, maps, and shared calls do not copy stored values.
  Synchronize mutations or copy values at the boundary. Include tenant and
  authorization scope in cache and request keys.
- **Limits:** cache capacity counts entries, not bytes; request coalescing does
  not bound distinct keys. `dataurl` does not limit input size. Bound untrusted
  input and concurrent work at the call site.
- **File replacement:** use trusted parent directories or an appropriate
  `os.Root`. Atomicity follows `os.Rename`; it is not guaranteed on Windows.
  The file is synced, but its directory is not. Read-modify-write operations
  need a separate lock. See [atomicfile](atomicfile) for the full contract.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, design criteria, and pull
requests. Run the following checks before submitting a change:

```sh
task check
task lint
task vulncheck
```

The public commands are `check`, `test`, `build`, `lint`, `vulncheck`, `fmt`,
`fix` and `tidy`; `task` defaults to `check`. Use `task test RACE=true` for
race tests or `task test COVER=true` for race tests with function coverage.
`task --list` and `task --list-all` show only these eight entry points.

[Release notes](RELEASE_NOTES.md) · [Changelog](CHANGELOG.md) · [Release guide](RELEASING.md) · [Security policy](SECURITY.md)

## License

[MIT](LICENSE).
