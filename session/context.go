package session

import "context"

type contextKey struct{}

// NewContext stores a Session in the context.
func NewContext(ctx context.Context, sess Session) context.Context {
	return context.WithValue(ctx, contextKey{}, sess)
}

// FromContext retrieves the Session from the context.
func FromContext(ctx context.Context) (Session, bool) {
	sess, ok := ctx.Value(contextKey{}).(Session)
	return sess, ok
}

// Ensure returns the Session from context, creating an in-memory one if absent.
func Ensure(ctx context.Context) Session {
	if sess, ok := FromContext(ctx); ok {
		return sess
	}
	return NewSession()
}
