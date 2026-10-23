// Package cleanup manages explicitly owned resources in reverse acquisition order.
package cleanup

import "errors"

// Stack records cleanup functions. Its zero value is ready for use, and it may
// be reused after Close. Stack is intended for a single owner and is not safe
// for concurrent use. It must not be copied after first use. Callbacks must not
// modify or reenter the same Stack.
type Stack struct {
	callbacks []func() error
}

// Add transfers cleanup responsibility for fn to the stack. A nil fn is ignored.
// Register a method value such as file.Close, or a closure for other resources.
func (s *Stack) Add(fn func() error) {
	if fn != nil {
		s.callbacks = append(s.callbacks, fn)
	}
}

// Close calls all pending functions in reverse order and joins their errors.
// Each function is removed before invocation and called at most once. Closing an
// empty stack returns nil; prior errors are not retained across Close calls.
// Callbacks run synchronously, so Close cannot interrupt a blocked callback.
//
// If a callback panics or calls runtime.Goexit, the remaining callbacks still run
// during unwinding. Their errors are then discarded, and ordinary panic/Goexit
// semantics apply, including replacement by a later cleanup panic.
func (s *Stack) Close() error {
	defer func() {
		if len(s.callbacks) != 0 {
			// Also drain the remaining resources while unwinding panic or Goexit.
			_ = s.Close()
		}
	}()
	var errs []error
	for len(s.callbacks) != 0 {
		i := len(s.callbacks) - 1
		fn := s.callbacks[i]
		s.callbacks[i] = nil
		s.callbacks = s.callbacks[:i]
		if err := fn(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
