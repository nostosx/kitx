// Package parallel provides bounded, structured concurrency for slices and iterators.
package parallel

import (
	"context"
	"errors"
	"sync"
)

// ErrInvalidArgument indicates a non-positive limit or a nil source, sequence or callback.
var ErrInvalidArgument = errors.New("parallel: invalid limit, source, sequence or callback")

// ErrGoexit indicates that a callback ended its worker with runtime.Goexit.
var ErrGoexit = errors.New("parallel: callback exited without returning")

// Map applies fn to each input using at most limit workers, preserving input
// order in the returned slice. Empty input produces a non-nil empty result.
// The first error cancels sibling work; Map waits for all workers and returns
// nil and the cancellation cause. Concurrent failures have no defined order.
//
// Callbacks must observe their context for prompt cancellation. Work already
// claimed by a worker may start concurrently with cancellation. All callbacks
// have finished and the child context is canceled when Map returns, so no worker
// can access inputs or results afterward. Goroutines spawned by callbacks remain
// caller-owned. Inputs and referenced objects must not be mutated concurrently
// unless the caller provides synchronization. Results are not deep-copied.
// As with a plain goroutine, a callback panic terminates the program. A callback
// that calls runtime.Goexit cancels the remaining work with ErrGoexit.
func Map[T, R any](ctx context.Context, inputs []T, limit int, fn func(context.Context, T) (R, error)) ([]R, error) {
	if limit <= 0 || fn == nil {
		return nil, ErrInvalidArgument
	}
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	results := make([]R, len(inputs))
	var mu sync.Mutex
	next := 0
	var workers sync.WaitGroup
	for range min(limit, len(inputs)) {
		workers.Go(func() {
			for {
				mu.Lock()
				if ctx.Err() != nil || next == len(inputs) {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()
				value, err := invoke(ctx, inputs[i], fn, cancel)
				if err != nil {
					cancel(err)
					return
				}
				results[i] = value
			}
		})
	}
	workers.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return results, nil
}

// invoke also settles work whose callback exits its goroutine without returning.
func invoke[T, R any](ctx context.Context, input T, fn func(context.Context, T) (R, error), cancel context.CancelCauseFunc) (R, error) {
	hasReturned := false
	defer func() {
		if !hasReturned {
			cancel(ErrGoexit)
		}
	}()
	value, err := fn(ctx, input)
	hasReturned = true
	return value, err
}
