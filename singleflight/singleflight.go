// Package singleflight shares the result of overlapping calls for the same key.
package singleflight

import (
	"context"
	"errors"
	"sync"
)

// ErrInvalidFunc reports a nil callback.
var ErrInvalidFunc = errors.New("singleflight: function must be non-nil")

// ErrInvalidKey reports a key unequal to itself, such as a floating-point NaN.
var ErrInvalidKey = errors.New("singleflight: key must equal itself")

// ErrGoexit is returned to waiters when the executing callback calls runtime.Goexit.
var ErrGoexit = errors.New("singleflight: callback exited without returning")

// Group suppresses overlapping calls by key. Its zero value is ready for use.
// A Group is safe for concurrent use and must not be copied after first use.
// Results are shared without copying and are not cached after execution ends.
// The number of distinct active keys is not bounded by Group.
type Group[K comparable, V any] struct {
	mu    sync.Mutex
	calls map[K]*call[V]
}

type call[V any] struct {
	done       chan struct{}
	value      V
	err        error
	panicValue any
}

// Do runs fn synchronously in the first caller's goroutine and with that
// caller's context. Overlapping callers for key wait for the same value and
// error; their callbacks are not run. Calls with different keys run independently.
// Callbacks for a key must be interchangeable, including authorization scope.
// A callback must not call Do on the same Group and key before it returns.
//
// A canceled waiter leaves without canceling the shared execution. Canceling
// the first caller cancels its callback context and can affect every waiter;
// Do cannot interrupt a callback that ignores cancellation. Successful execution
// wins over simultaneous cancellation in the first caller. Waiting callers
// return context.Cause(ctx) if cancellation is observed before reading a result.
// Panics propagate in the executing caller and in waiters that read the result.
// Goexit ends the executing goroutine normally and gives waiters ErrGoexit.
// A nil fn returns ErrInvalidFunc without joining or starting execution.
// Non-reflexive keys return ErrInvalidKey. Interface keys containing values
// without equality support panic, as they do when used as Go map keys.
func (g *Group[K, V]) Do(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error) {
	var zero V
	if fn == nil {
		return zero, ErrInvalidFunc
	}
	if err := context.Cause(ctx); err != nil {
		return zero, err
	}
	if key != key { //nolint:gocritic // NaN keys cannot be retrieved or deleted from a map.
		return zero, ErrInvalidKey
	}
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return zero, context.Cause(ctx)
		case <-c.done:
			if err := context.Cause(ctx); err != nil {
				return zero, err
			}
			if c.panicValue != nil {
				panic(c.panicValue)
			}
			return c.value, c.err
		}
	}
	c := &call[V]{done: make(chan struct{})}
	if g.calls == nil {
		g.calls = make(map[K]*call[V])
	}
	g.calls[key] = c
	g.mu.Unlock()
	return g.run(ctx, key, c, fn)
}

func (g *Group[K, V]) run(ctx context.Context, key K, c *call[V], fn func(context.Context) (V, error)) (V, error) {
	hasReturned := false
	defer func() {
		if !hasReturned {
			if p := recover(); p != nil {
				c.panicValue = p
			} else {
				c.err = ErrGoexit
			}
		}
		g.mu.Lock()
		delete(g.calls, key)
		close(c.done)
		g.mu.Unlock()
		if c.panicValue != nil {
			panic(c.panicValue)
		}
	}()
	c.value, c.err = fn(ctx)
	hasReturned = true
	return c.value, c.err
}
