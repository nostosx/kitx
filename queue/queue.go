// Package queue provides a bounded, in-memory FIFO with cancellable operations.
package queue

import (
	"context"
	"errors"
	"sync"
)

// ErrInvalidCapacity reports a non-positive capacity passed to New.
var ErrInvalidCapacity = errors.New("queue: capacity must be positive")

// ErrClosed reports a closed or uninitialized queue. Pop drains accepted values
// before returning ErrClosed; Push always rejects a closed queue.
var ErrClosed = errors.New("queue: closed")

// Queue owns a fixed-size ring of values. It is safe for concurrent use and
// must not be copied after first use. The zero value is closed. Values are not
// cloned or disposed of. Capacity bounds buffered values, not waiting callers;
// applications must bound their producers and consumers separately.
type Queue[T any] struct {
	mu      sync.Mutex
	values  []T
	head    int
	size    int
	closed  bool
	changed chan struct{}
}

// New returns an empty queue with a positive capacity. It starts no goroutines.
func New[T any](capacity int) (*Queue[T], error) {
	if capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	return &Queue[T]{values: make([]T, capacity), changed: make(chan struct{})}, nil
}

// Push appends value, waiting while full. Cancellation observed under the queue
// lock takes precedence over admission or closure and returns context.Cause(ctx).
// Success transfers the value into the queue; later cancellation does not undo it.
func (q *Queue[T]) Push(ctx context.Context, value T) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if q.closed || len(q.values) == 0 {
			return ErrClosed
		}
		if q.size < len(q.values) {
			// Avoid overflowing head+size at very large capacities.
			i := q.head - (len(q.values) - q.size)
			if i < 0 {
				i += len(q.values)
			}
			q.values[i] = value
			q.size++
			q.notify()
			return nil
		}
		if err := q.wait(ctx); err != nil {
			return err
		}
	}
}

// Pop removes the oldest value, waiting while empty. After Close it drains the
// buffered values, then returns the zero T and ErrClosed. Cancellation observed
// under the lock takes precedence and leaves buffered values untouched.
func (q *Queue[T]) Pop(ctx context.Context) (T, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var zero T
	for {
		if err := context.Cause(ctx); err != nil {
			return zero, err
		}
		if q.size > 0 {
			value := q.values[q.head]
			q.values[q.head] = zero
			q.head++
			if q.head == len(q.values) {
				q.head = 0
			}
			q.size--
			q.notify()
			return value, nil
		}
		if q.closed || len(q.values) == 0 {
			return zero, ErrClosed
		}
		if err := q.wait(ctx); err != nil {
			return zero, err
		}
	}
}

// Close rejects further pushes and wakes all blocked operations. Buffered values
// remain available to Pop. Close is idempotent and always returns nil.
func (q *Queue[T]) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed && q.changed != nil {
		q.closed = true
		q.notify()
	}
	return nil
}

// Len returns a snapshot of the number of buffered values.
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size
}

func (q *Queue[T]) notify() {
	close(q.changed)
	q.changed = make(chan struct{})
}

// wait returns with mu held on every path.
func (q *Queue[T]) wait(ctx context.Context) error {
	changed := q.changed
	q.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-changed:
	}
	q.mu.Lock()
	return context.Cause(ctx)
}
