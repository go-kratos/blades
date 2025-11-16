package blades

import (
	"context"
)

// RunOption defines options for configuring the RunOptions.
type RunOption func(*RunOptions)

// WithSession sets a custom session for the Runner.
func WithSession(session Session) RunOption {
	return func(o *RunOptions) {
		o.session = session
	}
}

// WithInvocationID sets a custom invocation ID for the Runner.
func WithInvocationID(invocationID string) RunOption {
	return func(r *RunOptions) {
		r.invocationID = invocationID
	}
}

// RunnerOption defines options for configuring the Runner.
type RunnerOption func(*Runner)

// WithResumable configures whether the Runner supports resumable sessions.
func WithResumable(resumable bool) RunnerOption {
	return func(r *Runner) {
		r.resumable = resumable
	}
}

// RunOptions holds configuration options for running an agent.
type RunOptions struct {
	session      Session
	invocationID string
}

// Runner is responsible for executing a Runnable agent within a session context.
type Runner struct {
	agent     Agent
	resumable bool
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(agent Agent, opts ...RunnerOption) *Runner {
	r := &Runner{
		agent: agent,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// buildInvocation constructs an Invocation object for the given message and options.
func (r *Runner) buildInvocation(ctx context.Context, message *Message, streamable bool, opts ...RunOption) (context.Context, *Invocation) {
	runOpts := &RunOptions{
		session:      NewSession(),
		invocationID: NewInvocationID(),
	}
	return NewSessionContext(ctx, runOpts.session), &Invocation{
		ID:         runOpts.invocationID,
		Session:    runOpts.session,
		Resumable:  r.resumable,
		Streamable: streamable,
		Message:    message,
	}
}

// Run executes the agent with the provided prompt and options within the session context.
func (r *Runner) Run(ctx context.Context, message *Message, opts ...RunOption) (*Message, error) {
	iter := r.agent.Run(r.buildInvocation(ctx, message, false, opts...))
	for output, err := range iter {
		if err != nil {
			return nil, err
		}
		return output, nil
	}
	return nil, ErrNoFinalResponse
}

func (r *Runner) RunStream(ctx context.Context, message *Message, opts ...RunOption) Generator[*Message, error] {
	return r.agent.Run(r.buildInvocation(ctx, message, true, opts...))
}
