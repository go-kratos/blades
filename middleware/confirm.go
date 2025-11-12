package middleware

import (
	"context"
	"errors"

	"github.com/go-kratos/blades"
)

var (
	// ErrConfirmDenied is returned when confirmation middleware denies execution.
	ErrConfirmDenied = errors.New("confirmation denied")
)

// ConfirmFunc is a callback used by the confirmation middleware
// to decide whether a prompt should proceed. It returns true to allow
// execution, false to deny, and may return an error to abort.
type ConfirmFunc func(context.Context, *blades.Message) (bool, error)

// Confirm returns a Middleware that invokes the provided confirmation
// callback before delegating to the next Handler. If confirmation is
// denied, it returns ErrConfirmDenied. If the callback returns an
// error, that error is propagated.
func Confirm(confirm ConfirmFunc) blades.Middleware {
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
			return func(yield func(*blades.Message, error) bool) {
				ok, err := confirm(ctx, invocation.Message)
				if err != nil {
					yield(nil, err)
					return
				}
				if !ok {
					yield(nil, ErrConfirmDenied)
					return
				}
				for msg, err := range next.Handle(ctx, invocation) {
					if !yield(msg, err) {
						break
					}
				}
			}
		})
	}
}
