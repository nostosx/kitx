package singleflight_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/nostosx/kitx/singleflight"
)

func TestDoSharesOverlappingCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, int]
		started, finish := make(chan struct{}), make(chan struct{})
		failure := errors.New("backend unavailable")
		var wg sync.WaitGroup
		wg.Go(func() {
			value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) {
				close(started)
				<-finish
				return 42, failure
			})
			if value != 42 || !errors.Is(err, failure) {
				t.Errorf("leader = %d, %v", value, err)
			}
		})
		<-started
		for range 16 {
			wg.Go(func() {
				value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) {
					t.Error("overlapping callback executed")
					return 0, nil
				})
				if value != 42 || !errors.Is(err, failure) {
					t.Errorf("waiter = %d, %v", value, err)
				}
			})
		}
		synctest.Wait()
		close(finish)
		wg.Wait()
		value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) { return 7, nil })
		if value != 7 || err != nil {
			t.Fatalf("later call = %d, %v; completed result was retained", value, err)
		}
	})
}

func TestDoWaiterCancellationDoesNotCancelLeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, int]
		finish, started, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			value, err := g.Do(t.Context(), "key", func(ctx context.Context) (int, error) {
				close(started)
				<-finish
				return 42, ctx.Err()
			})
			if value != 42 || err != nil {
				t.Errorf("leader = %d, %v", value, err)
			}
		}()
		<-started
		ctx, cancel := context.WithCancelCause(t.Context())
		cause := errors.New("waiter left")
		waiterDone := make(chan struct{})
		go func() {
			defer close(waiterDone)
			value, err := g.Do(ctx, "key", func(context.Context) (int, error) {
				t.Error("waiter callback executed")
				return 0, nil
			})
			if value != 0 || !errors.Is(err, cause) {
				t.Errorf("waiter = %d, %v", value, err)
			}
		}()
		synctest.Wait()
		cancel(cause)
		<-waiterDone
		value, err := g.Do(t.Context(), "other", func(context.Context) (int, error) { return 9, nil })
		if value != 9 || err != nil {
			t.Errorf("independent key = %d, %v", value, err)
		}
		close(finish)
		<-done
	})
}

func TestDoCanceledWaitersRejectCompletedResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, int]
		started, finish := make(chan struct{}), make(chan struct{})
		cause := errors.New("waiter no longer needs result")
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		var wg sync.WaitGroup
		wg.Go(func() {
			value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) {
				close(started)
				<-finish
				cancel(cause)
				return 42, nil
			})
			if value != 42 || err != nil {
				t.Errorf("leader = %d, %v", value, err)
			}
		})
		<-started
		for range 16 {
			wg.Go(func() {
				value, err := g.Do(ctx, "key", func(context.Context) (int, error) {
					t.Error("waiter callback executed")
					return 0, nil
				})
				if value != 0 || !errors.Is(err, cause) {
					t.Errorf("canceled waiter = %d, %v", value, err)
				}
			})
		}
		synctest.Wait()
		close(finish)
		wg.Wait()
	})
}

func TestDoUsesFirstCallerContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type key struct{}
		ctx, cancel := context.WithCancelCause(context.WithValue(t.Context(), key{}, "owner"))
		defer cancel(nil)
		cause := errors.New("owner stopped")
		var g singleflight.Group[string, string]
		started, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			_, _ = g.Do(ctx, "key", func(ctx context.Context) (string, error) {
				close(started)
				<-ctx.Done()
				return ctx.Value(key{}).(string), context.Cause(ctx)
			})
		}()
		<-started
		waiterDone := make(chan struct{})
		go func() {
			defer close(waiterDone)
			value, err := g.Do(t.Context(), "key", func(context.Context) (string, error) {
				t.Error("waiter callback executed")
				return "", nil
			})
			if value != "owner" || !errors.Is(err, cause) {
				t.Errorf("shared result = %q, %v", value, err)
			}
		}()
		synctest.Wait()
		cancel(cause)
		<-done
		<-waiterDone
	})
}

func TestDoInvalidOrCanceledCallsDoNotExecute(t *testing.T) {
	t.Parallel()
	var g singleflight.Group[string, int]
	if _, err := g.Do(t.Context(), "key", nil); !errors.Is(err, singleflight.ErrInvalidFunc) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := g.Do(ctx, "key", func(context.Context) (int, error) {
		t.Error("canceled callback executed")
		return 0, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	value, err := g.Do(ctx, "key", func(context.Context) (int, error) { cancel(); return 42, nil })
	if value != 42 || err != nil {
		t.Fatalf("successful leader = %d, %v", value, err)
	}
}

func TestDoPanicReleasesWaitersAndKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, int]
		started, finish := make(chan struct{}), make(chan struct{})
		panics := make(chan any, 2)
		invoke := func(fn func(context.Context) (int, error)) {
			defer func() { panics <- recover() }()
			_, _ = g.Do(t.Context(), "key", fn)
		}
		go invoke(func(context.Context) (int, error) { close(started); <-finish; panic("broken") })
		<-started
		go invoke(func(context.Context) (int, error) {
			t.Error("waiter callback executed")
			return 0, nil
		})
		synctest.Wait()
		close(finish)
		for range 2 {
			if p := <-panics; p != "broken" {
				t.Fatalf("panic = %v", p)
			}
		}
		if value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) { return 7, nil }); value != 7 || err != nil {
			t.Fatalf("after panic = %d, %v", value, err)
		}
	})
}

func TestDoInvalidKeyDoesNotRetainOrLockGroup(t *testing.T) {
	t.Parallel()
	var g singleflight.Group[any, int]
	fn := func(context.Context) (int, error) { return 42, nil }
	if _, err := g.Do(t.Context(), math.NaN(), fn); !errors.Is(err, singleflight.ErrInvalidKey) {
		t.Fatal(err)
	}
	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		_, _ = g.Do(t.Context(), []int{1}, fn)
	}()
	if panicValue == nil {
		t.Fatal("non-comparable interface key did not panic")
	}
	if value, err := g.Do(t.Context(), "valid", fn); value != 42 || err != nil {
		t.Fatalf("group after invalid key = %d, %v", value, err)
	}
}

func TestDoGoexitReleasesWaitersAndKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g singleflight.Group[string, int]
		started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			_, _ = g.Do(t.Context(), "key", func(context.Context) (int, error) {
				close(started)
				<-finish
				runtime.Goexit()
				return 0, nil
			})
			t.Error("Goexit returned")
		}()
		<-started
		waiter := make(chan error, 1)
		go func() {
			_, err := g.Do(t.Context(), "key", func(context.Context) (int, error) {
				t.Error("waiter callback executed")
				return 0, nil
			})
			waiter <- err
		}()
		synctest.Wait()
		close(finish)
		<-done
		if err := <-waiter; !errors.Is(err, singleflight.ErrGoexit) {
			t.Fatal(err)
		}
		if value, err := g.Do(t.Context(), "key", func(context.Context) (int, error) { return 7, nil }); value != 7 || err != nil {
			t.Fatalf("after Goexit = %d, %v", value, err)
		}
	})
}

func ExampleGroup() {
	var g singleflight.Group[string, string]
	value, err := g.Do(context.Background(), "settings", func(context.Context) (string, error) {
		return "loaded", nil
	})
	fmt.Println(value, err)
	// Output: loaded <nil>
}
