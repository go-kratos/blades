package blades

import (
	"context"

	"github.com/go-kratos/generics"
	"github.com/google/uuid"
)

// SessionStore defines the interface for session storage backends.
type SessionStore interface {
	GetSession(ctx context.Context, id string) (*Session, error)
	SaveSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, id string) error
}

// Session holds the state of a flow along with a unique session ID.
type Session struct {
	ID      string                   `json:"id"`
	History generics.Slice[*Message] `json:"history"`
	State   State                    `json:"state"`
}

// PutState sets a key-value pair in the session state.
func (s *Session) PutState(key string, value any) {
	s.State.Store(key, value)
}

// Record records the input prompt and output message under the given name.
func (s *Session) Record(input []*Message, output *Message) {
	messages := make([]*Message, 0, len(input)+1)
	messages = append(messages, input...)
	messages = append(messages, output)
	s.History.Append(messages...)
}

// NewSession creates a new Session instance with a unique ID.
func NewSession(states ...map[string]any) *Session {
	session := &Session{ID: uuid.NewString()}
	for _, state := range states {
		for k, v := range state {
			session.PutState(k, v)
		}
	}
	return session
}

// ctxSessionKey is an unexported type for keys defined in this package.
type ctxSessionKey struct{}

// NewSessionContext returns a new Context that carries value.
func NewSessionContext(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, ctxSessionKey{}, session)
}

// FromSessionContext retrieves the SessionContext from the context.
func FromSessionContext(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(ctxSessionKey{}).(*Session)
	return session, ok
}

// EnsureSession retrieves the SessionContext from the context, or creates a new one if it doesn't exist.
func EnsureSession(ctx context.Context) (*Session, context.Context) {
	session, ok := FromSessionContext(ctx)
	if !ok {
		session = NewSession()
		ctx = NewSessionContext(ctx, session)
	}
	return session, ctx
}
