package cleanup_test

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/nostosx/kitx/cleanup"
)

func TestStackCloseReverseOrderAndJoinedErrors(t *testing.T) {
	t.Parallel()
	var stack cleanup.Stack
	first, second := errors.New("first"), errors.New("second")
	var order []int
	stack.Add(func() error { order = append(order, 1); return first })
	stack.Add(nil)
	stack.Add(func() error { order = append(order, 2); return nil })
	stack.Add(func() error { order = append(order, 3); return second })
	err := stack.Close()
	if !errors.Is(err, first) || !errors.Is(err, second) || !slices.Equal(order, []int{3, 2, 1}) {
		t.Fatalf("Close = %v, order %v", err, order)
	}
	if err := stack.Close(); err != nil || len(order) != 3 {
		t.Fatalf("second Close = %v, order %v", err, order)
	}
	stack.Add(func() error { order = append(order, 4); return nil })
	if err := stack.Close(); err != nil || !slices.Equal(order, []int{3, 2, 1, 4}) {
		t.Fatalf("reused Close = %v, order %v", err, order)
	}
}

func TestStackCloseDrainsOnPanic(t *testing.T) {
	t.Parallel()
	var stack cleanup.Stack
	var order []int
	stack.Add(func() error { order = append(order, 1); return errors.New("cleanup failed") })
	stack.Add(func() error { order = append(order, 2); panic("inner") })
	stack.Add(func() error { order = append(order, 3); panic("outer") })
	var value any
	func() {
		defer func() { value = recover() }()
		_ = stack.Close()
	}()
	if value != "inner" || !slices.Equal(order, []int{3, 2, 1}) {
		t.Fatalf("panic = %v, order %v", value, order)
	}
	if err := stack.Close(); err != nil || len(order) != 3 {
		t.Fatalf("Close after panic = %v, order %v", err, order)
	}
}

func TestStackCloseDrainsOnGoexit(t *testing.T) {
	t.Parallel()
	var order []int
	done := make(chan struct{})
	go func() {
		defer close(done)
		var stack cleanup.Stack
		stack.Add(func() error { order = append(order, 1); return nil })
		stack.Add(func() error { order = append(order, 2); runtime.Goexit(); return nil })
		stack.Add(func() error { order = append(order, 3); return nil })
		_ = stack.Close()
		t.Error("Goexit returned")
	}()
	<-done
	if !slices.Equal(order, []int{3, 2, 1}) {
		t.Fatal(order)
	}
}

func ExampleStack() {
	var resources cleanup.Stack
	resources.Add(func() error { fmt.Println("close database"); return nil })
	resources.Add(func() error { fmt.Println("close client"); return nil })
	fmt.Println(resources.Close())
	// Output:
	// close client
	// close database
	// <nil>
}
