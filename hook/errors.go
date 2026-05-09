package hook

import "errors"

// ErrAbort is the sentinel error for hook-initiated abort.
var ErrAbort = errors.New("hook: abort")

// AbortError carries a reason for the abort.
type AbortError struct {
	Reason string
}

func (e *AbortError) Error() string {
	return "hook: abort: " + e.Reason
}

func (e *AbortError) Unwrap() error {
	return ErrAbort
}

// Abort creates an AbortError with the given reason.
func Abort(reason string) error {
	return &AbortError{Reason: reason}
}

// IsAbort checks if an error is an abort signal.
func IsAbort(err error) bool {
	return errors.Is(err, ErrAbort)
}
