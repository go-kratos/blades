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
	// Context returns the message list prepared for the next model call by
	// applying the configured Compressor to messages. Returns nil when no
	// Compressor is set, signalling the caller to use messages unchanged.
	Context(ctx context.Context, messages []*Message) ([]*Message, error)
}

// SessionOption configures a Session at construction time.
type SessionOption func(*sessionInMemory)

// WithCompressor sets the Compressor used by Session.Context to compress the
// message history before each model call.
func WithCompressor(c Compressor) SessionOption {
	return func(s *sessionInMemory) {
		s.compressor = c
	}
}

// NewSession creates a new Session instance with an auto-generated UUID.
// Pass SessionOption values to configure the session (e.g. WithCompressor).
// Legacy map arguments are still accepted for backwards compatibility.
func NewSession(opts ...SessionOption) Session {
	session := &sessionInMemory{id: uuid.NewString()}
	for _, opt := range opts {
		opt(session)
	}
	return session
}

// NewSessionWithCompressor returns a Session that delegates all operations to s
// but overrides Context to compress using c. This allows per-agent compressor
// overrides without mutating the shared session (e.g. in recipe flows).
func NewSessionWithCompressor(s Session, c Compressor) Session {
	return &sessionWithCompressor{Session: s, compressor: c}
}

type sessionWithCompressor struct {
	Session
	compressor Compressor
}

func (s *sessionWithCompressor) Context(ctx context.Context, messages []*Message) ([]*Message, error) {
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
	compressor Compressor
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

func (s *sessionInMemory) Context(ctx context.Context, messages []*Message) ([]*Message, error) {
	if s.compressor == nil {
		return nil, nil
	}
	return s.compressor.Compress(ctx, messages)
}
