package blades

import (
	"context"
	"iter"
	"slices"

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
	// appended to the session. Clone transfers this responsibility to the first
	// clone so that only one agent in a multi-agent tree performs the append.
	committed bool
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
// Commit responsibility is transferred to the first clone: the first call sets
// the original's committed to true and returns a clone with committed false
// (this clone will perform the append). All subsequent clones inherit
// committed true and skip the append.
// Callers that spawn clones concurrently (e.g. parallel agents) must call
// Clone before launching goroutines to avoid a data race on committed.
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
