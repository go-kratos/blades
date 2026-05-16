package blades

import "errors"

var (
	ErrModelProviderRequired       = errors.New("blades: model provider is required")
	ErrNoToolsConfigured           = errors.New("blades: no tools configured")
	ErrMaxStepsExceeded            = errors.New("blades: maximum steps exceeded")
	ErrAgentNotStarted             = errors.New("blades: agent failed to start")
	ErrNoResult                    = errors.New("blades: no result")
	ErrAgentNotForkable            = errors.New("blades: agent is not forkable")
	ErrContextTokenCounterRequired = errors.New("blades: context token counter is required for enforced context budget")
)
