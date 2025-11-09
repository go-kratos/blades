package blades

import (
	"context"
)

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
	Agent
	session      Session
	resumable    bool
	invocationID string
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(agent Agent, opts ...RunOption) *Runner {
	runner := &Runner{
		Agent:        agent,
		session:      NewSession(),
		invocationID: NewInvocationID(),
	}
	for _, opt := range opts {
		opt(runner)
	}
	return runner
}

// Run executes the agent with the provided prompt and options within the session context.
func (r *Runner) Run(ctx context.Context, message *Message, opts ...ModelOption) (*Message, error) {
	return nil, nil
}

func (r *Runner) RunStream(ctx context.Context, message *Message, opts ...ModelOption) Sequence[*Message] {
	return nil
}
