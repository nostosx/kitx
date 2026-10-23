package parallel_test

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/parallel"
)

func TestForEachBoundAndJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var active, processed atomic.Int32
		started := make(chan struct{}, 2)
		release := make(chan struct{})
		result := make(chan error, 1)
		go func() {
			result <- parallel.ForEach(t.Context(), []int{1, 2, 3, 4}, 2, func(context.Context, int) error {
				if active.Add(1) > 2 {
					t.Error("worker limit exceeded")
				}
				defer active.Add(-1)
				started <- struct{}{}
				<-release
				processed.Add(1)
				return nil
			})
		}()
		<-started
		<-started
		synctest.Wait()
		if processed.Load() != 0 {
			t.Fatal("workers escaped barrier")
		}
		close(release)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		if active.Load() != 0 || processed.Load() != 4 {
			t.Fatalf("active=%d processed=%d", active.Load(), processed.Load())
		}
	})
}

func TestForEachSeqStopsProducer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("stop")
		yielded := 0
		sequence := func(yield func(int) bool) {
			for i := 0; ; i++ {
				yielded++
				if !yield(i) {
					return
				}
			}
		}
		err := parallel.ForEachSeq(t.Context(), func(context.Context) iter.Seq[int] { return sequence }, 1,
			func(context.Context, int) error { return failure })
		if !errors.Is(err, failure) || yielded > 2 {
			t.Fatalf("error=%v yielded=%d", err, yielded)
		}
	})
}

func TestForEachSeqIteratorPanicJoinsWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var settled atomic.Bool
		started := make(chan struct{})
		func() {
			defer func() {
				if recover() != "iterator failed" {
					t.Error("iterator panic lost")
				}
			}()
			_ = parallel.ForEachSeq(t.Context(), func(context.Context) iter.Seq[int] {
				return func(yield func(int) bool) {
					if !yield(1) {
						return
					}
					<-started
					panic("iterator failed")
				}
			}, 1, func(ctx context.Context, _ int) error {
				close(started)
				<-ctx.Done()
				settled.Store(true)
				return ctx.Err()
			})
		}()
		if !settled.Load() {
			t.Fatal("returned without joining worker")
		}
	})
}

func TestForEachCallbackGoexit(t *testing.T) {
	err := parallel.ForEach(t.Context(), []int{1}, 1, func(context.Context, int) error { runtime.Goexit(); return nil })
	if !errors.Is(err, parallel.ErrGoexit) {
		t.Fatal(err)
	}
}

func TestForEachSeqIteratorGoexitJoinsWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, finished := make(chan struct{}), make(chan struct{})
		var settled atomic.Bool
		go func() {
			defer close(finished)
			_ = parallel.ForEachSeq(t.Context(), func(context.Context) iter.Seq[int] {
				return func(yield func(int) bool) {
					if !yield(1) {
						return
					}
					<-started
					runtime.Goexit()
				}
			}, 1, func(ctx context.Context, _ int) error {
				close(started)
				<-ctx.Done()
				settled.Store(true)
				return ctx.Err()
			})
			t.Error("iterator Goexit returned normally")
		}()
		<-finished
		if !settled.Load() {
			t.Fatal("Goexit did not join worker")
		}
	})
}

