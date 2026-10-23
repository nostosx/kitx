// Package retry runs explicitly selected operations with bounded attempts and
// context-aware waits. Callers are responsible for safe repetition, including
// idempotency and replaying request bodies.
package retry

import (
	"context"
	"errors"
	"time"
)

// Policy controls a call to Do. Its zero value makes a single attempt.
// A policy can be reused concurrently if its callbacks are safe for that use.
type Policy struct {
	// MaxAttempts includes the initial call. Zero means one; negatives are invalid.
	MaxAttempts int
	// RetryIf must explicitly accept an error before another attempt is made.
	// Nil disables retries. Cancellation errors are terminal. Deadline errors are
	// terminal unless this policy's own AttemptTimeout expired.
	RetryIf func(error) bool
	// Backoff receives the number of failed attempts (1 before the first retry).
	// Nil and non-positive delays retry immediately.
	Backoff func(attempt int) time.Duration
	// Timeout bounds the whole call, including attempts and waits. Zero uses
	// only the parent deadline. Negative timeouts are invalid.
	Timeout time.Duration
	// AttemptTimeout bounds each operation. Zero uses the call's context.
	// An expired attempt may be retried only if RetryIf accepts its error and
	// the total context is still live. Operations must observe their context.
	AttemptTimeout time.Duration
	// RetryAfter optionally extracts a minimum wait from an error. The actual
	// wait is max(Backoff(attempt), RetryAfter(err), 0). It runs only after the
	// retry predicate accepts the error; negative results are ignored.
	RetryAfter func(error) time.Duration
}

// ErrInvalidArgument indicates a negative budget/timeout or a nil operation.
var ErrInvalidArgument = errors.New("retry: budgets must be non-negative and operation must be non-nil")

// Do calls fn serially until it succeeds, the attempt budget is exhausted, the
// error is not retryable, or ctx ends. It returns the last value and error;
// cancellation between attempts replaces that error with ctx.Err(). A successful
// operation wins over concurrent cancellation. No attempt starts if ctx is
// already canceled. Do does not interrupt fn: fn must observe its context.
// All callbacks run synchronously. Cancellation is checked after RetryIf returns
// and before Backoff is called. Callback panics propagate to the caller.
func Do[T any](ctx context.Context, p Policy, fn func(context.Context) (T, error)) (T, error) {
	var value T
	if p.MaxAttempts < 0 || p.Timeout < 0 || p.AttemptTimeout < 0 || fn == nil {
		return value, ErrInvalidArgument
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return value, err
		}
		var err error
		var attemptExpired bool
		value, attemptExpired, err = invokeAttempt(ctx, p.AttemptTimeout, fn)
		if err == nil {
			return value, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return value, ctxErr
		}
		if attempt >= max(p.MaxAttempts, 1) || p.RetryIf == nil {
			return value, err
		}
		if errors.Is(err, context.Canceled) || (errors.Is(err, context.DeadlineExceeded) && !attemptExpired) {
			return value, err
		}
		shouldRetry := p.RetryIf(err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return value, ctxErr
		}
		if !shouldRetry {
			return value, err
		}
		var delay time.Duration
		if p.Backoff != nil {
			delay = p.Backoff(attempt)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return value, ctxErr
		}
		if p.RetryAfter != nil {
			delay = max(delay, p.RetryAfter(err))
		}
		if err := wait(ctx, delay); err != nil {
			return value, err
		}
	}
}

// invokeAttempt releases each attempt timer immediately, including on panic or Goexit.
func invokeAttempt[T any](ctx context.Context, timeout time.Duration, fn func(context.Context) (T, error)) (T, bool, error) {
	if timeout == 0 {
		value, err := fn(ctx)
		return value, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	value, err := fn(ctx)
	return value, errors.Is(ctx.Err(), context.DeadlineExceeded), err
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
