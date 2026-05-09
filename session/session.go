package session

import (
	"context"
	"sync"

	"github.com/go-kratos/blades/model"
	"github.com/google/uuid"
)

// Session manages conversation message history.
type Session interface {
	ID() string
	Metadata() map[string]any
	State() map[string]any
	SetState(key string, value any)
	Append(ctx context.Context, msgs ...*model.Message) error
	Messages(ctx context.Context) ([]*model.Message, error)
}

// SessionOption configures a new Session.
type SessionOption func(*inMemorySession)

// WithSessionID sets the session ID.
func WithSessionID(id string) SessionOption {
	return func(s *inMemorySession) {
		s.id = id
	}
}

// WithMessages pre-populates the session with messages.
func WithMessages(msgs ...*model.Message) SessionOption {
	return func(s *inMemorySession) {
		s.messages = append(s.messages, msgs...)
	}
}

// WithMetadata sets the initial metadata.
func WithMetadata(metadata map[string]any) SessionOption {
	return func(s *inMemorySession) {
		for k, v := range metadata {
			s.metadata[k] = v
		}
	}
}

// WithState sets the initial state.
func WithState(state map[string]any) SessionOption {
	return func(s *inMemorySession) {
		for k, v := range state {
			s.state[k] = v
		}
	}
}

// NewSession creates a new in-memory session.
func NewSession(opts ...SessionOption) Session {
	s := &inMemorySession{
		id:       uuid.NewString(),
		state:    make(map[string]any),
		metadata: make(map[string]any),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type inMemorySession struct {
	mu       sync.RWMutex
	id       string
	metadata map[string]any
	state    map[string]any
	messages []*model.Message
}

func (s *inMemorySession) ID() string {
	return s.id
}

func (s *inMemorySession) Metadata() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]any, len(s.metadata))
	for k, v := range s.metadata {
		cp[k] = v
	}
	return cp
}

func (s *inMemorySession) State() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]any, len(s.state))
	for k, v := range s.state {
		cp[k] = v
	}
	return cp
}

func (s *inMemorySession) SetState(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
}

func (s *inMemorySession) Append(_ context.Context, msgs ...*model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msgs...)
	return nil
}

func (s *inMemorySession) Messages(_ context.Context) ([]*model.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]*model.Message, len(s.messages))
	copy(cp, s.messages)
	return cp, nil
}
