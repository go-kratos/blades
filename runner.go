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
		r.Resumable = resumable
	}
}

// WithResumeHistory configures whether the Runner should resume history.
func WithResumeHistory(resumeHistory bool) RunnerOption {
	return func(r *Runner) {
		r.ResumeHistory = resumeHistory
	}
}

// RunOptions holds configuration options for running the agent.
type RunOptions struct {
	Session      Session
	InvocationID string
}

// Runner is responsible for executing a Runnable agent within a session context.
type Runner struct {
	Resumable     bool
	ResumeHistory bool
	rootAgent     Agent
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
		Resumable:  r.Resumable,
		Streamable: streamable,
		Message:    message,
	}
	// Append the new message to the session history if it doesn't already exist.
	if err := r.appendNewMessage(ctx, invocation, message); err != nil {
		return nil, err
	}
	return invocation, nil
}

// appendNewMessage appends a new message to the session history.
func (r *Runner) appendNewMessage(ctx context.Context, invocation *Invocation, message *Message) error {
	if invocation.Session == nil {
		return nil
	}
	message.InvocationID = invocation.ID
	return invocation.Session.Append(ctx, message)
}

// historySets creates a map of message IDs to messages from the session history.
// This map is used to filter out already processed messages during resume operations.
// Returns nil if the session is nil.
func (r *Runner) historySets(ctx context.Context, session Session) map[string]*Message {
	if session == nil {
		return nil
	}
	history := session.History()
	sets := make(map[string]*Message, len(history))
	for _, m := range history {
		if m.ID == "" {
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
	history := r.historySets(ctx, o.Session)
	return stream.Filter(r.rootAgent.Run(NewSessionContext(ctx, o.Session), invocation), func(msg *Message) bool {
		// If ResumeHistory is enabled, allow all messages.
		// Otherwise, filter out messages that already exist in history.
		if r.ResumeHistory {
			return true
		}
		_, exists := history[msg.ID]
		return !exists
	})
}
