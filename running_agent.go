package blades

import "context"

// RunningAgent is an Agent annotated with its position in the current run tree.
type RunningAgent interface {
	Agent
	Root() RunningAgent
	Parent() (RunningAgent, bool)
}

type runningAgentKey struct{}

// NewContext stores a RunningAgent in the context.
func NewContext(ctx context.Context, agent RunningAgent) context.Context {
	return context.WithValue(ctx, runningAgentKey{}, agent)
}

// FromContext retrieves the current RunningAgent from the context.
func FromContext(ctx context.Context) (RunningAgent, bool) {
	agent, ok := ctx.Value(runningAgentKey{}).(RunningAgent)
	return agent, ok
}

type runningAgent struct {
	Agent
	parent RunningAgent
}

func newRunningAgent(ctx context.Context, agent Agent) RunningAgent {
	if running, ok := agent.(RunningAgent); ok {
		return running
	}

	var parent RunningAgent
	if p, ok := FromContext(ctx); ok {
		parent = p
	}
	return &runningAgent{
		Agent:  agent,
		parent: parent,
	}
}

func (a *runningAgent) Parent() (RunningAgent, bool) {
	if a.parent == nil {
		return nil, false
	}
	return a.parent, true
}

func (a *runningAgent) Root() RunningAgent {
	if a.parent == nil {
		return a
	}
	return a.parent.Root()
}

func (a *runningAgent) unwrapAgent() Agent {
	return a.Agent
}
