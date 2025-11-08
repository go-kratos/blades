package blades

import (
	"errors"
)

var (
	// ErrNoAgentContext is returned when an agent context is missing from the context.
	ErrNoAgentContext = errors.New("no agent context")
	// ErrMissingInvocationContext is returned when an invocation context is missing from the context.
	ErrNoInvocationContext = errors.New("no invocation context")
	// ErrMaxIterationsExceeded is returned when an agent exceeds the maximum allowed iterations.
	ErrMaxIterationsExceeded = errors.New("maximum iterations exceeded in agent execution")
	// ErrMissingFinalResponse is returned when an agent's stream ends without a final response.
	ErrNoFinalResponse = errors.New("stream ended without a final response")
	// ErrConfirmDenied is returned when confirmation middleware denies execution.
	ErrConfirmDenied = errors.New("confirmation denied")
)
