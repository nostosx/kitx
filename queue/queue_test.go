package queue_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/nostosx/kitx/queue"
)

func TestFIFOAndDrain(t *testing.T) {
	t.Parallel()
	q, err := queue.New[int](2)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		if err := q.Push(t.Context(), i); err != nil {
			t.Fatal(err)
		}
		if got, err := q.Pop(t.Context()); got != i || err != nil {
			t.Fatalf("Pop = %d, %v", got, err)
		}
	}
	for _, n := range []int{10, 20} {
		if err := q.Push(t.Context(), n); err != nil {
			t.Fatal(err)
		}
	}
	if q.Len() != 2 {
		t.Fatal(q.Len())
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(q.Push(t.Context(), 30), queue.ErrClosed) {
		t.Fatal("push after close succeeded")
	}
	for _, want := range []int{10, 20} {
		if got, err := q.Pop(t.Context()); got != want || err != nil {
			t.Fatalf("drain = %d, %v", got, err)
		}
	}
	if got, err := q.Pop(t.Context()); got != 0 || !errors.Is(err, queue.ErrClosed) {
		t.Fatalf("empty = %d, %v", got, err)
	}
}

func TestBlockingCancellationAndClose(t *testing.T) {
	for _, operation := range []string{"push", "pop"} {
		for _, exit := range []string{"cancel", "close"} {
			t.Run(operation+" "+exit, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					q, _ := queue.New[int](1)
					if operation == "push" {
						if err := q.Push(t.Context(), 1); err != nil {
							t.Fatal(err)
						}
					}
					ctx, cancel := context.WithCancelCause(t.Context())
					defer cancel(nil)
					cause := errors.New("stopped")
					result := make(chan error, 1)
					go func() {
						if operation == "push" {
							result <- q.Push(ctx, 2)
						} else {
							_, err := q.Pop(ctx)
							result <- err
						}
					}()
					synctest.Wait()
					want := queue.ErrClosed
					if exit == "cancel" {
						cancel(cause)
						want = cause
					} else if err := q.Close(); err != nil {
						t.Fatal(err)
					}
					if err := <-result; !errors.Is(err, want) {
						t.Fatalf("got %v, want %v", err, want)
					}
					if operation == "push" && q.Len() != 1 {
						t.Fatal("blocked push changed queue")
					}
				})
			})
		}
	}
}

func TestBackpressureAndConcurrentDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, _ := queue.New[int](3)
		var producers, consumers sync.WaitGroup
		results := make(chan int, 100)
		for worker := range 4 {
			producers.Go(func() {
				for i := range 25 {
					if err := q.Push(t.Context(), worker*25+i); err != nil {
						t.Error(err)
						return
					}
				}
			})
		}
		for range 3 {
			consumers.Go(func() {
				for {
					value, err := q.Pop(t.Context())
					if errors.Is(err, queue.ErrClosed) {
						return
					}
					if err != nil {
						t.Error(err)
						return
					}
					results <- value
				}
			})
		}
		producers.Wait()
		if err := q.Close(); err != nil {
			t.Fatal(err)
		}
		consumers.Wait()
		close(results)
		seen := make(map[int]bool)
		for n := range results {
			if seen[n] {
				t.Fatalf("duplicate %d", n)
			}
			seen[n] = true
		}
		if len(seen) != 100 || q.Len() != 0 {
			t.Fatalf("received %d, remaining %d", len(seen), q.Len())
		}
	})
}

func TestZeroAndCanceledOperations(t *testing.T) {
	t.Parallel()
	for _, capacity := range []int{-1, 0} {
		if q, err := queue.New[int](capacity); q != nil || !errors.Is(err, queue.ErrInvalidCapacity) {
			t.Fatalf("New = %v, %v", q, err)
		}
	}
	var zero queue.Queue[int]
	if !errors.Is(zero.Push(t.Context(), 1), queue.ErrClosed) {
		t.Fatal("zero push")
	}
	if _, err := zero.Pop(t.Context()); !errors.Is(err, queue.ErrClosed) {
		t.Fatal(err)
	}
	if err := zero.Close(); err != nil {
		t.Fatal(err)
	}
	q, _ := queue.New[int](1)
	if err := q.Push(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.Pop(ctx); !errors.Is(err, context.Canceled) || q.Len() != 1 {
		t.Fatal("canceled pop consumed value")
	}
	if !errors.Is(q.Push(ctx, 2), context.Canceled) {
		t.Fatal("canceled push succeeded")
	}
}

func ExampleQueue() {
	q, err := queue.New[string](2)
	if err != nil {
		panic(err)
	}
	if err := q.Push(context.Background(), "work"); err != nil {
		panic(err)
	}
	if err := q.Close(); err != nil {
		panic(err)
	}
	value, err := q.Pop(context.Background())
	fmt.Println(value, err)
	_, err = q.Pop(context.Background())
	fmt.Println(errors.Is(err, queue.ErrClosed))
	// Output:
	// work <nil>
	// true
}

func TestBlockedOperationsResumeOnProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, _ := queue.New[int](1)
		if err := q.Push(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		pushed := make(chan error, 1)
		go func() { pushed <- q.Push(t.Context(), 2) }()
		synctest.Wait()
		if got, err := q.Pop(t.Context()); got != 1 || err != nil {
			t.Fatalf("first pop = %d, %v", got, err)
		}
		if err := <-pushed; err != nil {
			t.Fatal(err)
		}
		if got, err := q.Pop(t.Context()); got != 2 || err != nil {
			t.Fatalf("resumed push = %d, %v", got, err)
		}
		popped := make(chan int, 1)
		go func() {
			got, err := q.Pop(t.Context())
			if err != nil {
				t.Error(err)
			}
			popped <- got
		}()
		synctest.Wait()
		if err := q.Push(t.Context(), 3); err != nil {
			t.Fatal(err)
		}
		if got := <-popped; got != 3 {
			t.Fatalf("resumed pop = %d", got)
		}
	})
}
