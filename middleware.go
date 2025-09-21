package blades

import "context"

// Middleware represents a function that wraps a Runnable with additional behavior.
type Middleware func(next Runner) Runner

// ApplyMiddlewares applies a series of middlewares to a Runnable.
func ApplyMiddlewares(outer Middleware, others ...Middleware) Middleware {
	return func(next Runner) Runner {
		for i := len(others) - 1; i >= 0; i-- { // reverse
			next = others[i](next)
		}
		return outer(next)
	}
}

// RunnerFunc is a helper to create Runnable instances from functions.
type RunnerFunc struct {
	run       func(context.Context, *Prompt, ...ModelOption) (*Generation, error)
	runStream func(context.Context, *Prompt, ...ModelOption) (Streamer[*Generation], error)
}

// NewRunnerFunc creates a new RunnerFunc from the provided functions.
func NewRunnerFunc(
	run func(context.Context, *Prompt, ...ModelOption) (*Generation, error),
	runStream func(context.Context, *Prompt, ...ModelOption) (Streamer[*Generation], error),
) Runner {
	return RunnerFunc{
		run:       run,
		runStream: runStream,
	}
}

// Run executes the RunnableFunc with the given context, prompt, and options.
func (f RunnerFunc) Run(ctx context.Context, p *Prompt, opts ...ModelOption) (*Generation, error) {
	return f.run(ctx, p, opts...)
}

// RunStream executes the RunnableFunc in streaming mode with the given context, prompt, and options.
func (f RunnerFunc) RunStream(ctx context.Context, p *Prompt, opts ...ModelOption) (Streamer[*Generation], error) {
	return f.runStream(ctx, p, opts...)
}
