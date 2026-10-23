package ratelimit_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/ratelimit"
)

func TestValidationAndZero(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		interval time.Duration
		burst    int
	}{
		{"zero interval", 0, 1}, {"negative interval", -1, 1},
		{"zero burst", time.Second, 0}, {"negative burst", time.Second, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if l, err := ratelimit.New(tt.interval, tt.burst); l != nil || !errors.Is(err, ratelimit.ErrInvalidArgument) {
				t.Fatalf("New = %v, %v", l, err)
			}
		})
	}
	var l ratelimit.Limiter
	if l.Allow() || !errors.Is(l.Wait(t.Context()), ratelimit.ErrInvalidArgument) {
		t.Fatal("zero limiter admitted work")
	}
}

func TestRefillAndBurstCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l, _ := ratelimit.New(time.Second, 2)
		for i, want := range []bool{true, true, false} {
			if got := l.Allow(); got != want {
				t.Fatalf("initial admission %d = %v, want %v", i, got, want)
			}
		}
		time.Sleep(999 * time.Millisecond)
		if l.Allow() {
			t.Fatal("refilled before interval")
		}
		time.Sleep(time.Millisecond)
		if !l.Allow() || l.Allow() {
			t.Fatal("expected one refill")
		}
		time.Sleep(100 * time.Hour)
		for i, want := range []bool{true, true, false} {
			if got := l.Allow(); got != want {
				t.Fatalf("idle admission %d = %v, want %v", i, got, want)
			}
		}
	})
}

func TestWaitCancellationDoesNotReserve(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l, _ := ratelimit.New(time.Second, 1)
		l.Allow()
		ctx, cancel := context.WithCancelCause(t.Context())
		cause := errors.New("request left")
		result := make(chan error, 1)
		go func() { result <- l.Wait(ctx) }()
		synctest.Wait()
		cancel(cause)
		if err := <-result; !errors.Is(err, cause) {
			t.Fatal(err)
		}
		start := time.Now()
		if err := l.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != time.Second {
			t.Fatal("canceled wait left token debt")
		}
		cancelled, stop := context.WithCancel(t.Context())
		stop()
		time.Sleep(time.Second)
		if !errors.Is(l.Wait(cancelled), context.Canceled) || !l.Allow() {
			t.Fatal("canceled admission consumed a token")
		}
	})
}

func TestConcurrentAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l, _ := ratelimit.New(time.Hour, 7)
		var admitted atomic.Int32
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				if l.Allow() {
					admitted.Add(1)
				}
			})
		}
		wg.Wait()
		if admitted.Load() != 7 {
			t.Fatalf("admitted %d", admitted.Load())
		}
		large, err := ratelimit.New(time.Nanosecond, math.MaxInt)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(math.MaxInt64))
		if !large.Allow() {
			t.Fatal("large refill overflowed")
		}
	})
}

func ExampleLimiter() {
	l, err := ratelimit.New(time.Hour, 2)
	if err != nil {
		panic(err)
	}
	fmt.Println(l.Allow(), l.Allow(), l.Allow())
	// Output: true true false
}
