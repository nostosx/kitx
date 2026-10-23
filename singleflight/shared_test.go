package singleflight_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/singleflight"
)

func TestSharedInitiatorCanLeave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g, err := singleflight.NewShared[string, int](t.Context(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = g.Close() }()
		started, release := make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		cause := errors.New("request left")
		first := make(chan error, 1)
		var calls atomic.Int32
		go func() {
			_, err := g.Do(ctx, "key", func(ctx context.Context) (int, error) {
				calls.Add(1)
				close(started)
				<-release
				return 42, context.Cause(ctx)
			})
			first <- err
		}()
		<-started
		cancel(cause)
		if err := <-first; !errors.Is(err, cause) {
			t.Fatal(err)
		}
		second := make(chan int, 1)
		go func() {
			value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) { t.Error("duplicate execution"); return 0, nil })
			if err != nil {
				t.Error(err)
			}
			second <- value
		}()
		synctest.Wait()
		close(release)
		if value := <-second; value != 42 || calls.Load() != 1 {
			t.Fatalf("value=%d calls=%d", value, calls.Load())
		}
	})
}

func TestSharedCloseCancelsAndJoins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g, err := singleflight.NewShared[string, int](t.Context(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{})
		var settled atomic.Bool
		result := make(chan error, 1)
		go func() {
			_, err := g.Do(t.Context(), "key", func(ctx context.Context) (int, error) {
				close(started)
				<-ctx.Done()
				time.Sleep(time.Second)
				settled.Store(true)
				return 0, context.Cause(ctx)
			})
			result <- err
		}()
		<-started
		var closers sync.WaitGroup
		for range 8 {
			closers.Go(func() {
				if err := g.Close(); err != nil {
					t.Error(err)
				}
				if !settled.Load() {
					t.Error("Close returned before callback")
				}
			})
		}
		closers.Wait()
		if err := <-result; !errors.Is(err, singleflight.ErrClosed) {
			t.Fatal(err)
		}
		if _, err := g.Do(t.Context(), "new", func(context.Context) (int, error) { t.Error("admitted after close"); return 0, nil }); !errors.Is(err, singleflight.ErrClosed) {
			t.Fatal(err)
		}
	})
}

func TestSharedTimeoutAndOwner(t *testing.T) {
	for _, mode := range []string{"timeout", "owner", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				owner, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				g, err := singleflight.NewShared[string, int](owner, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = g.Close() }()
				cause := errors.New("application stopped")
				want := context.DeadlineExceeded
				_, err = g.Do(t.Context(), "key", func(ctx context.Context) (int, error) {
					if mode == "goexit" {
						runtime.Goexit()
					}
					if mode == "owner" {
						cancel(cause)
					}
					<-ctx.Done()
					return 0, context.Cause(ctx)
				})
				switch mode {
				case "goexit":
					want = singleflight.ErrGoexit
				case "owner":
					want = cause
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v want %v", err, want)
				}
			})
		})
	}
}

func TestSharedValidation(t *testing.T) {
	t.Parallel()
	for _, timeout := range []time.Duration{0, -1} {
		if g, err := singleflight.NewShared[string, int](t.Context(), timeout); g != nil || !errors.Is(err, singleflight.ErrInvalidTimeout) {
			t.Fatalf("NewShared=%v,%v", g, err)
		}
	}
	var zero singleflight.SharedGroup[string, int]
	if _, err := zero.Do(t.Context(), "k", func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, singleflight.ErrClosed) {
		t.Fatal(err)
	}
	if err := zero.Close(); err != nil {
		t.Fatal(err)
	}
	g, err := singleflight.NewShared[float64, int](t.Context(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	if _, err := g.Do(t.Context(), 0, nil); !errors.Is(err, singleflight.ErrInvalidFunc) {
		t.Fatal(err)
	}
	if _, err := g.Do(t.Context(), math.NaN(), func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, singleflight.ErrInvalidKey) {
		t.Fatal(err)
	}
}

func ExampleSharedGroup() {
	group, err := singleflight.NewShared[string, int](context.Background(), time.Minute)
	if err != nil {
		panic(err)
	}
	defer func() { _ = group.Close() }()

	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	completed := make(chan int, 1)
	result := make(chan error, 1)
	go func() {
		_, err := group.Do(request, "tenant-a:report", func(ctx context.Context) (int, error) {
			close(started)
			select {
			case <-release:
				completed <- 42
				return 42, nil
			case <-ctx.Done():
				completed <- 0
				return 0, context.Cause(ctx)
			}
		})
		result <- err
	}()
	<-started
	cancel()
	fmt.Println("request canceled:", errors.Is(<-result, context.Canceled))

	// Shared work belongs to the group owner and survives this request.
	close(release)
	fmt.Println("shared result:", <-completed)
	// Output:
	// request canceled: true
	// shared result: 42
}

func TestSharedOwnerValuesAndNoResultCache(t *testing.T) {
	t.Parallel()
	type scopeKey struct{}
	owner, cancel := context.WithCancelCause(context.WithValue(t.Context(), scopeKey{}, "owner"))
	defer cancel(nil)
	g, err := singleflight.NewShared[string, int](owner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	waiter := context.WithValue(t.Context(), scopeKey{}, "waiter")
	for want := 1; want <= 2; want++ {
		value, err := g.Do(waiter, "key", func(ctx context.Context) (int, error) {
			if got := ctx.Value(scopeKey{}); got != "owner" {
				t.Errorf("execution scope = %v", got)
			}
			return want, nil
		})
		if err != nil || value != want {
			t.Fatalf("uncached execution = %d, %v; want %d", value, err, want)
		}
	}
	cause := errors.New("owner stopped")
	cancel(cause)
	if _, err := g.Do(waiter, "new", func(context.Context) (int, error) {
		t.Error("admitted after owner cancellation")
		return 0, nil
	}); !errors.Is(err, cause) {
		t.Fatal(err)
	}
}

func TestSharedCloseRacesAdmission(t *testing.T) {
	t.Parallel()
	g, err := singleflight.NewShared[int, int](t.Context(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var callers sync.WaitGroup
	for key := range 100 {
		callers.Go(func() {
			<-start
			_, err := g.Do(t.Context(), key, func(ctx context.Context) (int, error) {
				<-ctx.Done()
				return 0, context.Cause(ctx)
			})
			if !errors.Is(err, singleflight.ErrClosed) {
				t.Errorf("Do racing Close = %v", err)
			}
		})
	}
	close(start)
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	callers.Wait()
}

func TestSharedInvalidInterfaceKeyDoesNotLockGroup(t *testing.T) {
	t.Parallel()
	g, err := singleflight.NewShared[any, int](t.Context(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	fn := func(context.Context) (int, error) { return 42, nil }
	func() {
		defer func() {
			if recover() == nil {
				t.Error("non-comparable key did not panic")
			}
		}()
		_, _ = g.Do(t.Context(), []int{1}, fn)
	}()
	if value, err := g.Do(t.Context(), "valid", fn); err != nil || value != 42 {
		t.Fatalf("valid key after panic = %d, %v", value, err)
	}
}
