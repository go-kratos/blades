package blades

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"
)

// Session holds the state of a flow along with a unique session ID.
type Session interface {
	ID() string
	State() State
	History() []*Message
	PutState(string, any)
	Append(context.Context, []*Message) error
}

// NewSession creates a new Session instance with an auto-generated UUID and optional initial state maps.
func NewSession(states ...map[string]any) Session {
	session := &sessionInMemory{id: uuid.NewString(), state: State{}}
	for _, state := range states {
		for k, v := range state {
			session.state[k] = v
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
func (s *sessionInMemory) PutState(key string, value any) {
	s.m.Lock()
	defer s.m.Unlock()
	s.state[key] = value
}
func (s *sessionInMemory) Append(ctx context.Context, history []*Message) error {
	s.m.Lock()
	defer s.m.Unlock()
	s.history = append(s.history, history...)
	return nil
}
