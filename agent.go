package blades

import (
	"context"
	"slices"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/skills"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// Agent is the core interface for all agents in the system.
type Agent interface {
	Name() string
	Description() string
	Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error)
}

// Schemaer is an optional interface that agents can implement to
// expose input/output JSON schemas for structured tool integration.
type Schemaer interface {
	InputSchema() *jsonschema.Schema
	OutputSchema() *jsonschema.Schema
}

// llmAgent is the default Agent implementation backed by an LLM provider.
type llmAgent struct {
	name           string
	description    string
	inputSchema    *jsonschema.Schema
	outputSchema   *jsonschema.Schema
	hooks          []hook.Hook
	tools          []tools.Tool
	skills         []skills.Skill
	skillToolset   *skills.Toolset
	resolver       tools.Resolver
	provider       model.Provider
	promptBuilders []prompt.Builder
	compactor      compact.Compactor
	contextWindow  model.ContextWindow
	tokenCounter   model.TokenCounter
	policy         policy.Policy
	thinkingAsText bool
}

// NewAgent creates a new default LLM-backed Agent.
func NewAgent(name string, opts ...AgentOption) (Agent, error) {
	a := &llmAgent{
		name:          name,
		tokenCounter:  model.ApproxTokenCounter{},
		contextWindow: model.ContextWindow{MaxTokens: 128_000, OutputTokens: 8_192},
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.provider == nil {
		return nil, ErrModelProviderRequired
	}
	if err := a.configureSkills(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *llmAgent) configureSkills() error {
	a.skillToolset = nil
	if len(a.skills) == 0 {
		return nil
	}
	toolset, err := skills.NewToolset(a.skills)
	if err != nil {
		return err
	}
	a.skillToolset = toolset
	return nil
}

// Name returns the name of the agent.
func (a *llmAgent) Name() string { return a.name }

// Description returns a brief description of the agent's purpose and capabilities.
func (a *llmAgent) Description() string { return a.description }

// InputSchema returns the JSON schema for the agent's input.
func (a *llmAgent) InputSchema() *jsonschema.Schema { return a.inputSchema }

// OutputSchema returns the JSON schema for the agent's output.
func (a *llmAgent) OutputSchema() *jsonschema.Schema { return a.outputSchema }

// Run implements the Agent interface.
func (a *llmAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	sess := session.Ensure(ctx)
	ctx = session.NewContext(ctx, sess)
	ctx = NewContext(ctx, newRunningAgent(ctx, a))
	allTools, err := a.resolveTools(ctx)
	if err != nil {
		return nil, err
	}
	var skillRuntime *skills.Runtime
	if a.skillToolset != nil {
		skillRuntime = a.skillToolset.NewRuntime()
		allTools = append(allTools, skillRuntime.Tools()...)
	}
	output := make(chan event.Output, 64)
	l := &agentLoop{
		agent:        a,
		ctx:          ctx,
		output:       output,
		allTools:     allTools,
		skillRuntime: skillRuntime,
		sess:         sess,
		inputs:       newInputQueue(ctx, input),
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

func (a *llmAgent) clone() *llmAgent {
	fork := *a
	fork.hooks = slices.Clone(a.hooks)
	fork.tools = slices.Clone(a.tools)
	fork.skills = slices.Clone(a.skills)
	fork.promptBuilders = slices.Clone(a.promptBuilders)
	return &fork
}
