package flow

import "context"

// Transition represents a state transition.
type Transition struct {
	Previous string
	Current  string
}

// TransitionHandler is a function that handles the transition of a flow.
type TransitionHandler[I, O any] func(ctx context.Context, transition Transition, output O) (I, error)
