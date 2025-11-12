package blades

import (
	"errors"
)

var (
	// ErrNoSessionContext is returned when a session context is missing from the context.
	ErrNoSessionContext = errors.New("session not found in context")
	// ErrNoAgentContext is returned when an agent context is missing from the context.
	ErrNoAgentContext = errors.New("agent not found in context")
	// ErrMissingInvocationContext is returned when an invocation context is missing from the context.
	ErrNoInvocationContext = errors.New("invocation not found in context")
	// ErrModelProviderRequired is returned when a model provider is not supplied where required.
	ErrModelProviderRequired = errors.New("model provider is required")
	// ErrMaxIterationsExceeded is returned when an agent exceeds the maximum allowed iterations.
	ErrMaxIterationsExceeded = errors.New("maximum iterations exceeded in agent execution")
	// ErrMissingFinalResponse is returned when an agent's stream ends without a final response.
	ErrNoFinalResponse = errors.New("stream ended without a final response")
)
