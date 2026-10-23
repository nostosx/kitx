package singleflight

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrClosed reports a closed or uninitialized SharedGroup.
var ErrClosed = errors.New("singleflight: shared group closed")

// ErrInvalidTimeout reports a non-positive execution timeout.
var ErrInvalidTimeout = errors.New("singleflight: execution timeout must be positive")

// SharedGroup owns shared execution independently of waiting request contexts.
// Each active key uses one owned goroutine and a timeout. NewShared establishes
// its lifetime; Close cancels and joins its work. All methods are safe for
// concurrent use; do not copy it. The zero value is closed.
// Distinct keys are unbounded: applications must provide admission control.
type SharedGroup[K comparable, V any] struct {
	mu      sync.Mutex
	owner   context.Context
	cancel  context.CancelCauseFunc
	timeout time.Duration
	closed  bool
	calls   map[K]*call[V]
	workers sync.WaitGroup
}

// NewShared creates a group owned by ctx, with a positive per-execution timeout.
// Execution inherits values and cancellation from ctx, never from a waiter.
// Call Close even if ctx is canceled, to join callbacks and release ownership.
func NewShared[K comparable, V any](ctx context.Context, timeout time.Duration) (*SharedGroup[K, V], error) {
	if timeout <= 0 {
		return nil, ErrInvalidTimeout
	}
	owner, cancel := context.WithCancelCause(ctx)
	return &SharedGroup[K, V]{owner: owner, cancel: cancel, timeout: timeout, calls: make(map[K]*call[V])}, nil
}

// Do joins or starts shared execution for key. Canceling any waiter, including
// the initiating caller, affects only that wait. Work continues until completion,
// its execution timeout, owner cancellation or Close. Callbacks must honor their
// execution context; neither timeout nor Close can forcibly interrupt them.
// Results are shared without copying and are not cached. Authorization scope
// must be represented in keys and the owner's context. Same-key reentrancy and
// calling Close from a callback deadlock and are prohibited.
//
// A callback panic terminates the process like an ordinary goroutine panic;
// Goexit settles waiters with ErrGoexit. Canceled waiters return their cause
// before reading a result. Key and nil-callback rules match Group.Do.
func (g *SharedGroup[K, V]) Do(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error) {
	var zero V
	if fn == nil {
		return zero, ErrInvalidFunc
	}
	if err := context.Cause(ctx); err != nil {
		return zero, err
	}
	if key != key { //nolint:gocritic // Map keys must be reflexive.
		return zero, ErrInvalidKey
	}
	g.mu.Lock()
	if g.closed || g.owner == nil {
		g.mu.Unlock()
		return zero, ErrClosed
	}
	if err := context.Cause(g.owner); err != nil {
		g.mu.Unlock()
		return zero, err
	}
	c, ok := g.calls[key]
	if !ok {
		c = &call[V]{done: make(chan struct{})}
		g.calls[key] = c
		g.workers.Go(func() { g.runShared(key, c, fn) })
	}
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		return zero, context.Cause(ctx)
	case <-c.done:
		if err := context.Cause(ctx); err != nil {
			return zero, err
		}
		return c.value, c.err
	}
}

func (g *SharedGroup[K, V]) runShared(key K, c *call[V], fn func(context.Context) (V, error)) {
	ctx, cancel := context.WithTimeout(g.owner, g.timeout)
	defer cancel()
	hasReturned := false
	defer func() {
		if !hasReturned {
			c.err = ErrGoexit
		}
		g.mu.Lock()
		delete(g.calls, key)
		close(c.done)
		g.mu.Unlock()
	}()
	c.value, c.err = fn(ctx)
	hasReturned = true
}

// Close stops admission, cancels all executions with ErrClosed and waits for
// every callback to return. It is idempotent; concurrent Close calls all join
// the same work. Callbacks that ignore cancellation can prevent it from returning.
func (g *SharedGroup[K, V]) Close() error {
	g.mu.Lock()
	g.closed = true
	if g.cancel != nil {
		g.cancel(ErrClosed)
	}
	g.mu.Unlock()
	g.workers.Wait()
	return nil
}
