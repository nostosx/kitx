package parallel_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/backoff"
	"github.com/nostosx/kitx/parallel"
	"github.com/nostosx/kitx/retry"
)

func TestMap(t *testing.T) {
	for _, limit := range []int{1, 3, 20} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var active, peak atomic.Int32
				inputs := []int{1, 2, 3, 4, 5, 6}
				got, err := parallel.Map(t.Context(), inputs, limit, func(_ context.Context, value int) (int, error) {
					current := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); current > old; old = peak.Load() {
						if peak.CompareAndSwap(old, current) {
							break
						}
					}
					time.Sleep(time.Duration(7-value) * time.Second)
					return value * value, nil
				})
				if err != nil || !reflect.DeepEqual(got, []int{1, 4, 9, 16, 25, 36}) {
					t.Fatalf("results=%v error=%v", got, err)
				}
				if active.Load() != 0 || int(peak.Load()) != min(limit, len(inputs)) {
					t.Fatalf("active=%d peak=%d limit=%d", active.Load(), peak.Load(), limit)
				}
				if !reflect.DeepEqual(inputs, []int{1, 2, 3, 4, 5, 6}) {
					t.Fatal("inputs changed")
				}
			})
		})
	}
}

func TestMapFailureWaitsForWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("task failed")
		started := make(chan struct{})
		var settled atomic.Bool
		var calls atomic.Int32
		got, err := parallel.Map(t.Context(), []int{0, 1, 2, 3, 4}, 2, func(ctx context.Context, value int) (int, error) {
			calls.Add(1)
			if value == 0 {
				<-started
				return 0, failure
			}
			close(started)
			<-ctx.Done()
			time.Sleep(time.Second)
			settled.Store(true)
			return value, ctx.Err()
		})
		if !errors.Is(err, failure) || got != nil || !settled.Load() || calls.Load() != 2 {
			t.Fatalf("results=%v error=%v settled=%v calls=%d", got, err, settled.Load(), calls.Load())
		}
	})
}

func TestMapParentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("caller stopped work")
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		started := make(chan struct{}, 2)
		go func() {
			<-started
			<-started
			cancel(cause)
		}()
		var settled atomic.Int32
		got, err := parallel.Map(ctx, []int{1, 2, 3}, 2, func(ctx context.Context, value int) (int, error) {
			started <- struct{}{}
			<-ctx.Done()
			settled.Add(1)
			return value, ctx.Err()
		})
		if got != nil || !errors.Is(err, cause) || settled.Load() != 2 {
			t.Fatalf("results=%v error=%v settled=%d", got, err, settled.Load())
		}
	})
}

func TestMapGoexitCancelsOtherWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		var settled atomic.Bool
		got, err := parallel.Map(t.Context(), []int{0, 1}, 2, func(ctx context.Context, value int) (int, error) {
			if value == 0 {
				<-started
				runtime.Goexit()
			}
			close(started)
			<-ctx.Done()
			settled.Store(true)
			return 0, ctx.Err()
		})
		if got != nil || !errors.Is(err, parallel.ErrGoexit) || !settled.Load() {
			t.Fatalf("result=%v error=%v settled=%v", got, err, settled.Load())
		}
	})
}

func TestMapCancelsContextAfterSuccess(t *testing.T) {
	t.Parallel()
	var callbackContext context.Context
	got, err := parallel.Map(t.Context(), []int{42}, 1, func(ctx context.Context, value int) (int, error) {
		callbackContext = ctx
		return value, nil
	})
	if err != nil || !reflect.DeepEqual(got, []int{42}) {
		t.Fatalf("results=%v error=%v", got, err)
	}
	if callbackContext == nil || !errors.Is(callbackContext.Err(), context.Canceled) {
		t.Fatal("callback context not canceled before return")
	}
	if err := t.Context().Err(); err != nil {
		t.Fatalf("parent context canceled: %v", err)
	}
}

func TestMapEmptyAndInvalid(t *testing.T) {
	t.Parallel()
	fn := func(context.Context, int) (int, error) { t.Error("unexpected callback"); return 0, nil }
	got, err := parallel.Map(t.Context(), nil, 1, fn)
	if got == nil || len(got) != 0 || err != nil {
		t.Fatalf("empty result=%v error=%v", got, err)
	}
	for _, tt := range []struct {
		name  string
		limit int
		fn    func(context.Context, int) (int, error)
	}{
		{"zero limit", 0, fn},
		{"negative limit", -1, fn},
		{"nil function", 1, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := parallel.Map(t.Context(), []int{1}, tt.limit, tt.fn); got != nil || !errors.Is(err, parallel.ErrInvalidArgument) {
				t.Fatalf("results=%v error=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := parallel.Map(ctx, []int{1}, 1, fn); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled result=%v error=%v", got, err)
	}
}

func ExampleMap() {
	lengths, err := parallel.Map(context.Background(), []string{"agentx", "servex", "kitx"}, 2,
		func(_ context.Context, name string) (int, error) { return len(name), nil })
	fmt.Println(lengths, err)
	// Output: [6 6 4] <nil>
}

func ExampleMap_withRetry() {
	temporary := errors.New("temporary")
	policy := retry.Policy{
		MaxAttempts: 3,
		RetryIf:     func(err error) bool { return errors.Is(err, temporary) },
		Backoff:     backoff.FullJitter(backoff.Exponential(time.Millisecond, 10*time.Millisecond)),
	}
	lengths, err := parallel.Map(context.Background(), []string{"agentx", "servex", "kitx"}, 2,
		func(ctx context.Context, name string) (int, error) {
			attempts := 0
			return retry.Do(ctx, policy, func(context.Context) (int, error) {
				attempts++
				if attempts == 1 {
					return 0, temporary
				}
				return len(name), nil
			})
		})
	fmt.Println(lengths, err)
	// Output: [6 6 4] <nil>
}
