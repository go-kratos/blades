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
	Append(context.Context, *Message) error
	History(ctx context.Context) ([]*Message, error)
	// Context returns the compressed message history prepared for the next model
	// call by applying the configured ContextCompressor to History(). When no
	// ContextCompressor is set, the raw History() is returned unchanged.
	Context(ctx context.Context) ([]*Message, error)
}

// SessionOption configures a Session at construction time.
type SessionOption func(*sessionInMemory)

// WithContextCompressor sets the ContextCompressor used by Session.Context to
// compress the message history returned by History() before each model call.
func WithContextCompressor(c ContextCompressor) SessionOption {
	return func(s *sessionInMemory) {
		s.compressor = c
	}
}

// NewSession creates a new Session instance with an auto-generated UUID.
// Pass SessionOption values to configure the session (e.g. WithContextCompressor).
// Legacy map arguments are still accepted for backwards compatibility.
func NewSession(opts ...SessionOption) Session {
	session := &sessionInMemory{id: uuid.NewString()}
	for _, opt := range opts {
		opt(session)
	}
	return session
}

type sessionWithCompressor struct {
	Session
	compressor ContextCompressor
}

func (s *sessionWithCompressor) Context(ctx context.Context) ([]*Message, error) {
	messages, err := s.Session.History(ctx)
	if err != nil {
		return nil, err
	}
	return s.compressor.Compress(ctx, messages)
}

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
	id         string
	state      maps.Map[string, any]
	history    slices.Slice[*Message]
	compressor ContextCompressor
}

func (s *sessionInMemory) ID() string {
	return s.id
}
func (s *sessionInMemory) State() State {
	return s.state.ToMap()
}
func (s *sessionInMemory) History(_ context.Context) ([]*Message, error) {
	return s.history.ToSlice(), nil
}
func (s *sessionInMemory) SetState(key string, value any) {
	s.state.Store(key, value)
}
func (s *sessionInMemory) Append(ctx context.Context, message *Message) error {
	s.history.Append(message)
	return nil
}

func (s *sessionInMemory) Context(ctx context.Context) ([]*Message, error) {
	messages, err := s.History(ctx)
	if err != nil {
		return nil, err
	}
	if s.compressor == nil {
		return messages, nil
	}
	return s.compressor.Compress(ctx, messages)
}
