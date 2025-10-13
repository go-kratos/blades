package flow

import "context"

// Transition represents a change between two states.
type Transition struct {
	From string
	To   string
}

// TransitionHandler defines a function that handles a state transition in a flow.
// I represents the input type, O represents the output type.
type TransitionHandler[I, O any] func(ctx context.Context, trans Transition, output O) (I, error)
