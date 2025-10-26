package blades

import "errors"

var (
    // ErrMaxIterationsExceeded is returned when an agent exceeds the maximum allowed iterations.
    ErrMaxIterationsExceeded = errors.New("maximum iterations exceeded in agent execution")
    // ErrMissingFinalResponse is returned when an agent's stream ends without a final response.
    ErrMissingFinalResponse = errors.New("stream ended without a final response")
    // ErrConfirmationDenied is returned when confirmation middleware denies execution.
    ErrConfirmationDenied = errors.New("confirmation denied")
)
