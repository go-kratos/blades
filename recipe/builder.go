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
	modelRegistry ModelRegistry
	toolRegistry  ToolRegistry
	params        map[string]any
}

// WithModelRegistry sets the model registry for resolving model names.
func WithModelRegistry(r ModelRegistry) BuildOption {
	return func(o *buildOptions) {
		o.modelRegistry = r
	}
}

// WithToolRegistry sets the tool registry for resolving tool names.
func WithToolRegistry(r ToolRegistry) BuildOption {
	return func(o *buildOptions) {
		o.toolRegistry = r
	}
}

// WithParams sets parameter values for template rendering.
func WithParams(params map[string]any) BuildOption {
	return func(o *buildOptions) {
		o.params = params
	}
}

// Build constructs a blades.Agent from a RecipeSpec.
func Build(spec *RecipeSpec, opts ...BuildOption) (blades.Agent, error) {
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

	// No sub-recipes: build a single agent
	var (
		agent blades.Agent
		err   error
	)
	if len(spec.SubRecipes) == 0 {
		agent, err = buildSingleAgent(spec, params, o)
	} else {
		// With sub-recipes: build based on execution mode
		switch spec.Execution {
		case ExecutionSequential:
			agent, err = buildSequentialAgent(spec, params, o)
		case ExecutionParallel:
			agent, err = buildParallelAgent(spec, params, o)
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

// buildSingleAgent creates a single blades.Agent from a RecipeSpec with no sub-recipes.
func buildSingleAgent(spec *RecipeSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
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

	// Resolve external tools
	resolvedTools, err := resolveTools(spec.Tools, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(resolvedTools) > 0 {
		agentOpts = append(agentOpts, blades.WithTools(resolvedTools...))
	}

	return blades.NewAgent(spec.Name, agentOpts...)
}

// buildSubAgent creates a blades.Agent from a SubRecipeSpec.
// parentModel is the fallback model name from the parent spec.
func buildSubAgent(sub *SubRecipeSpec, parentModel string, params map[string]any, o *buildOptions) (blades.Agent, error) {
	modelName := sub.Model
	if modelName == "" {
		modelName = parentModel
	}
	if modelName == "" {
		return nil, fmt.Errorf("recipe: sub_recipe %q has no model and parent has no model", sub.Name)
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
	if err := validateParamValues(fmt.Sprintf("sub_recipe %q", sub.Name), sub.Parameters, subParams); err != nil {
		return nil, err
	}

	// Preserve unknown keys (e.g. {{.syntax_report}}) so the runtime renderer
	// can resolve them from session state while still pre-rendering known params.
	instruction := sub.Instruction
	if hasTemplateActions(instruction) {
		rendered, err := renderTemplatePreservingUnknown(instruction, subParams)
		if err != nil {
			return nil, fmt.Errorf("sub_recipe %q: failed to render instruction: %w", sub.Name, err)
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

	resolvedTools, err := resolveTools(sub.Tools, o)
	if err != nil {
		return nil, fmt.Errorf("sub_recipe %q: %w", sub.Name, err)
	}
	if len(resolvedTools) > 0 {
		agentOpts = append(agentOpts, blades.WithTools(resolvedTools...))
	}

	agent, err := blades.NewAgent(sub.Name, agentOpts...)
	if err != nil {
		return nil, err
	}
	return withPromptTemplate(agent, fmt.Sprintf("sub_recipe %q", sub.Name), sub.Prompt, subParams)
}

// buildSequentialAgent creates a sequential flow from sub-recipes.
func buildSequentialAgent(spec *RecipeSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	subAgents := make([]blades.Agent, 0, len(spec.SubRecipes))
	for i := range spec.SubRecipes {
		agent, err := buildSubAgent(&spec.SubRecipes[i], spec.Model, params, o)
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

// buildParallelAgent creates a parallel flow from sub-recipes.
func buildParallelAgent(spec *RecipeSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	subAgents := make([]blades.Agent, 0, len(spec.SubRecipes))
	for i := range spec.SubRecipes {
		agent, err := buildSubAgent(&spec.SubRecipes[i], spec.Model, params, o)
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

// buildToolAgent creates a parent agent with sub-recipes wrapped as tools.
func buildToolAgent(spec *RecipeSpec, params map[string]any, o *buildOptions) (blades.Agent, error) {
	model, err := o.modelRegistry.Resolve(spec.Model)
	if err != nil {
		return nil, err
	}

	// Build each sub-recipe as an agent, then wrap as a tool
	agentTools := make([]tools.Tool, 0, len(spec.SubRecipes))
	for i := range spec.SubRecipes {
		subAgent, err := buildSubAgent(&spec.SubRecipes[i], spec.Model, params, o)
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
