// Package semaphore provides cancellable, FIFO admission to a weighted budget.
package semaphore

import (
	"container/list"
	"context"
	"errors"
	"sync"
)

// ErrInvalidCapacity reports a non-positive capacity, including an uninitialized Semaphore.
var ErrInvalidCapacity = errors.New("semaphore: capacity must be positive")

// ErrInvalidWeight reports a weight outside [1, capacity].
var ErrInvalidWeight = errors.New("semaphore: weight must be between one and capacity")

// Semaphore limits the total weight of admitted work. Create it with New;
// Acquire on its zero value returns ErrInvalidCapacity and TryAcquire fails.
// Methods and returned release functions are safe for concurrent use.
// A Semaphore must not be copied after first use. It starts no goroutines.
type Semaphore struct {
	mu       sync.Mutex
	capacity int
	used     int
	waiters  list.List
}

type waiter struct {
	weight  int
	ready   chan struct{}
	granted bool
}

// New returns a Semaphore with the given capacity, or ErrInvalidCapacity.
func New(capacity int) (*Semaphore, error) {
	if capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	return &Semaphore{capacity: capacity}, nil
}

// Acquire waits for weight units and returns an idempotent function that releases
// them. The caller must release every successful acquisition. Cancellation before
// admission returns context.Cause(ctx) and consumes no units. A cancellation
// racing with admission may either succeed or return the cause; success always
// transfers release ownership to the caller. Invalid weights return ErrInvalidWeight.
//
// Queued callers are admitted in FIFO order; a large request can block smaller
// ones behind it. Canceling a queued request removes it promptly. Callbacks or
// work associated with the acquired units remain entirely caller-owned.
func (s *Semaphore) Acquire(ctx context.Context, weight int) (release func(), err error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.capacity <= 0 {
		s.mu.Unlock()
		return nil, ErrInvalidCapacity
	}
	if weight <= 0 || weight > s.capacity {
		s.mu.Unlock()
		return nil, ErrInvalidWeight
	}
	if s.waiters.Len() == 0 && weight <= s.capacity-s.used {
		s.used += weight
		s.mu.Unlock()
		return s.release(weight), nil
	}
	w := &waiter{weight: weight, ready: make(chan struct{})}
	e := s.waiters.PushBack(w)
	s.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	}
	s.mu.Lock()
	if err := context.Cause(ctx); err != nil {
		if w.granted {
			s.used -= weight
		} else {
			s.waiters.Remove(e)
		}
		s.notify()
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	return s.release(weight), nil
}

// TryAcquire immediately acquires weight units when no earlier waiter is queued
// and enough capacity is available. On failure it returns nil, false; this
// includes invalid weights and an uninitialized Semaphore. Successful callers
// own the returned idempotent release function, as with Acquire.
func (s *Semaphore) TryAcquire(weight int) (release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if weight <= 0 || weight > s.capacity-s.used || s.waiters.Len() != 0 {
		return nil, false
	}
	s.used += weight
	return s.release(weight), true
}

func (s *Semaphore) release(weight int) func() {
	return sync.OnceFunc(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.used -= weight
		s.notify()
	})
}

// notify admits waiters while holding s.mu.
func (s *Semaphore) notify() {
	for e := s.waiters.Front(); e != nil; e = s.waiters.Front() {
		w := e.Value.(*waiter)
		if w.weight > s.capacity-s.used {
			return
		}
		s.waiters.Remove(e)
		s.used += w.weight
		w.granted = true
		close(w.ready)
	}
}
