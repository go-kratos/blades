package blades

import (
	"context"
	"iter"
	"slices"
	"sync/atomic"

	"github.com/go-kratos/blades/tools"
	"github.com/google/uuid"
)

// Invocation holds information about the current invocation.
type Invocation struct {
	ID          string
	Model       string
	Resume      bool
	Stream      bool
	Session     Session
	Instruction *Message
	Message     *Message
	Tools       []tools.Tool
	// committed tracks whether the initial user message has been (or will be)
	// appended to the session. All clones share the same *atomic.Bool pointer,
	// so CompareAndSwap guarantees exactly-once append even under concurrent
	// goroutines. Initialized by prepareInvocation when nil.
	committed *atomic.Bool
}

// Generator is a generic type representing a sequence generator that yields values of type T or errors of type E.
type Generator[T, E any] = iter.Seq2[T, E]

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
// All clones share the same committed *atomic.Bool pointer. The first agent
// (original or any clone) to call CompareAndSwap(false, true) on it will
// perform the session append; all others skip. This is safe for concurrent use.
func (inv *Invocation) Clone() *Invocation {
	return &Invocation{
		ID:          inv.ID,
		Model:       inv.Model,
		Session:     inv.Session,
		Resume:      inv.Resume,
		Stream:      inv.Stream,
		Message:     inv.Message.Clone(),
		Instruction: inv.Instruction.Clone(),
		committed:   inv.committed,
		Tools:       slices.Clone(inv.Tools),
	}
}
