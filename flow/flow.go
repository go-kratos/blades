package flow

import (
	"context"

	"github.com/go-kratos/blades"
)

// Flow represents a typed input→output pipeline.
// It builds a prompt from input I, runs the underlying Runner, and converts
// the model output into type O via OutputConverter.
type Flow[I, O any] struct {
	conv           *blades.OutputConverter[O]
	userTemplate   string
	systemTemplate string
}

// NewFlow creates a Flow wrapping the given Runner and applying options.
func NewFlow[I, O any](runner blades.Runner) *Flow[I, O] {
	return &Flow[I, O]{conv: blades.NewOutputConverter[O](runner)}
}

// WithUserTemplate sets the user message template using a chainable method.
func (f *Flow[I, O]) WithUserTemplate(tmpl string) *Flow[I, O] {
	f.userTemplate = tmpl
	return f
}

// WithSystemTemplate sets the system message template using a chainable method.
func (f *Flow[I, O]) WithSystemTemplate(tmpl string) *Flow[I, O] {
	f.systemTemplate = tmpl
	return f
}

// buildPrompt constructs a Prompt from the input using the templates.
func (f *Flow[I, O]) buildPrompt(input I) (*blades.Prompt, error) {
	return blades.NewPromptTemplate().
		System(f.systemTemplate, input).
		User(f.userTemplate, input).
		Build()
}

// Run executes the flow with the given input and options, returning the output.
func (f *Flow[I, O]) Run(ctx context.Context, input I, opts ...blades.ModelOption) (o O, err error) {
	prompt, err := f.buildPrompt(input)
	if err != nil {
		return o, err
	}
	return f.conv.Run(ctx, prompt, opts...)
}

// RunStream executes the flow in a streaming manner.
// OutputConverter currently yields a single final item, so this returns a
// one-shot stream.
func (f *Flow[I, O]) RunStream(ctx context.Context, input I, opts ...blades.ModelOption) (blades.Streamer[O], error) {
	prompt, err := f.buildPrompt(input)
	if err != nil {
		return nil, err
	}
	return f.conv.RunStream(ctx, prompt, opts...)
}