func TestForEachSeqValidationAndCancellation(t *testing.T) {
	t.Parallel()
	source := func(context.Context) iter.Seq[int] { return func(func(int) bool) {} }
	fn := func(context.Context, int) error { return nil }
	for _, tt := range []struct {
		name   string
		source func(context.Context) iter.Seq[int]
		limit  int
		fn     func(context.Context, int) error
	}{
		{"nil source", nil, 1, fn},
		{"nil sequence", func(context.Context) iter.Seq[int] { return nil }, 1, fn},
		{"nil callback", source, 1, nil}, {"zero limit", source, 0, fn},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := parallel.ForEachSeq(t.Context(), tt.source, tt.limit, tt.fn); !errors.Is(err, parallel.ErrInvalidArgument) {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("owner stopped")
	cancel(cause)
	if err := parallel.ForEachSeq(ctx, func(context.Context) iter.Seq[int] { t.Error("canceled source ran"); return nil }, 1, fn); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err := parallel.ForEach[int](t.Context(), nil, 1, fn); err != nil {
		t.Fatal(err)
	}
}

func TestForEachLimitExceedsInput(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		inputs []int
	}{
		{"empty", nil}, {"single", []int{42}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			err := parallel.ForEach(t.Context(), tt.inputs, math.MaxInt, func(_ context.Context, value int) error {
				calls.Add(1)
				if value != 42 {
					t.Errorf("value = %d", value)
				}
				return nil
			})
			if err != nil || int(calls.Load()) != len(tt.inputs) {
				t.Fatalf("ForEach = %v, calls = %d", err, calls.Load())
			}
		})
	}
}

func TestForEachSeqFailureCancelsBlockedInput(t *testing.T) {
	for _, mode := range []string{"error", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
				defer cancel()
				waiting := make(chan struct{})
				failure := errors.New("worker failed")
				var inputContext context.Context
				source := func(ctx context.Context) iter.Seq[int] {
					inputContext = ctx
					return func(yield func(int) bool) {
						if !yield(1) {
							return
						}
						close(waiting)
						<-ctx.Done()
					}
				}
				err := parallel.ForEachSeq(ctx, source, 1, func(context.Context, int) error {
					<-waiting
					if mode == "goexit" {
						runtime.Goexit()
					}
					return failure
				})
				want := failure
				if mode == "goexit" {
					want = parallel.ErrGoexit
				}
				if !errors.Is(err, want) || !errors.Is(context.Cause(inputContext), want) || time.Since(start) != 0 {
					t.Fatalf("error = %v, source cause = %v, elapsed = %v", err, context.Cause(inputContext), time.Since(start))
				}
			})
		})
	}
}

func TestForEachSeqReleasesSourceContext(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"exhaustion", "nil sequence", "panic"} {
		t.Run(mode, func(t *testing.T) {
			var sourceContext context.Context
			calls := 0
			func() {
				defer func() {
					got := recover()
					var want any
					if mode == "panic" {
						want = "source failed"
					}
					if got != want {
						t.Errorf("source panic = %v", got)
					}
				}()
				err := parallel.ForEachSeq(t.Context(), func(ctx context.Context) iter.Seq[int] {
					calls++
					sourceContext = ctx
					switch mode {
					case "nil sequence":
						return nil
					case "panic":
						panic("source failed")
					}
					return func(func(int) bool) {}
				}, 1, func(context.Context, int) error {
					t.Error("empty source invoked callback")
					return nil
				})
				if mode == "nil sequence" {
					if !errors.Is(err, parallel.ErrInvalidArgument) {
						t.Fatalf("ForEachSeq = %v", err)
					}
				} else if err != nil {
					t.Fatalf("ForEachSeq = %v", err)
				}
			}()
			if calls != 1 || !errors.Is(context.Cause(sourceContext), context.Canceled) {
				t.Fatalf("source calls = %d, context cause = %v", calls, context.Cause(sourceContext))
			}
		})
	}
}

func ExampleForEachSeq() {
	source := func(ctx context.Context) iter.Seq[int] {
		return func(yield func(int) bool) {
			for n := range 3 {
				if ctx.Err() != nil || !yield(n) {
					return
				}
			}
		}
	}
	var total atomic.Int64
	err := parallel.ForEachSeq(context.Background(), source, 2, func(_ context.Context, n int) error {
		total.Add(int64(n))
		return nil
	})
	fmt.Println(total.Load(), err)
	// Output: 3 <nil>
}

func ExampleForEach() {
	var total atomic.Int64
	err := parallel.ForEach(context.Background(), []int64{1, 2, 3}, 2, func(_ context.Context, n int64) error { total.Add(n); return nil })
	fmt.Println(total.Load(), err)
	// Output: 6 <nil>
}
