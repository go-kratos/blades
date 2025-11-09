package blades

import (
	"context"
	"iter"
)

// Sequence is an alias for iter.Seq2 to represent a sequence of items of type T or an error of type E.
type Sequence[T, E any] = iter.Seq2[T, E]

// Agent represents an autonomous agent that can process invocations and produce a sequence of messages.
type Agent interface {
	Name() string
	Description() string
	Run(context.Context, *Invocation) Sequence[*Message, error]
}

// AgentContext holds information about the agent handling the request.
type AgentContext interface {
	Name() string
	Model() string
	Description() string
	Instructions() string
}
