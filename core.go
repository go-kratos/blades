package blades

import (
	"context"
	"iter"

	"github.com/google/uuid"
)

// Generator is an alias for iter.Seq2 to represent a sequence of items of type T or an error of type E.
type Generator[T, E any] iter.Seq2[T, E]

// Invocation holds information about the current invocation.
type Invocation struct {
	ID           string
	Session      Session
	Resumable    bool
	Streamable   bool
	Message      *Message
	ModelOptions []ModelOption
}

// Agent represents an autonomous agent that can process invocations and produce a sequence of messages.
type Agent interface {
	Name() string
	Description() string
	Run(context.Context, *Invocation) Generator[*Message, error]
}

// Runner represents a component that can execute a single message and return a response message or a stream of messages.
type Runner interface {
	Run(context.Context, *Message, ...ModelOption) (*Message, error)
	RunStream(context.Context, *Message, ...ModelOption) Generator[*Message, error]
}

// NewInvocationID generates a new unique invocation ID.
func NewInvocationID() string {
	return uuid.NewString()
}

// Clone creates a deep copy of the Invocation.
func (i *Invocation) Clone() *Invocation {
	clone := *i
	return &clone
}
