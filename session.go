package blades

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"
)

// SessionStore defines the interface for session storage backends.
type SessionStore interface {
	Get(ctx context.Context, id string) (*Session, error)
	Save(ctx context.Context, session *Session) error
	Delete(ctx context.Context, id string) error
}

// Session holds the state of a flow along with a unique session ID.
type Session interface {
	ID() string
	State() State
	History() []*Message
	Append(context.Context, State, []*Message) error
}

// NewSession creates a new Session instance with the provided ID.
func NewSession(id string, states ...map[string]any) Session {
	session := &sessionInMemory{id: id, state: State{}}
	for _, state := range states {
		for k, v := range state {
			session.state[k] = v
		}
	}
	return session
}

// ctxSessionKey is an unexported type for keys defined in this package.
type ctxSessionKey struct{}

// NewSessionContext returns a new Context that carries value.
func NewSessionContext(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, ctxSessionKey{}, session)
}

// FromSessionContext retrieves the SessionContext from the context.
func FromSessionContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(ctxSessionKey{}).(Session)
	return session, ok
}

// EnsureSession retrieves the SessionContext from the context, or creates a new one if it doesn't exist.
func EnsureSession(ctx context.Context) (Session, context.Context) {
	session, ok := FromSessionContext(ctx)
	if !ok {
		session = NewSession(uuid.NewString())
		ctx = NewSessionContext(ctx, session)
	}
	return session, ctx
}

// sessionInMemory is an in-memory implementation of the Session interface.
type sessionInMemory struct {
	id      string
	state   State
	history []*Message
	m       sync.RWMutex
}

func (s *sessionInMemory) ID() string {
	return s.id
}
func (s *sessionInMemory) State() State {
	s.m.RLock()
	defer s.m.RUnlock()
	return s.state.Clone()
}
func (s *sessionInMemory) History() []*Message {
	s.m.RLock()
	defer s.m.RUnlock()
	return slices.Clone(s.history)
}
func (s *sessionInMemory) Append(ctx context.Context, state State, history []*Message) error {
	s.m.Lock()
	defer s.m.Unlock()
	for k, v := range state {
		s.state[k] = v
	}
	s.history = append(s.history, history...)
	return nil
}
