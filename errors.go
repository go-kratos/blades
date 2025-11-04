package blades

import "errors"

var (
	// ErrMaxIterationsExceeded is returned when an agent exceeds the maximum allowed iterations.
	ErrMaxIterationsExceeded = errors.New("maximum iterations exceeded in agent execution")
	// ErrMissingFinalResponse is returned when an agent's stream ends without a final response.
	ErrMissingFinalResponse = errors.New("stream ended without a final response")
	// ErrConfirmDenied is returned when confirmation middleware denies execution.
	ErrConfirmDenied = errors.New("confirmation denied")
)
