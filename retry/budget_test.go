package retry_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/retry"
)

func TestAttemptTimeoutCanRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		var previous context.Context
		value, err := retry.Do(t.Context(), retry.Policy{MaxAttempts: 3, AttemptTimeout: time.Second, RetryIf: always}, func(ctx context.Context) (int, error) {
			if previous != nil && previous.Err() == nil {
				t.Error("previous attempt context still live")
			}
			previous = ctx
			calls++
			if calls == 3 {
				return 42, nil
			}
			<-ctx.Done()
			return 0, ctx.Err()
		})
		if err != nil || value != 42 || calls != 3 || time.Since(start) != 2*time.Second {
			t.Fatalf("value=%d error=%v calls=%d elapsed=%v", value, err, calls, time.Since(start))
		}
		if previous.Err() == nil {
			t.Fatal("successful attempt context not released")
		}
	})
}

func TestTotalBudgetIncludesWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		value, err := retry.Do(t.Context(), retry.Policy{MaxAttempts: 10, Timeout: 3 * time.Second, AttemptTimeout: time.Second, RetryIf: always, Backoff: func(int) time.Duration { return time.Hour }}, func(ctx context.Context) (int, error) { calls++; <-ctx.Done(); return 7, ctx.Err() })
		if !errors.Is(err, context.DeadlineExceeded) || value != 7 || calls != 1 || time.Since(start) != 3*time.Second {
			t.Fatalf("value=%d error=%v calls=%d elapsed=%v", value, err, calls, time.Since(start))
		}
	})
}

func TestRetryAfterMinimum(t *testing.T) {
	for _, tt := range []struct {
		name                string
		backoff, hint, want time.Duration
	}{
		{"hint dominates", time.Second, 3 * time.Second, 3 * time.Second},
		{"backoff dominates", 3 * time.Second, time.Second, 3 * time.Second},
		{"negative ignored", -1, -1, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				calls, hints := 0, 0
				failure := errors.New("busy")
				_, err := retry.Do(t.Context(), retry.Policy{MaxAttempts: 2, RetryIf: always, Backoff: func(int) time.Duration { return tt.backoff }, RetryAfter: func(err error) time.Duration {
					hints++
					if !errors.Is(err, failure) {
						t.Error("hint received wrong error")
					}
					return tt.hint
				}}, func(context.Context) (int, error) {
					calls++
					if calls == 1 {
						return 0, failure
					}
					return 1, nil
				})
				if err != nil || hints != 1 || time.Since(start) != tt.want {
					t.Fatalf("error=%v hints=%d elapsed=%v", err, hints, time.Since(start))
				}
			})
		})
	}
}

func TestRetryAfterSkippedOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := retry.Do(ctx, retry.Policy{MaxAttempts: 2, RetryIf: always, Backoff: func(int) time.Duration { cancel(); return 0 }, RetryAfter: func(error) time.Duration { t.Error("hint called after cancellation"); return 0 }}, func(context.Context) (int, error) { return 0, errors.New("busy") })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestInvalidTimeoutBudgets(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		policy retry.Policy
	}{
		{"total", retry.Policy{Timeout: -1}},
		{"attempt", retry.Policy{AttemptTimeout: -1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := retry.Do(t.Context(), tt.policy, func(context.Context) (int, error) {
				t.Error("invalid policy executed")
				return 0, nil
			})
			if !errors.Is(err, retry.ErrInvalidArgument) {
				t.Fatal(err)
			}
		})
	}
}

func TestAttemptTimeoutDoesNotRetryUnrelatedDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		_, err := retry.Do(t.Context(), retry.Policy{
			MaxAttempts: 3, AttemptTimeout: time.Second,
			RetryIf: func(error) bool { t.Error("unrelated deadline reached predicate"); return true },
		}, func(context.Context) (int, error) {
			calls++
			return 0, context.DeadlineExceeded
		})
		if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) != 0 {
			t.Fatalf("error = %v, calls = %d, elapsed = %v", err, calls, time.Since(start))
		}
	})
}
