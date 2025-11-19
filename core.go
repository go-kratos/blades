package blades

import (
	"context"
	"iter"

	"github.com/go-kratos/blades/tools"
	"github.com/google/uuid"
)

// Invocation holds information about the current invocation.
type Invocation struct {
	ID          string
	Model       string
	Session     Session
	Resumable   bool
	Streamable  bool
	Instruction *Message
	Message     *Message
	History     []*Message
	Tools       []tools.Tool
}

// Generator is a generic type representing a sequence generator that yields values of type T or errors of type E.
type Generator[T, E any] iter.Seq2[T, E]

// Agent represents an autonomous agent that can process invocations and produce a sequence of messages.
type Agent interface {
	// Name returns the name of the agent.
	Name() string
	// Description returns a brief description of the agent's functionality.
	Description() string
	// Run processes the given invocation and returns a generator that yields messages or errors.
	Run(context.Context, *Invocation) Generator[*Message, error]
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
