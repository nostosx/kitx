// Package circuitbreaker provides a process-local consecutive-failure breaker.
package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State describes admission. Transitions occur lazily during Allow and completion.
type State uint8

const (
	// Closed admits calls and counts consecutive failures. It is the initial state.
	Closed State = iota
	// Open rejects calls until the configured cooldown has elapsed.
	Open
	// HalfOpen admits a bounded batch of recovery probes.
	HalfOpen
)

// ErrOpen reports rejected admission, including an exhausted half-open probe batch.
var ErrOpen = errors.New("circuitbreaker: open")

// ErrInvalidArgument reports invalid configuration, a nil operation or an uninitialized breaker.
var ErrInvalidArgument = errors.New("circuitbreaker: invalid configuration or operation")

// Config controls a breaker. FailureThreshold and OpenTimeout must be positive.
// Consecutive failures are counted in completion order, not admission order.
// HalfOpenLimit is the number of successful probes required to close; zero uses
// one, and negative values are invalid. Failure classification belongs to callers.
type Config struct {
	FailureThreshold int
	OpenTimeout      time.Duration
	HalfOpenLimit    int
}

// Breaker gates calls without creating goroutines or interrupting operations.
// Methods are safe for concurrent use; do not copy it after first use. A zero
// Breaker rejects admission. It retains no errors, keys, or request data.
type Breaker struct {
	mu         sync.Mutex
	config     Config
	state      State
	generation uint64
	failures   int
	probes     int
	successes  int
	retryAt    time.Time
}

// New returns a closed breaker with validated configuration.
func New(config Config) (*Breaker, error) {
	if config.FailureThreshold <= 0 || config.OpenTimeout <= 0 || config.HalfOpenLimit < 0 {
		return nil, ErrInvalidArgument
	}
	config.HalfOpenLimit = max(config.HalfOpenLimit, 1)
	return &Breaker{config: config}, nil
}

// Allow admits an operation and returns its idempotent completion function.
// Call done exactly once with the caller's success classification, including on
// cancellation or panic. In HalfOpen only HalfOpenLimit probes are admitted per
// generation; all must succeed to close. Any failure reopens the breaker.
// Late results from a prior generation are ignored. No user callback runs under
// the lock. A lost completion can keep HalfOpen occupied indefinitely; use Do
// when the operation and classification fit its synchronous callback contract.
func (b *Breaker) Allow() (done func(success bool), err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.config.FailureThreshold <= 0 {
		return nil, ErrInvalidArgument
	}
	if b.state == Open {
		if time.Now().Before(b.retryAt) {
			return nil, ErrOpen
		}
		b.transition(HalfOpen)
	}
	if b.state == HalfOpen {
		if b.probes == b.config.HalfOpenLimit {
			return nil, ErrOpen
		}
		b.probes++
	}
	generation := b.generation
	var once sync.Once
	return func(success bool) { once.Do(func() { b.complete(generation, success) }) }, nil
}

// State returns the current state without advancing time-based transitions.
// An Open breaker may admit a probe on the next Allow after its cooldown.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) complete(generation uint64, success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation != b.generation {
		return
	}
	if b.state == HalfOpen {
		if !success {
			b.open()
			return
		}
		b.successes++
		if b.successes == b.config.HalfOpenLimit {
			b.transition(Closed)
		}
		return
	}
	if success {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures == b.config.FailureThreshold {
		b.open()
	}
}

func (b *Breaker) transition(state State) {
	b.state = state
	b.generation++
	b.failures, b.probes, b.successes = 0, 0, 0
}

func (b *Breaker) open() {
	b.transition(Open)
	b.retryAt = time.Now().Add(b.config.OpenTimeout)
}

// Do calls fn synchronously after admission. Nil is success; other errors count
// as failures unless isFailure explicitly returns false. Nil isFailure counts
// every error as failure, including cancellation. The original value and error
// are returned unchanged. Already-canceled contexts do not request admission.
// Operation/classifier panics and Goexit settle the permit as a failure and keep
// their ordinary control-flow semantics. Do cannot stop an uncooperative fn.
func Do[T any](ctx context.Context, b *Breaker, fn func(context.Context) (T, error), isFailure func(error) bool) (T, error) {
	var zero T
	if b == nil || fn == nil {
		return zero, ErrInvalidArgument
	}
	if err := context.Cause(ctx); err != nil {
		return zero, err
	}
	done, err := b.Allow()
	if err != nil {
		return zero, err
	}
	success := false
	defer func() { done(success) }()
	value, err := fn(ctx)
	success = err == nil || (isFailure != nil && !isFailure(err))
	return value, err
}
