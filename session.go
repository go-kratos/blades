package blades

import (
	"context"

	"github.com/go-kratos/generics"
	"github.com/google/uuid"
)

// Task represents a single input-output pair within a session.
type Task struct {
	Input  *Prompt  `json:"input"`
	Output *Message `json:"output"`
}

// Session holds the state of a flow along with a unique session ID.
type Session struct {
	ID    string                     `json:"id"`   // Unique identifier for the session
	Task  generics.Map[string, Task] `json:"task"` // Agent tasks within the session
	State State                      `json:"state"`
}

// Record records the input prompt and output message under the given name.
func (s *Session) Record(name string, input *Prompt, output *Message) {
	s.Task.Store(name, Task{Input: input, Output: output})
}

// NewSession creates a new Session instance with a unique ID.
func NewSession(id string) *Session {
	return &Session{ID: id}
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
		session = NewSession(uuid.NewString())
		ctx = NewSessionContext(ctx, session)
	}
	return session, ctx
}
