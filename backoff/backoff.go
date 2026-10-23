// Package backoff provides stateless, bounded retry delay functions.
package backoff

import (
	"math/rand/v2"
	"time"
)

// Exponential returns a delay function that doubles initial after each failed
// attempt, up to limit. Attempts are one-based; values below 1 use the first
// delay. Non-positive initial or limit disables the delay. Growth saturates
// without overflowing, even for the largest duration or attempt number.
// The returned function is safe for concurrent use.
func Exponential(initial, limit time.Duration) func(attempt int) time.Duration {
	return func(attempt int) time.Duration {
		if initial <= 0 || limit <= 0 {
			return 0
		}
		d := min(initial, limit)
		for i := 1; i < attempt && d < limit; i++ {
			if d > limit/2 {
				return limit
			}
			d *= 2
		}
		return d
	}
}

// FullJitter wraps delay with a uniform random delay in [0, delay(attempt)).
// Nil delay and non-positive results return zero. The wrapper is safe for
// concurrent use if delay is. It uses math/rand/v2, not cryptographic randomness.
func FullJitter(delay func(attempt int) time.Duration) func(attempt int) time.Duration {
	return func(attempt int) time.Duration {
		if delay == nil {
			return 0
		}
		d := delay(attempt)
		if d <= 0 {
			return 0
		}
		return rand.N(d)
	}
}
