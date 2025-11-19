package blades

import (
	"context"
)

// RunOption defines options for configuring the Runner.
type RunOption func(*RunOptions)

// WithSession sets a custom session for the Runner.
func WithSession(session Session) RunOption {
	return func(r *RunOptions) {
		r.Session = session
	}
}

// WithResumable configures whether the Runner supports resumable sessions.
func WithResumable(resumable bool) RunOption {
	return func(r *RunOptions) {
		r.Resumable = resumable
	}
}

// WithInvocationID sets a custom invocation ID for the Runner.
func WithInvocationID(invocationID string) RunOption {
	return func(r *RunOptions) {
		r.InvocationID = invocationID
	}
}

// RunOptions holds configuration options for running the agent.
type RunOptions struct {
	Session      Session
	Resumable    bool
	InvocationID string
}

// Runner is responsible for executing a Runnable agent within a session context.
type Runner struct {
	rootAgent Agent
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(rootAgent Agent) *Runner {
	return &Runner{
		rootAgent: rootAgent,
	}
}

// buildInvocation constructs an Invocation object for the given message and options.
func (r *Runner) buildInvocation(ctx context.Context, message *Message, streamable bool, o *RunOptions) (*Invocation, error) {
	invocation := &Invocation{
		ID:         o.InvocationID,
		Resumable:  o.Resumable,
		Session:    o.Session,
		Streamable: streamable,
		Message:    message,
	}
	if err := r.appendNewMessage(ctx, invocation, message); err != nil {
		return nil, err
	}
	return invocation, nil
}

func (r *Runner) appendNewMessage(ctx context.Context, invocation *Invocation, message *Message) error {
	message.InvocationID = invocation.ID
	return invocation.Session.Append(ctx, message)
}

// Run executes the agent with the provided prompt and options within the session context.
func (r *Runner) Run(ctx context.Context, message *Message, opts ...RunOption) (*Message, error) {
	o := &RunOptions{
		Session:      NewSession(),
		InvocationID: NewInvocationID(),
	}
	for _, opt := range opts {
		opt(o)
	}
	var (
		err    error
		output *Message
	)
	invocation, err := r.buildInvocation(ctx, message, false, o)
	if err != nil {
		return nil, err
	}
	iter := r.rootAgent.Run(NewSessionContext(ctx, o.Session), invocation)
	for output, err = range iter {
		if err != nil {
			return nil, err
		}
	}
	if output == nil {
		return nil, ErrNoFinalResponse
	}
	return output, nil
}

func (r *Runner) RunStream(ctx context.Context, message *Message, opts ...RunOption) Generator[*Message, error] {
	o := &RunOptions{
		Session:      NewSession(),
		InvocationID: NewInvocationID(),
	}
	for _, opt := range opts {
		opt(o)
	}
	invocation, err := r.buildInvocation(ctx, message, true, o)
	if err != nil {
		return func(yield func(*Message, error) bool) {
			yield(nil, err)
		}
	}
	return r.rootAgent.Run(NewSessionContext(ctx, o.Session), invocation)
}
