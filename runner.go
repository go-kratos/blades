package blades

import (
	"context"
)

// RunOption defines options for configuring the Runner.
type RunOption func(*runner)

// WithSession sets a custom session for the Runner.
func WithSession(session Session) RunOption {
	return func(r *runner) {
		r.session = session
	}
}

// WithResumable configures whether the Runner supports resumable sessions.
func WithResumable(resumable bool) RunOption {
	return func(r *runner) {
		r.resumable = resumable
	}
}

// WithInvocationID sets a custom invocation ID for the Runner.
func WithInvocationID(invocationID string) RunOption {
	return func(r *runner) {
		r.invocationID = invocationID
	}
}

// runner is responsible for executing a Runnable agent within a session context.
type runner struct {
	Agent
	session      Session
	resumable    bool
	invocationID string
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(agent Agent, opts ...RunOption) Runner {
	r := &runner{
		Agent:        agent,
		session:      NewSession(),
		invocationID: NewInvocationID(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// buildInvocation constructs an Invocation object for the given message and options.
func (r *runner) buildInvocation(ctx context.Context, message *Message, streamable bool, opts ...ModelOption) *Invocation {
	return &Invocation{
		ID:           r.invocationID,
		Resumable:    r.resumable,
		Session:      r.session,
		Streamable:   streamable,
		Message:      message,
		ModelOptions: opts,
	}
}

// Run executes the agent with the provided prompt and options within the session context.
func (r *runner) Run(ctx context.Context, message *Message, opts ...ModelOption) (*Message, error) {
	for output, err := range r.Agent.Run(ctx, r.buildInvocation(ctx, message, false, opts...)) {
		if err != nil {
			return nil, err
		}
		return output, nil
	}
	return nil, ErrNoFinalResponse
}

func (r *runner) RunStream(ctx context.Context, message *Message, opts ...ModelOption) Generator[*Message, error] {
	return r.Agent.Run(ctx, r.buildInvocation(ctx, message, true, opts...))
}
