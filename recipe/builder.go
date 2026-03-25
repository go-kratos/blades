package recipe

import (
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/tools"
)

// BuildOption configures the Build process.
type BuildOption func(*buildOptions)

type buildOptions struct {
	modelRegistry      ModelResolver
	toolRegistry       ToolResolver
	middlewareRegistry MiddlewareResolver
	params             map[string]any
	contextEnabled     *bool
}

// WithModelRegistry sets the model resolver for resolving model names.
func WithModelRegistry(r ModelResolver) BuildOption {
	return func(o *buildOptions) {
		o.modelRegistry = r
	}
}

// WithToolRegistry sets the tool resolver for resolving tool names.
func WithToolRegistry(r ToolResolver) BuildOption {
	return func(o *buildOptions) {
		o.toolRegistry = r
	}
}

// WithMiddlewareRegistry sets the middleware resolver for resolving middleware names.
func WithMiddlewareRegistry(r MiddlewareResolver) BuildOption {
	return func(o *buildOptions) {
		o.middlewareRegistry = r
	}
}

// WithParams sets parameter values for template rendering.
func WithParams(params map[string]any) BuildOption {
	return func(o *buildOptions) {
		o.params = params
	}
}

// WithContext explicitly controls whether built agents load session history.
// When omitted, the underlying agent default behavior is preserved.
func WithContext(enabled bool) BuildOption {
	return func(o *buildOptions) {
		o.contextEnabled = &enabled
	}
}

// Build constructs a blades.Agent from a AgentSpec.
func Build(spec *AgentSpec, opts ...BuildOption) (blades.Agent, error) {
	if err := Validate(spec); err != nil {
		return nil, err
	}
	o := &buildOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.modelRegistry == nil {
		return nil, fmt.Errorf("recipe: model registry is required")
	}

	// Merge params with defaults and validate
	params := resolveParams(spec.Parameters, o.params)
	if err := ValidateParams(spec, params); err != nil {
		return nil, err
	}

	// No sub-agents: build a single agent
	var (
		agent blades.Agent
		err   error
	)
	if len(spec.SubAgents) == 0 {
		agent, err = buildSingleAgent(spec, params, o)
	} else {
		// With sub-agents: build based on execution mode
		switch spec.Execution {
		case ExecutionSequential:
			agent, err = buildSequentialAgent(spec, params, o)
		case ExecutionParallel:
			agent, err = buildParallelAgent(spec, params, o)
		case ExecutionLoop:
			agent, err = buildLoopAgent(spec, params, o)
		case ExecutionTool:
			agent, err = buildToolAgent(spec, params, o)
		default:
			return nil, fmt.Errorf("recipe: unsupported execution mode %q", spec.Execution)
		}
	}
	if err != nil {
		return nil, err
	}
	return withPromptInjection(spec, params, agent)
}

