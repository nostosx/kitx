package parallel

import (
	"context"
	"iter"
	"slices"
	"sync"
)

// ForEach runs fn for each input with at most limit concurrent callbacks and
// without collecting results. It has the cancellation, joining, ownership and
// panic rules of Map. Callback order is unspecified. Empty inputs do no work.
func ForEach[T any](ctx context.Context, inputs []T, limit int, fn func(context.Context, T) error) error {
	if limit <= 0 || fn == nil {
		return ErrInvalidArgument
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if len(inputs) == 0 {
		return nil
	}
	return ForEachSeq(ctx, func(context.Context) iter.Seq[T] {
		return slices.Values(inputs)
	}, min(limit, len(inputs)), fn)
}

// ForEachSeq calls source once with the same child context used by workers,
// then consumes its sequence in the caller's goroutine and processes values
// with at most limit workers. An unbuffered handoff applies backpressure; no
// slice of all inputs or outputs is built. At most one yielded value can be
// waiting for a worker. A nil source, returned sequence or callback, or
// non-positive limit, returns ErrInvalidArgument.
//
// The source and iterator must use the supplied child context for blocking work;
// it is canceled on worker failure, parent cancellation and return. The iterator
// must stop when yield returns false. ForEachSeq cannot interrupt code that
// ignores cancellation. All workers are joined, including when the iterator
// panics or calls Goexit. Callback panics retain ordinary goroutine semantics.
// Callback Goexit cancels the operation with ErrGoexit. The first cause wins.
// Callback order is unspecified; ownership rules match Map.
func ForEachSeq[T any](ctx context.Context, source func(context.Context) iter.Seq[T], limit int, fn func(context.Context, T) error) error {
	if source == nil || fn == nil || limit <= 0 {
		return ErrInvalidArgument
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	jobs := make(chan T)
	hasClosed := false
	var workers sync.WaitGroup
	defer func() {
		cancel(nil)
		if !hasClosed {
			close(jobs)
		}
		workers.Wait()
	}()
	inputs := source(ctx)
	if inputs == nil {
		return ErrInvalidArgument
	}
	for range limit {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case value, ok := <-jobs:
					if !ok || ctx.Err() != nil {
						return
					}
					_, err := invoke(ctx, value, func(ctx context.Context, value T) (struct{}, error) {
						return struct{}{}, fn(ctx, value)
					}, cancel)
					if err != nil {
						cancel(err)
						return
					}
				}
			}
		})
	}
	inputs(func(value T) bool {
		if ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case jobs <- value:
			return ctx.Err() == nil
		}
	})
	// Closing before joining lets workers finish normally on successful exhaustion.
	close(jobs)
	hasClosed = true
	workers.Wait()
	return context.Cause(ctx)
}
