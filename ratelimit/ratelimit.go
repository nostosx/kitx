// Package ratelimit provides a process-local token bucket without background workers.
package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrInvalidArgument reports a non-positive interval or burst, or an uninitialized limiter.
var ErrInvalidArgument = errors.New("ratelimit: interval and burst must be positive")

// Limiter admits one operation per token. New starts with a full burst and
// replenishes one token each interval, up to burst. Unused refill ticks do not
// accumulate beyond burst. Methods are safe for concurrent use; do not copy a
// Limiter after first use. Its zero value rejects all operations.
// Waiting callers are not queued fairly and must be bounded by the application.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	burst    int
	tokens   int
	last     time.Time
}

// New returns a limiter with a positive refill interval and burst capacity.
func New(interval time.Duration, burst int) (*Limiter, error) {
	if interval <= 0 || burst <= 0 {
		return nil, ErrInvalidArgument
	}
	return &Limiter{interval: interval, burst: burst, tokens: burst, last: time.Now()}, nil
}

// Allow consumes a token immediately if one is available. It never waits.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interval <= 0 {
		return false
	}
	return l.take(time.Now()) == 0
}

// Wait waits for and consumes one token, or returns context.Cause(ctx).
// Cancellation observed before admission consumes no token. Cancellation after
// admission does not refund it. Wait creates no goroutine and makes no future
// reservation; canceled callers cannot leave token debt behind.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		if err := context.Cause(ctx); err != nil {
			l.mu.Unlock()
			return err
		}
		if l.interval <= 0 {
			l.mu.Unlock()
			return ErrInvalidArgument
		}
		delay := l.take(time.Now())
		l.mu.Unlock()
		if delay == 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
}

// take runs under mu. Refill arithmetic never multiplies the unbounded tick count.
func (l *Limiter) take(now time.Time) time.Duration {
	elapsed := now.Sub(l.last)
	if ticks := elapsed / l.interval; ticks > 0 {
		room := l.burst - l.tokens
		if ticks >= time.Duration(room) {
			l.tokens = l.burst
		} else {
			l.tokens += int(ticks)
		}
		l.last = now.Add(-(elapsed % l.interval))
	}
	if l.tokens > 0 {
		l.tokens--
		return 0
	}
	return l.interval - now.Sub(l.last)
}
