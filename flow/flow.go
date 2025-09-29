package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// FlowOption is a function that configures a Flow.
type FlowOption[I, O any] func(f *Flow[I, O])

// WithUserMessage sets the user message for the flow.
func WithUserMessage[I, O any](msg string) FlowOption[I, O] {
	return func(f *Flow[I, O]) {
		f.userMessage = msg
	}
}

// WithSystemMessage sets the system message for the flow.
func WithSystemMessage[I, O any](msg string) FlowOption[I, O] {
	return func(f *Flow[I, O]) {
		f.systemMessage = msg
	}
}

// Flow represents a sequence of operations that process input of type I and produce output of type O.
type Flow[I, O any] struct {
	runner        *blades.OutputConverter[O]
	userMessage   string
	systemMessage string
}

// NewFlow creates a new Flow with the given runner and options.
func NewFlow[I, O any](runner blades.Runner, opts ...FlowOption[I, O]) *Flow[I, O] {
	f := &Flow[I, O]{
		runner: blades.NewOutputConverter[O](runner),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Run executes the flow with the given input and options, returning the output or an error.
func (f *Flow[I, O]) Run(ctx context.Context, input I, opts ...blades.ModelOption) (o O, err error) {
	prompt, err := blades.NewPromptTemplate().
		System(f.systemMessage, input).
		User(f.userMessage, input).
		Build()
	if err != nil {
		return
	}
	res, err := f.runner.Run(ctx, prompt, opts...)
	if err != nil {
		return
	}
	return res, nil
}

// RunStream executes the flow in a streaming manner with the given input and options, returning a streamer or an error.
func (f *Flow[I, O]) RunStream(ctx context.Context, input I, opts ...blades.ModelOption) (blades.Streamer[O], error) {
	res, err := f.Run(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	pipe := blades.NewStreamPipe[O]()
	pipe.Send(res)
	pipe.Close()
	return pipe, nil
}
