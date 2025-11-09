package blades

import (
	"github.com/google/uuid"
)

// Invocation holds information about the current invocation.
type Invocation struct {
	ID           string
	Session      Session
	Resumable    bool
	Streamable   bool
	Message      *Message
	ModelOptions []ModelOption
}

// Clone creates a deep copy of the Invocation.
func (i *Invocation) CloneWithMessage(message *Message) *Invocation {
	clone := *i
	clone.Message = message
	return &clone
}

// NewInvocationID generates a new unique invocation ID.
func NewInvocationID() string {
	return uuid.NewString()
}
