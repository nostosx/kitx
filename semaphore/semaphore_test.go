package semaphore_test

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

	"github.com/nostosx/kitx/semaphore"
)

func TestNewAndAcquireValidation(t *testing.T) {
	t.Parallel()
	for _, capacity := range []int{0, -1} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			if s, err := semaphore.New(capacity); s != nil || !errors.Is(err, semaphore.ErrInvalidCapacity) {
				t.Fatalf("New = %v, %v", s, err)
			}
		})
	}
	var zero semaphore.Semaphore
	if release, err := zero.Acquire(t.Context(), 1); release != nil || !errors.Is(err, semaphore.ErrInvalidCapacity) {
		t.Fatalf("zero Acquire = %v", err)
	}
	if _, ok := zero.TryAcquire(1); ok {
		t.Fatal("zero TryAcquire succeeded")
	}
	s, err := semaphore.New(3)
	if err != nil {
		t.Fatal(err)
	}
	for _, weight := range []int{-1, 0, 4} {
		t.Run(fmt.Sprintf("weight=%d", weight), func(t *testing.T) {
			if release, err := s.Acquire(t.Context(), weight); release != nil || !errors.Is(err, semaphore.ErrInvalidWeight) {
				t.Fatalf("Acquire = %v", err)
			}
			if _, ok := s.TryAcquire(weight); ok {
				t.Fatal("invalid TryAcquire succeeded")
			}
		})
	}
}

func TestAcquireMaximumCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := semaphore.New(math.MaxInt)
		if err != nil {
			t.Fatal(err)
		}
		release, err := s.Acquire(t.Context(), math.MaxInt-1)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if r, ok := s.TryAcquire(2); ok {
			r()
			t.Fatal("overflow allowed TryAcquire beyond capacity")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if r, err := s.Acquire(ctx, 2); r != nil || !errors.Is(err, context.DeadlineExceeded) {
			if r != nil {
				r()
			}
			t.Fatalf("overflowing acquisition = %v", err)
		}
		remainder, ok := s.TryAcquire(1)
		if !ok {
			t.Fatal("remaining unit unavailable after cancellation")
		}
		defer remainder()
		if r, ok := s.TryAcquire(math.MaxInt); ok {
			r()
			t.Fatal("full semaphore admitted more work")
		}
		remainder()
		release()
		all, ok := s.TryAcquire(math.MaxInt)
		if !ok {
			t.Fatal("maximum acquisition leaked capacity")
		}
		all()
	})
}

func TestAcquireFIFOAndCanceledHead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := semaphore.New(3)
		if err != nil {
			t.Fatal(err)
		}
		release, err := s.Acquire(t.Context(), 2)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		cause := errors.New("head canceled")
		headDone := make(chan error, 1)
		go func() {
			r, err := s.Acquire(ctx, 3)
			if r != nil {
				r()
			}
			headDone <- err
		}()
		synctest.Wait()
		if _, ok := s.TryAcquire(1); ok {
			t.Fatal("TryAcquire overtook queued head")
		}
		tailDone := make(chan struct{})
		go func() {
			defer close(tailDone)
			r, err := s.Acquire(t.Context(), 1)
			if err != nil {
				t.Error(err)
				return
			}
			r()
		}()
		synctest.Wait()
		select {
		case <-tailDone:
			t.Fatal("tail overtook queued head")
		default:
		}
		cancel(cause)
		if err := <-headDone; !errors.Is(err, cause) {
			t.Fatal(err)
		}
		<-tailDone
		release()
		all, ok := s.TryAcquire(3)
		if !ok {
			t.Fatal("canceled head leaked units")
		}
		all()
	})
}

func TestAcquireFIFOAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := semaphore.New(3)
		if err != nil {
			t.Fatal(err)
		}
		release, err := s.Acquire(t.Context(), 2)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		headRelease := make(chan func(), 1)
		go func() {
			r, err := s.Acquire(t.Context(), 3)
			if err != nil {
				t.Error(err)
			}
			headRelease <- r
		}()
		synctest.Wait()
		tailDone := make(chan struct{})
		go func() {
			defer close(tailDone)
			r, err := s.Acquire(t.Context(), 1)
			if err != nil {
				t.Error(err)
				return
			}
			r()
		}()
		synctest.Wait()
		release()
		r := <-headRelease
		if r == nil {
			t.Fatal("queued head was not admitted")
		}
		defer r()
		synctest.Wait()
		select {
		case <-tailDone:
			t.Fatal("tail admitted before head released capacity")
		default:
		}
		r()
		<-tailDone
	})
}

func TestAcquireQueuedAdmissionAndDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := semaphore.New(1)
		if err != nil {
			t.Fatal(err)
		}
		release, err := s.Acquire(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if r, err := s.Acquire(ctx, 1); r != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline = %v", err)
		}
		admitted := make(chan struct{})
		go func() {
			defer close(admitted)
			r, err := s.Acquire(t.Context(), 1)
			if err != nil {
				t.Error(err)
				return
			}
			r()
		}()
		synctest.Wait()
		release()
		<-admitted
		ctx, cancel = context.WithCancel(t.Context())
		cancel()
		if r, err := s.Acquire(ctx, 1); r != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("already canceled = %v", err)
		}
	})
}

func TestReleaseIsConcurrentAndIdempotent(t *testing.T) {
	t.Parallel()
	s, err := semaphore.New(3)
	if err != nil {
		t.Fatal(err)
	}
	release, ok := s.TryAcquire(3)
	if !ok {
		t.Fatal("initial acquisition failed")
	}
	if _, ok := s.TryAcquire(1); ok {
		t.Fatal("over-capacity acquisition succeeded")
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(release)
	}
	wg.Wait()
	all, ok := s.TryAcquire(3)
	if !ok {
		t.Fatal("release did not restore capacity")
	}
	defer all()
	if _, ok := s.TryAcquire(1); ok {
		t.Fatal("duplicate release inflated capacity")
	}
}

func TestAcquireCancellationRacingWithAdmissionDoesNotLeak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := semaphore.New(1)
		if err != nil {
			t.Fatal(err)
		}
		release, err := s.Acquire(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		var wg sync.WaitGroup
		var cancels []context.CancelFunc
		for range 32 {
			ctx, cancel := context.WithCancel(t.Context())
			cancels = append(cancels, cancel)
			wg.Go(func() {
				r, err := s.Acquire(ctx, 1)
				if err != nil {
					if !errors.Is(err, context.Canceled) || r != nil {
						t.Errorf("canceled acquisition = %v", err)
					}
					return
				}
				r()
			})
		}
		synctest.Wait()
		release()
		for _, cancel := range cancels {
			cancel()
		}
		wg.Wait()
		all, ok := s.TryAcquire(1)
		if !ok {
			t.Fatal("admission/cancellation race leaked capacity")
		}
		all()
	})
}

func TestAcquireBoundsConcurrentWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := semaphore.New(7)
		if err != nil {
			t.Fatal(err)
		}
		finish := make(chan struct{})
		started := make(chan struct{}, 64)
		var active atomic.Int64
		var wg sync.WaitGroup
		for range 64 {
			wg.Go(func() {
				release, err := s.Acquire(t.Context(), 2)
				if err != nil {
					t.Error(err)
					return
				}
				if n := active.Add(2); n > 7 {
					t.Errorf("active weight = %d", n)
				}
				started <- struct{}{}
				<-finish
				active.Add(-2)
				release()
			})
		}
		synctest.Wait()
		if n := active.Load(); n != 6 {
			t.Errorf("saturated weight = %d, want 6", n)
		}
		if n := len(started); n != 3 {
			t.Errorf("admitted workers = %d, want 3", n)
		}
		close(finish)
		wg.Wait()
		all, ok := s.TryAcquire(7)
		if !ok {
			t.Fatal("work leaked capacity")
		}
		all()
	})
}

func ExampleSemaphore_Acquire() {
	s, err := semaphore.New(4)
	if err != nil {
		panic(err)
	}
	release, err := s.Acquire(context.Background(), 2)
	if err != nil {
		panic(err)
	}
	defer release()
	fmt.Println("two units acquired")
	// Output: two units acquired
}
