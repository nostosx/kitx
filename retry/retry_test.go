package retry_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/backoff"
	"github.com/nostosx/kitx/retry"
)

func TestDo(t *testing.T) {
	t.Parallel()
	failure := errors.New("temporary failure")
	for _, tt := range []struct {
		name                string
		policy              retry.Policy
		failures, wantCalls int
		wantErr             error
	}{
		{"success", retry.Policy{}, 0, 1, nil},
		{"zero policy", retry.Policy{}, 5, 1, failure},
		{"explicit predicate required", retry.Policy{MaxAttempts: 3}, 5, 1, failure},
		{"predicate rejects", retry.Policy{MaxAttempts: 3, RetryIf: func(error) bool { return false }}, 5, 1, failure},
		{"eventual success", retry.Policy{MaxAttempts: 3, RetryIf: always}, 2, 3, nil},
		{"exhausted", retry.Policy{MaxAttempts: 3, RetryIf: always}, 5, 3, failure},
		{"one attempt", retry.Policy{MaxAttempts: 1, RetryIf: always}, 5, 1, failure},
		{"invalid policy", retry.Policy{MaxAttempts: -1}, 5, 0, retry.ErrInvalidArgument},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			got, err := retry.Do(t.Context(), tt.policy, func(context.Context) (int, error) {
				calls++
				if calls <= tt.failures {
					return calls, failure
				}
				return calls, nil
			})
			if calls != tt.wantCalls || got != tt.wantCalls || !errors.Is(err, tt.wantErr) {
				t.Fatalf("value, error, calls = %d, %v, %d; want %d, %v, %d", got, err, calls, tt.wantCalls, tt.wantErr, tt.wantCalls)
			}
		})
	}
	if _, err := retry.Do[int](t.Context(), retry.Policy{}, nil); !errors.Is(err, retry.ErrInvalidArgument) {
		t.Fatalf("nil operation: %v", err)
	}
}

func TestDoBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var attempts []int
		calls := 0
		_, err := retry.Do(t.Context(), retry.Policy{
			MaxAttempts: 4,
			RetryIf:     always,
			Backoff: func(attempt int) time.Duration {
				attempts = append(attempts, attempt)
				return backoff.Exponential(time.Second, time.Minute)(attempt)
			},
		}, func(context.Context) (int, error) {
			calls++
			if calls < 4 {
				return calls, errors.New("temporary")
			}
			return calls, nil
		})
		if err != nil || calls != 4 || !reflect.DeepEqual(attempts, []int{1, 2, 3}) || time.Since(start) != 7*time.Second {
			t.Fatalf("error=%v calls=%d backoffs=%v elapsed=%v", err, calls, attempts, time.Since(start))
		}
	})
}

func TestDoCancellation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		delay time.Duration
	}{
		{"during wait", time.Hour},
		{"immediate retry", 0},
		{"negative delay", -time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				start := time.Now()
				_, err := retry.Do(ctx, retry.Policy{
					MaxAttempts: 5, RetryIf: always,
					Backoff: func(int) time.Duration {
						if tt.delay > 0 {
							go func() { time.Sleep(time.Second); cancel() }()
						} else {
							cancel()
						}
						return tt.delay
					},
				}, func(context.Context) (int, error) { calls++; return calls, errors.New("temporary") })
				if !errors.Is(err, context.Canceled) || calls != 1 || time.Since(start) > time.Second {
					t.Fatalf("error=%v calls=%d elapsed=%v", err, calls, time.Since(start))
				}
			})
		})
	}
}

func TestDoCanceledBeforeCall(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := retry.Do(ctx, retry.Policy{}, func(context.Context) (int, error) {
		t.Fatal("operation called with canceled context")
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDoCancellationDuringPredicate(t *testing.T) {
	t.Parallel()
	for _, shouldRetry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry=%t", shouldRetry), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			value, err := retry.Do(ctx, retry.Policy{
				MaxAttempts: 3,
				RetryIf: func(error) bool {
					cancel()
					return shouldRetry
				},
				Backoff: func(int) time.Duration {
					t.Error("backoff called after cancellation")
					return 0
				},
			}, func(context.Context) (int, error) {
				calls++
				return 42, errors.New("temporary")
			})
			if value != 42 || !errors.Is(err, context.Canceled) || calls != 1 {
				t.Fatalf("value=%d error=%v calls=%d", value, err, calls)
			}
		})
	}
}

func TestDoDeadlineDuringBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		calls := 0
		start := time.Now()
		value, err := retry.Do(ctx, retry.Policy{
			MaxAttempts: 3,
			RetryIf:     always,
			Backoff:     func(int) time.Duration { return time.Hour },
		}, func(context.Context) (int, error) {
			calls++
			return 42, errors.New("temporary")
		})
		if value != 42 || !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) != time.Second {
			t.Fatalf("value=%d error=%v calls=%d elapsed=%v", value, err, calls, time.Since(start))
		}
	})
}

func TestDoPolicyCallbacksRespectAttemptBudget(t *testing.T) {
	t.Parallel()
	failure := errors.New("temporary")
	var attempts []int
	var calls, predicates int
	value, err := retry.Do(t.Context(), retry.Policy{
		MaxAttempts: 3,
		RetryIf: func(error) bool {
			predicates++
			return true
		},
		Backoff: func(attempt int) time.Duration {
			attempts = append(attempts, attempt)
			return 0
		},
	}, func(context.Context) (int, error) {
		calls++
		return calls, failure
	})
	if value != 3 || !errors.Is(err, failure) || calls != 3 || predicates != 2 || !reflect.DeepEqual(attempts, []int{1, 2}) {
		t.Fatalf("value=%d error=%v calls=%d predicates=%d backoffs=%v", value, err, calls, predicates, attempts)
	}
}

func TestDoOperationCancelsContext(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			value, err := retry.Do(ctx, retry.Policy{MaxAttempts: 3, RetryIf: always}, func(context.Context) (int, error) {
				cancel()
				if success {
					return 42, nil
				}
				return 42, errors.New("temporary")
			})
			if value != 42 || success && err != nil || !success && !errors.Is(err, context.Canceled) {
				t.Fatalf("value=%d error=%v", value, err)
			}
		})
	}
}

func TestDoContextErrorsAreTerminal(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			calls := 0
			_, err := retry.Do(t.Context(), retry.Policy{MaxAttempts: 3, RetryIf: always}, func(context.Context) (int, error) {
				calls++
				return 0, fmt.Errorf("operation: %w", cause)
			})
			if calls != 1 || !errors.Is(err, cause) {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func always(error) bool { return true }

func ExampleDo() {
	temporary := errors.New("temporary")
	calls := 0
	value, err := retry.Do(context.Background(), retry.Policy{
		MaxAttempts: 3,
		RetryIf:     func(err error) bool { return errors.Is(err, temporary) },
	}, func(context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", temporary
		}
		return "ready", nil
	})
	fmt.Println(value, calls, err)
	// Output: ready 3 <nil>
}
