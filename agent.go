package blades

import (
	"context"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
)

const outputBufferSize = 64

// Agent is the core interface for all agents in the system.
type Agent interface {
	Name() string
	Description() string
	Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error)
}

// llmAgent is the default Agent implementation backed by an LLM provider.
type llmAgent struct {
	name           string
	description    string
	hooks          []hook.Hook
	tools          []tools.Tool
	resolver       tools.Resolver
	provider       model.Provider
	promptBuilders []prompt.Builder
	compactor      compact.Compactor
	policy         policy.Policy
}

// NewAgent creates a new default LLM-backed Agent.
func NewAgent(name string, opts ...AgentOption) (Agent, error) {
	a := &llmAgent{name: name}
	for _, opt := range opts {
		opt(a)
	}
	if a.provider == nil {
		return nil, ErrModelProviderRequired
	}
	return a, nil
}

func (a *llmAgent) Name() string        { return a.name }
func (a *llmAgent) Description() string { return a.description }

// Run implements the Agent interface.
func (a *llmAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	allTools, err := a.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	sess := session.Ensure(ctx)
	ctx = session.NewContext(ctx, sess)
	output := make(chan event.Output, outputBufferSize)
	l := &agentLoop{
		agent:    a,
		ctx:      ctx,
		output:   output,
		allTools: allTools,
		sess:     sess,
		inputs:   newInputQueue(ctx, input),
	}
	go l.run()
	return output, nil
}

func (a *llmAgent) resolveTools(ctx context.Context) ([]tools.Tool, error) {
	allTools := make([]tools.Tool, 0, len(a.tools))
	allTools = append(allTools, a.tools...)
	if a.resolver != nil {
		resolved, err := a.resolver.List(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, resolved...)
	}
	return allTools, nil
}
