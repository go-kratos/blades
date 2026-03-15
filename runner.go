package blades

import (
	"context"

	"github.com/go-kratos/blades/stream"
)

// RunOption defines options for configuring the Runner.
type RunOption func(*RunOptions)

// WithSession sets a custom session for the Runner.
func WithSession(session Session) RunOption {
	return func(r *RunOptions) {
		r.Session = session
	}
}

// WithResume indicates whether to resume from the last session state.
func WithResume(resume bool) RunOption {
	return func(r *RunOptions) {
		r.Resume = resume
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
	Resume       bool
	InvocationID string
}

// Runner is responsible for executing a Runnable agent within a session context.
type Runner struct {
	rootAgent Agent
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(rootAgent Agent) *Runner {
	r := &Runner{
		rootAgent: rootAgent,
	}
	return r
}

// buildInvocation constructs an Invocation object for the given message and options.
func (r *Runner) buildInvocation(ctx context.Context, message *Message, stream bool, o *RunOptions) (*Invocation, error) {
	invocation := &Invocation{
		ID:      o.InvocationID,
		Session: o.Session,
		Resume:  o.Resume,
		Stream:  stream,
		Message: message,
	}
	if o.Session != nil {
		invocation.History = append(invocation.History, o.Session.History()...)
	}
	if message != nil {
		message.Author = "user"
		if err := r.appendNewMessage(ctx, invocation, message); err != nil {
			return nil, err
		}
	}
	return invocation, nil
}

// appendNewMessage appends a new message to the session history.
func (r *Runner) appendNewMessage(ctx context.Context, invocation *Invocation, message *Message) error {
	if invocation.Session == nil || message == nil {
		return nil
	}
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
		if output.Status == StatusCompleted {
			if err := r.appendNewMessage(ctx, invocation, output); err != nil {
				return nil, err
			}
		}
	}
	if output == nil {
		return nil, ErrNoFinalResponse
	}
	return output, nil
}

// RunStream executes the agent in a streaming manner, yielding messages as they are produced.
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
		return stream.Error[*Message](err)
	}
	return func(yield func(*Message, error) bool) {
		iter := r.rootAgent.Run(NewSessionContext(ctx, o.Session), invocation)
		for output, err := range iter {
			if err != nil {
				yield(nil, err)
				return
			}
			if output.Status == StatusCompleted {
				if err := r.appendNewMessage(ctx, invocation, output); err != nil {
					yield(nil, err)
					return
				}
			}
			if !yield(output, nil) {
				return
			}
		}
	}
}
