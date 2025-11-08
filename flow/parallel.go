package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
	"golang.org/x/sync/errgroup"
)

// ParallelOption defines a function type for configuring Parallel.
type ParallelOption func(*Parallel)

// WithParallelMerger sets a custom merger function for the Parallel.
func WithParallelMerger(merger ParallelMerger) ParallelOption {
	return func(p *Parallel) {
		p.merger = merger
	}
}

// ParallelMerger is a function that merges the outputs of multiple runners into a single output.
type ParallelMerger func(ctx context.Context, outputs []*blades.Message) (*blades.Message, error)

// Parallel represents a collection of Runnable runners that process input concurrently.
type Parallel struct {
	merger  ParallelMerger
	runners []blades.Runnable
}

// NewParallel creates a new Parallel with the given runners.
func NewParallel(runners []blades.Runnable, opts ...ParallelOption) *Parallel {
	p := &Parallel{
		runners: runners,
		merger: func(ctx context.Context, outputs []*blades.Message) (*blades.Message, error) {
			result := blades.NewMessage(blades.RoleAssistant)
			for _, output := range outputs {
				result.Parts = append(result.Parts, output.Parts...)
			}
			return result, nil
		},
	}
	for _, apply := range opts {
		apply(p)
	}
	return p
}

// Run executes the chain of runners sequentially, passing the output of one as the input to the next.
func (p *Parallel) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (o *blades.Message, err error) {
	var (
		outputs = make([]*blades.Message, len(p.runners))
	)
	eg, ctx := errgroup.WithContext(ctx)
	for idx, runner := range p.runners {
		idxCopy := idx
		eg.Go(func() error {
			output, err := runner.Run(ctx, input, opts...)
			if err != nil {
				return err
			}
			outputs[idxCopy] = output
			return nil
		})
	}
	if err = eg.Wait(); err != nil {
		return
	}
	return p.merger(ctx, outputs)
}

// RunStream executes the runners sequentially, streaming each output as it is produced.
// Note: Although this method belongs to the Parallel struct, it runs runners one after another, not in parallel.
func (p *Parallel) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) stream.Streamable[*blades.Message] {
	return func(yield func(*blades.Message, error) bool) {
		message, err := p.Run(ctx, input, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(message, nil)
	}
}
