package blades

import "errors"

var (
	// ErrMaxIterationsExceeded is returned when an agent exceeds the maximum allowed iterations.
	ErrMaxIterationsExceeded = errors.New("maximum iterations exceeded in agent execution")
)
