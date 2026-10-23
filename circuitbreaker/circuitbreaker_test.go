package circuitbreaker_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/circuitbreaker"
)

func newBreaker(t *testing.T, threshold, probes int) *circuitbreaker.Breaker {
	t.Helper()
	b, err := circuitbreaker.New(circuitbreaker.Config{FailureThreshold: threshold, OpenTimeout: time.Second, HalfOpenLimit: probes})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func permit(t *testing.T, b *circuitbreaker.Breaker) func(bool) {
	t.Helper()
	done, err := b.Allow()
	if err != nil {
		t.Fatal(err)
	}
	return done
}

func TestConsecutiveFailuresAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBreaker(t, 2, 2)
		permit(t, b)(false)
		permit(t, b)(true)
		permit(t, b)(false)
		if b.State() != circuitbreaker.Closed {
			t.Fatal("success did not reset failures")
		}
		permit(t, b)(false)
		if b.State() != circuitbreaker.Open {
			t.Fatal("threshold did not open")
		}
		if done, err := b.Allow(); done != nil || !errors.Is(err, circuitbreaker.ErrOpen) {
			t.Fatalf("open admission = %v", err)
		}
		time.Sleep(time.Second)
		one, two := permit(t, b), permit(t, b)
		if b.State() != circuitbreaker.HalfOpen {
			t.Fatal(b.State())
		}
		if _, err := b.Allow(); !errors.Is(err, circuitbreaker.ErrOpen) {
			t.Fatal("extra probe admitted")
		}
		one(true)
		one(false) // Completion is idempotent.
		if b.State() != circuitbreaker.HalfOpen {
			t.Fatal("closed before all probes succeeded")
		}
		two(true)
		if b.State() != circuitbreaker.Closed {
			t.Fatal("successful probes did not close")
		}
	})
}

func TestLateResultsCannotChangeNewGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBreaker(t, 1, 2)
		oldSuccess, oldFailure := permit(t, b), permit(t, b)
		permit(t, b)(false)
		oldSuccess(true)
		time.Sleep(time.Second)
		first, late := permit(t, b), permit(t, b)
		first(false)
		late(true)
		if b.State() != circuitbreaker.Open {
			t.Fatal("late probe reopened admission")
		}
		time.Sleep(time.Second)
		permit(t, b)(true)
		permit(t, b)(true)
		oldFailure(false)
		if b.State() != circuitbreaker.Closed {
			t.Fatal("old failure affected recovered breaker")
		}
	})
}

func TestDoClassificationAndAbnormalExit(t *testing.T) {
	t.Parallel()
	failure := errors.New("application error")
	b := newBreaker(t, 1, 0)
	value, err := circuitbreaker.Do(t.Context(), b, func(context.Context) (int, error) { return 42, failure }, func(error) bool { return false })
	if value != 42 || !errors.Is(err, failure) || b.State() != circuitbreaker.Closed {
		t.Fatalf("Do = %d, %v, state %v", value, err, b.State())
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(failure)
	if _, err := circuitbreaker.Do(ctx, b, func(context.Context) (int, error) { t.Error("canceled operation ran"); return 0, nil }, nil); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, mode := range []string{"operation panic", "classifier panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			b := newBreaker(t, 1, 1)
			settled := make(chan struct{})
			go func() {
				defer close(settled)
				defer func() {
					if mode != "goexit" && recover() != "failure" {
						t.Error("panic was not preserved")
					}
				}()
				_, _ = circuitbreaker.Do(t.Context(), b, func(context.Context) (int, error) {
					if mode == "goexit" {
						runtime.Goexit()
					}
					if mode == "operation panic" {
						panic("failure")
					}
					return 0, failure
				}, func(error) bool { panic("failure") })
			}()
			<-settled
			if b.State() != circuitbreaker.Open {
				t.Fatal("abnormal exit lost completion")
			}
		})
	}
}

func TestConcurrentProbeLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBreaker(t, 1, 3)
		permit(t, b)(false)
		time.Sleep(time.Second)
		var admitted atomic.Int32
		var wg sync.WaitGroup
		release := make(chan struct{})
		for range 50 {
			wg.Go(func() {
				done, err := b.Allow()
				if errors.Is(err, circuitbreaker.ErrOpen) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				admitted.Add(1)
				<-release
				done(true)
			})
		}
		synctest.Wait()
		if admitted.Load() != 3 {
			t.Fatalf("admitted %d", admitted.Load())
		}
		close(release)
		wg.Wait()
		if b.State() != circuitbreaker.Closed {
			t.Fatal(b.State())
		}
	})
}

func TestInvalidArguments(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		config circuitbreaker.Config
	}{
		{"zero", circuitbreaker.Config{}},
		{"negative threshold", circuitbreaker.Config{FailureThreshold: -1, OpenTimeout: time.Second}},
		{"negative timeout", circuitbreaker.Config{FailureThreshold: 1, OpenTimeout: -1}},
		{"negative probes", circuitbreaker.Config{FailureThreshold: 1, OpenTimeout: time.Second, HalfOpenLimit: -1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if b, err := circuitbreaker.New(tt.config); b != nil || !errors.Is(err, circuitbreaker.ErrInvalidArgument) {
				t.Fatalf("New=%v,%v", b, err)
			}
		})
	}
	var zero circuitbreaker.Breaker
	if _, err := zero.Allow(); !errors.Is(err, circuitbreaker.ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := circuitbreaker.Do[int](t.Context(), nil, nil, nil); !errors.Is(err, circuitbreaker.ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := circuitbreaker.Do[int](t.Context(), newBreaker(t, 1, 1), nil, nil); !errors.Is(err, circuitbreaker.ErrInvalidArgument) {
		t.Fatal(err)
	}
}

func ExampleDo() {
	b, err := circuitbreaker.New(circuitbreaker.Config{FailureThreshold: 2, OpenTimeout: time.Minute})
	if err != nil {
		panic(err)
	}
	value, err := circuitbreaker.Do(context.Background(), b, func(context.Context) (string, error) { return "ready", nil }, nil)
	fmt.Println(value, err)
	// Output: ready <nil>
}
