package blades

import (
	"context"
	"iter"

	"github.com/google/uuid"
)

// Invocation holds information about the current invocation.
type Invocation struct {
	ID         string
	Session    Session
	Resumable  bool
	Streamable bool
	Message    *Message
	History    []*Message
}

// Generator is a generic type representing a sequence generator that yields values of type T or errors of type E.
type Generator[T, E any] iter.Seq2[T, E]

// Agent represents an autonomous agent that can process invocations and produce a sequence of messages.
type Agent interface {
	Name() string
	Description() string
	Run(context.Context, *Invocation) Generator[*Message, error]
}

// Runner represents a component that can execute a single message and return a response message or a stream of messages.
type Runner interface {
	Run(context.Context, *Message) (*Message, error)
	RunStream(context.Context, *Message) Generator[*Message, error]
}

// NewInvocationID generates a new unique invocation ID.
func NewInvocationID() string {
	return uuid.NewString()
}

// Clone creates a deep copy of the Invocation.
func (inv *Invocation) Clone() *Invocation {
	historyCopy := make([]*Message, len(inv.History))
	copy(historyCopy, inv.History)
	return &Invocation{
		ID:         inv.ID,
		Session:    inv.Session,
		Resumable:  inv.Resumable,
		Streamable: inv.Streamable,
		Message:    inv.Message,
		History:    historyCopy,
	}
}
