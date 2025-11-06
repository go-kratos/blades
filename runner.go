package blades

import (
	"context"

	"github.com/google/uuid"
)

var _ Runnable = (*Runner)(nil)

// RunOption defines options for configuring the Runner.
type RunOption func(*Runner)

// WithSession sets a custom session for the Runner.
func WithSession(session Session) RunOption {
	return func(r *Runner) {
		r.session = session
	}
}

// WithResumable configures whether the Runner supports resumable sessions.
func WithResumable(resumable bool) RunOption {
	return func(r *Runner) {
		r.resumable = resumable
	}
}

// WithInvocationID sets a custom invocation ID for the Runner.
func WithInvocationID(invocationID string) RunOption {
	return func(r *Runner) {
		r.invocationID = invocationID
	}
}

// Runner is responsible for executing a Runnable agent within a session context.
type Runner struct {
	agent        Runnable
	session      Session
	resumable    bool
	invocationID string
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(agent Runnable, opts ...RunOption) *Runner {
	runner := &Runner{
		agent:        agent,
		invocationID: uuid.NewString(),
		session:      NewSession(),
	}
	for _, opt := range opts {
		opt(runner)
	}
	return runner
}

func (r *Runner) buildInvocationContext(ctx context.Context) context.Context {
	return NewInvocationContext(ctx, &InvocationContext{
		Session:      r.session,
		Resumable:    r.resumable,
		InvocationID: r.invocationID,
	})
}

// Run executes the agent with the provided prompt and options within the session context.
func (r *Runner) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (*Message, error) {
	return r.agent.Run(r.buildInvocationContext(ctx), prompt, opts...)
}

// RunStream executes the agent in a streaming manner with the provided prompt and options within the session context.
func (r *Runner) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamable[*Message], error) {
	return r.agent.RunStream(r.buildInvocationContext(ctx), prompt, opts...)
}