// buildSingleAgent creates a single blades.Agent from a AgentSpec with no sub-agents.
func buildSingleAgent(spec *AgentSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	model, err := o.modelRegistry.Resolve(spec.Model)
	if err != nil {
		return nil, err
	}

	agentOpts := []blades.AgentOption{
		blades.WithModel(model),
	}
	if spec.Description != "" {
		agentOpts = append(agentOpts, blades.WithDescription(spec.Description))
	}

	// Render instruction with params. Pre-render only what can be resolved from params.
	instruction, err := renderTemplate(spec.Instruction, params)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: failed to render instruction: %w", spec.Name, err)
	}
	agentOpts = append(agentOpts, blades.WithInstruction(instruction))

	if spec.OutputKey != "" {
		agentOpts = append(agentOpts, blades.WithOutputKey(spec.OutputKey))
	}
	if spec.MaxIterations > 0 {
		agentOpts = append(agentOpts, blades.WithMaxIterations(spec.MaxIterations))
	}
	if o.contextEnabled != nil {
		agentOpts = append(agentOpts, blades.WithContext(*o.contextEnabled))
	}

	// Resolve external tools
	resolvedTools, err := resolveTools(spec.Tools, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(resolvedTools) > 0 {
		agentOpts = append(agentOpts, blades.WithTools(resolvedTools...))
	}

	// Resolve middlewares
	middlewares, err := resolveMiddlewares(spec.Middlewares, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(middlewares) > 0 {
		agentOpts = append(agentOpts, blades.WithMiddleware(middlewares...))
	}

	agent, err := blades.NewAgent(spec.Name, agentOpts...)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// buildSubAgent creates a blades.Agent from a SubAgentSpec.
// parentModel is the fallback model name from the parent spec.
func buildSubAgent(sub *SubAgentSpec, parentModel string, params map[string]any, o *buildOptions) (blades.Agent, error) {
	modelName := sub.Model
	if modelName == "" {
		modelName = parentModel
	}
	if modelName == "" {
		return nil, fmt.Errorf("recipe: sub_agent %q has no model and parent has no model", sub.Name)
	}

	model, err := o.modelRegistry.Resolve(modelName)
	if err != nil {
		return nil, err
	}

	agentOpts := []blades.AgentOption{
		blades.WithModel(model),
	}
	if sub.Description != "" {
		agentOpts = append(agentOpts, blades.WithDescription(sub.Description))
	}

	subParams := resolveParams(sub.Parameters, params)
	if err := validateParamValues(fmt.Sprintf("sub_agent %q", sub.Name), sub.Parameters, subParams); err != nil {
		return nil, err
	}

	// Preserve unknown keys (e.g. {{.syntax_report}}) so the runtime renderer
	// can resolve them from session state while still pre-rendering known params.
	instruction := sub.Instruction
	if hasTemplateActions(instruction) {
		rendered, err := renderTemplatePreservingUnknown(instruction, subParams)
		if err != nil {
			return nil, fmt.Errorf("sub_agent %q: failed to render instruction: %w", sub.Name, err)
		}
		instruction = rendered
	}
	agentOpts = append(agentOpts, blades.WithInstruction(instruction))

	if sub.OutputKey != "" {
		agentOpts = append(agentOpts, blades.WithOutputKey(sub.OutputKey))
	}
	if sub.MaxIterations > 0 {
		agentOpts = append(agentOpts, blades.WithMaxIterations(sub.MaxIterations))
	}
	if o.contextEnabled != nil {
		agentOpts = append(agentOpts, blades.WithContext(*o.contextEnabled))
	}

	resolvedTools, err := resolveTools(sub.Tools, o)
	if err != nil {
		return nil, fmt.Errorf("sub_agent %q: %w", sub.Name, err)
	}
	if len(resolvedTools) > 0 {
		agentOpts = append(agentOpts, blades.WithTools(resolvedTools...))
	}

	// Resolve middlewares
	middlewares, err := resolveMiddlewares(sub.Middlewares, o)
	if err != nil {
		return nil, fmt.Errorf("sub_agent %q: %w", sub.Name, err)
	}
	if len(middlewares) > 0 {
		agentOpts = append(agentOpts, blades.WithMiddleware(middlewares...))
	}

	agent, err := blades.NewAgent(sub.Name, agentOpts...)
	if err != nil {
		return nil, err
	}
	agent, err = withUserPromptTemplate(agent, fmt.Sprintf("sub_agent %q", sub.Name), sub.Prompt, subParams)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// buildSequentialAgent creates a sequential flow from sub-agents.
func buildSequentialAgent(spec *AgentSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	subAgents := make([]blades.Agent, 0, len(spec.SubAgents))
	for i := range spec.SubAgents {
		agent, err := buildSubAgent(&spec.SubAgents[i], spec.Model, params, o)
		if err != nil {
			return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		subAgents = append(subAgents, agent)
	}
	return flow.NewSequentialAgent(flow.SequentialConfig{
		Name:        spec.Name,
		Description: spec.Description,
		SubAgents:   subAgents,
	}), nil
}

// buildParallelAgent creates a parallel flow from sub-agents.
func buildParallelAgent(spec *AgentSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	subAgents := make([]blades.Agent, 0, len(spec.SubAgents))
	for i := range spec.SubAgents {
		agent, err := buildSubAgent(&spec.SubAgents[i], spec.Model, params, o)
		if err != nil {
			return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		subAgents = append(subAgents, agent)
	}
	return flow.NewParallelAgent(flow.ParallelConfig{
		Name:        spec.Name,
		Description: spec.Description,
		SubAgents:   subAgents,
	}), nil
}

// buildLoopAgent creates a loop flow from sub-agents.
// The loop runs until max_iterations is reached or a sub-agent signals exit
// via the loop_exit tool. LoopCondition is not supported in recipe YAML.
func buildLoopAgent(spec *AgentSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	subAgents := make([]blades.Agent, 0, len(spec.SubAgents))
	for i := range spec.SubAgents {
		agent, err := buildSubAgent(&spec.SubAgents[i], spec.Model, params, o)
		if err != nil {
			return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		subAgents = append(subAgents, agent)
	}
	return flow.NewLoopAgent(flow.LoopConfig{
		Name:          spec.Name,
		Description:   spec.Description,
		MaxIterations: spec.MaxIterations,
		SubAgents:     subAgents,
	}), nil
}

// buildToolAgent creates a parent agent with sub-agents wrapped as tools.
func buildToolAgent(spec *AgentSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	model, err := o.modelRegistry.Resolve(spec.Model)
	if err != nil {
		return nil, err
	}

	// Build each sub-agent as an agent, then wrap as a tool
	agentTools := make([]tools.Tool, 0, len(spec.SubAgents))
	for i := range spec.SubAgents {
		subAgent, err := buildSubAgent(&spec.SubAgents[i], spec.Model, params, o)
		if err != nil {
			return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		agentTools = append(agentTools, blades.NewAgentTool(subAgent))
	}

	// Also resolve any explicit tools
	externalTools, err := resolveTools(spec.Tools, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	allTools := append(agentTools, externalTools...)

	instruction, err := renderTemplate(spec.Instruction, params)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: failed to render instruction: %w", spec.Name, err)
	}

	agentOpts := []blades.AgentOption{
		blades.WithModel(model),
		blades.WithInstruction(instruction),
		blades.WithTools(allTools...),
	}
	if spec.Description != "" {
		agentOpts = append(agentOpts, blades.WithDescription(spec.Description))
	}
	if spec.OutputKey != "" {
		agentOpts = append(agentOpts, blades.WithOutputKey(spec.OutputKey))
	}
	if spec.MaxIterations > 0 {
		agentOpts = append(agentOpts, blades.WithMaxIterations(spec.MaxIterations))
	}
	if o.contextEnabled != nil {
		agentOpts = append(agentOpts, blades.WithContext(*o.contextEnabled))
	}

	// Resolve middlewares
	middlewares, err := resolveMiddlewares(spec.Middlewares, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(middlewares) > 0 {
		agentOpts = append(agentOpts, blades.WithMiddleware(middlewares...))
	}

	return blades.NewAgent(spec.Name, agentOpts...)
}

// resolveTools resolves a list of tool names to actual tools.Tool instances.
func resolveTools(names []string, o *buildOptions) ([]tools.Tool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if o.toolRegistry == nil {
		return nil, fmt.Errorf("tool registry is required when tools are referenced")
	}
	resolved := make([]tools.Tool, 0, len(names))
	for _, name := range names {
		t, err := o.toolRegistry.Resolve(name)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, t)
	}
	return resolved, nil
}

// resolveMiddlewares resolves a list of MiddlewareSpec entries to blades.Middleware instances.
func resolveMiddlewares(specs []MiddlewareSpec, o *buildOptions) ([]blades.Middleware, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if o.middlewareRegistry == nil {
		return nil, fmt.Errorf("middleware registry is required when middlewares are referenced")
	}
	resolved := make([]blades.Middleware, 0, len(specs))
	for _, spec := range specs {
		mw, err := o.middlewareRegistry.Resolve(spec.Name, spec.Options)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, mw)
	}
	return resolved, nil
}
