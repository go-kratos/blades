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
	Append(context.Context, State, []*Message) error
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

// FromSessionContext retrieves the SessionContext from the context.
func FromSessionContext(ctx context.Context) (Session, bool) {
	invocation, ok := FromInvocationContext(ctx)
	if !ok {
		return nil, false
	}
	return invocation.Session, true
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
