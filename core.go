package blades

import (
	"context"
	"iter"
)

// Sequence returns an iterator over the messages in the prompt.
type Sequence[T any] = iter.Seq2[T, error]

// Agent represents an autonomous agent that can perform tasks based on invocations.
type Agent interface {
	Name() string
	Description() string
	Run(context.Context, *Invocation) Sequence[*Message]
}

// AgentContext holds information about the agent handling the request.
type AgentContext interface {
	Name() string
	Model() string
	Description() string
	Instructions() string
}
