package blades

import (
	"context"

	"github.com/go-kratos/kit/container/maps"
	"github.com/go-kratos/kit/container/slices"
	"github.com/google/uuid"
)

// Session holds the state of a flow along with a unique session ID.
type Session interface {
	ID() string
	State() State
	SetState(string, any)
	History() []*Message
	Append(context.Context, *Message) error
}

// NewSession creates a new Session instance with an auto-generated UUID and optional initial state maps.
func NewSession(states ...map[string]any) Session {
	session := &sessionInMemory{id: uuid.NewString()}
	for _, state := range states {
		for k, v := range state {
			session.SetState(k, v)
		}
	}
	return session
}

// ctxSessionKey is an unexported type for keys defined in this package.
type ctxSessionKey struct{}

// NewSessionContext returns a new Context that carries the session value.
func NewSessionContext(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, ctxSessionKey{}, session)
}

// FromSessionContext retrieves the SessionContext from the context.
func FromSessionContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(ctxSessionKey{}).(Session)
	return session, ok
}

// sessionInMemory is an in-memory implementation of the Session interface.
type sessionInMemory struct {
	id      string
	state   maps.Map[string, any]
	history slices.Slice[*Message]
}

func (s *sessionInMemory) ID() string {
	return s.id
}
func (s *sessionInMemory) State() State {
	return s.state.ToMap()
}
func (s *sessionInMemory) History() []*Message {
	return s.history.ToSlice()
}
func (s *sessionInMemory) SetState(key string, value any) {
	s.state.Store(key, value)
}
func (s *sessionInMemory) Append(ctx context.Context, message *Message) error {
	s.history.Append(message)
	return nil
}
