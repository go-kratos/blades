package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Transition represents a change between two states.
type Transition struct {
	From string
	To   string
}

// TransitionHandler defines a function that handles a state transition in a flow.
// I represents the input type, O represents the output type.
type TransitionHandler func(ctx context.Context, trans Transition, output *blades.Message) (*blades.Prompt, error)
