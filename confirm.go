package blades

import (
	"context"

	"github.com/go-kratos/blades/stream"
)

// ConfirmFunc is a callback used by the confirmation middleware
// to decide whether a prompt should proceed. It returns true to allow
// execution, false to deny, and may return an error to abort.
type ConfirmFunc func(context.Context, *Prompt) (bool, error)

// Confirm returns a Middleware that invokes the provided confirmation
// callback before delegating to the next Runnable. If confirmation is
// denied, it returns ErrConfirmDenied. If the callback returns an
// error, that error is propagated.
func Confirm(confirm ConfirmFunc) Middleware {
	return func(next Runnable) Runnable {
		return &confirmMiddleware{next: next, confirm: confirm}
	}
}

type confirmMiddleware struct {
	next    Runnable
	confirm ConfirmFunc
}

func (m *confirmMiddleware) Run(ctx context.Context, p *Prompt, opts ...ModelOption) (*Message, error) {
	ok, err := m.confirm(ctx, p)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrConfirmDenied
	}
	return m.next.Run(ctx, p, opts...)
}

func (m *confirmMiddleware) RunStream(ctx context.Context, p *Prompt, opts ...ModelOption) stream.Streamable[*Message] {
	return func(yield func(*Message, error) bool) {
		ok, err := m.confirm(ctx, p)
		if err != nil {
			yield(nil, err)
			return
		}
		if !ok {
			yield(nil, ErrConfirmDenied)
			return
		}
		for msg, err := range m.next.RunStream(ctx, p, opts...) {
			yield(msg, err)
		}
	}
}
