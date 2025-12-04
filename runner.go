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

// WithInvocationID sets a custom invocation ID for the Runner.
func WithInvocationID(invocationID string) RunOption {
	return func(r *RunOptions) {
		r.InvocationID = invocationID
	}
}

// RunnerOption defines options for configuring the Runner itself.
type RunnerOption func(*Runner)

// WithResumable configures whether the Runner supports resumable sessions.
func WithResumable(resumable bool) RunnerOption {
	return func(r *Runner) {
		r.resumable = resumable
	}
}

// RunOptions holds configuration options for running the agent.
type RunOptions struct {
	Session      Session
	InvocationID string
}

// Runner is responsible for executing a Runnable agent within a session context.
type Runner struct {
	resumable bool
	rootAgent Agent
}

// NewRunner creates a new Runner with the given agent and options.
func NewRunner(rootAgent Agent, opts ...RunnerOption) *Runner {
	r := &Runner{
		rootAgent: rootAgent,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// buildInvocation constructs an Invocation object for the given message and options.
func (r *Runner) buildInvocation(ctx context.Context, message *Message, streamable bool, o *RunOptions) (*Invocation, error) {
	invocation := &Invocation{
		ID:         o.InvocationID,
		Session:    o.Session,
		Resumable:  r.resumable,
		Streamable: streamable,
		Message:    message,
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
	if message == nil {
		return nil
	}
	message.InvocationID = invocation.ID
	return invocation.Session.Append(ctx, message)
}

// historyByResume creates a map of message IDs to messages from the session history.
// This map is used to filter out already processed messages during resume operations.
// Returns nil if the session is nil.
func (r *Runner) historyByResume(ctx context.Context, session Session, invocation *Invocation) map[string]*Message {
	if !r.resumable {
		return nil
	}
	history := session.History()
	sets := make(map[string]*Message, len(history))
	for _, m := range history {
		if m.InvocationID != invocation.ID {
			continue
		}
		sets[m.ID] = m
	}
	return sets
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
	invocationHistory := r.historyByResume(ctx, o.Session, invocation)
	return func(yield func(*Message, error) bool) {
		iter := r.rootAgent.Run(NewSessionContext(ctx, o.Session), invocation)
		for output, err := range iter {
			if err != nil {
				yield(nil, err)
				return
			}
			// If the output message ID exists in history, skip yielding it.
			_, exists := invocationHistory[output.ID]
			if exists {
				continue
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
