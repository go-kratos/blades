package recipe

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	bladescontext "github.com/go-kratos/blades/context/summary"
	"github.com/go-kratos/blades/context/window"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/middleware"
	"github.com/go-kratos/blades/tools"
)

// BuildOption configures the Build process.
type BuildOption func(*buildOptions)

// TracingMiddlewareFactory creates a tracing middleware given the ObservabilitySpec.
// Returning nil disables tracing even when observability is configured.
type TracingMiddlewareFactory func(spec *ObservabilitySpec) blades.Middleware

type buildOptions struct {
	modelRegistry            ModelRegistry
	toolRegistry             ToolRegistry
	middlewareRegistry       MiddlewareRegistry
	approvalHandler          middleware.ConfirmFunc
	tracingMiddlewareFactory TracingMiddlewareFactory
	params                   map[string]any
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

// WithApprovalHandler sets the confirmation callback used when spec.Approval is set.
// It is required when any RecipeSpec (or AgentSpec) has a non-nil Approval field.
func WithApprovalHandler(fn middleware.ConfirmFunc) BuildOption {
	return func(o *buildOptions) {
		o.approvalHandler = fn
	}
}

// WithMiddlewareRegistry sets the registry used to resolve named hooks in HooksSpec.
func WithMiddlewareRegistry(r MiddlewareRegistry) BuildOption {
	return func(o *buildOptions) {
		o.middlewareRegistry = r
	}
}

// WithTracingMiddlewareFactory sets a factory function that produces a tracing middleware
// from an ObservabilitySpec. This is called when spec.Observability is non-nil.
// Example with contrib/otel:
//
//	recipe.WithTracingMiddlewareFactory(func(spec *recipe.ObservabilitySpec) blades.Middleware {
//	    if spec.Tracing == "otel" {
//	        return otel.Tracing(otel.WithSystem(spec.System))
//	    }
//	    return nil
//	})
func WithTracingMiddlewareFactory(factory TracingMiddlewareFactory) BuildOption {
	return func(o *buildOptions) {
		o.tracingMiddlewareFactory = factory
	}
}

// buildContextManager constructs a blades.ContextManager from a ContextSpec.
// strategy=truncate → context/window manager
// strategy=summarize → context/summary manager (requires spec.Model to be resolvable)
func buildContextManager(spec *ContextSpec, o *buildOptions) (blades.ContextManager, error) {
	if spec == nil {
		return nil, nil
	}
	switch spec.Strategy {
	case ContextTruncate:
		opts := []window.Option{}
		if spec.MaxTokens > 0 {
			opts = append(opts, window.WithMaxTokens(spec.MaxTokens))
		}
		if spec.MaxMessages > 0 {
			opts = append(opts, window.WithMaxMessages(spec.MaxMessages))
		}
		return window.NewContextManager(opts...), nil

	case ContextSummarize:
		if spec.Model == "" {
			return nil, fmt.Errorf("recipe: context strategy=summarize requires model to be set")
		}
		sumModel, err := o.modelRegistry.Resolve(spec.Model)
		if err != nil {
			return nil, fmt.Errorf("recipe: context summarize model: %w", err)
		}
		opts := []bladescontext.Option{
			bladescontext.WithSummarizer(sumModel),
		}
		if spec.MaxTokens > 0 {
			opts = append(opts, bladescontext.WithMaxTokens(spec.MaxTokens))
		}
		if spec.KeepRecent > 0 {
			opts = append(opts, bladescontext.WithKeepRecent(spec.KeepRecent))
		}
		if spec.BatchSize > 0 {
			opts = append(opts, bladescontext.WithBatchSize(spec.BatchSize))
		}
		return bladescontext.NewContextManager(opts...), nil

	default:
		return nil, fmt.Errorf("recipe: unknown context strategy %q", spec.Strategy)
	}
}

// buildAgentMiddlewares assembles the middleware stack from observability, approval, and hooks specs.
// Order: observability(otel) → on_start hooks → approval → on_complete hooks → on_error hooks.
func buildAgentMiddlewares(spec *RecipeSpec, o *buildOptions) ([]blades.Middleware, error) {
	var mws []blades.Middleware

	// Observability (tracing)
	if spec.Observability != nil && spec.Observability.Tracing != "" {
		if o.tracingMiddlewareFactory == nil {
			return nil, fmt.Errorf("recipe: observability.tracing=%q is configured but no tracing factory was provided (use WithTracingMiddlewareFactory)", spec.Observability.Tracing)
		}
		if mw := o.tracingMiddlewareFactory(spec.Observability); mw != nil {
			mws = append(mws, mw)
		}
	}

	// Hooks: on_start
	if spec.Hooks != nil {
		for _, name := range spec.Hooks.OnStart {
			mw, err := resolveMiddleware(name, o)
			if err != nil {
				return nil, fmt.Errorf("recipe: hooks.on_start %q: %w", name, err)
			}
			mws = append(mws, mw)
		}
	}

	// Approval
	if spec.Approval != nil {
		if o.approvalHandler == nil {
			return nil, fmt.Errorf("recipe: approval is configured but no approval handler was provided (use WithApprovalHandler)")
		}
		if len(spec.Approval.OnTools) == 0 {
			// Confirm every invocation
			mws = append(mws, middleware.Confirm(o.approvalHandler))
		} else {
			// Confirm only when specific tools are registered on the invocation
			onTools := spec.Approval.OnTools
			confirmFn := o.approvalHandler
			mws = append(mws, func(next blades.Handler) blades.Handler {
				return blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
					return func(yield func(*blades.Message, error) bool) {
						matched := false
						for _, t := range inv.Tools {
							for _, name := range onTools {
								if t.Name() == name {
									matched = true
									break
								}
							}
							if matched {
								break
							}
						}
						if matched {
							ok, err := confirmFn(ctx, inv.Message)
							if err != nil {
								yield(nil, err)
								return
							}
							if !ok {
								yield(nil, blades.ErrInterrupted)
								return
							}
						}
						for msg, err := range next.Handle(ctx, inv) {
							if !yield(msg, err) {
								break
							}
						}
					}
				})
			})
		}
	}

	// Hooks: on_complete — only fires when the inner handler finishes without error.
	if spec.Hooks != nil {
		for _, name := range spec.Hooks.OnComplete {
			mw, err := resolveMiddleware(name, o)
			if err != nil {
				return nil, fmt.Errorf("recipe: hooks.on_complete %q: %w", name, err)
			}
			mws = append(mws, wrapOnComplete(mw))
		}
		// Hooks: on_error — only fires when the inner handler yields an error.
		for _, name := range spec.Hooks.OnError {
			mw, err := resolveMiddleware(name, o)
			if err != nil {
				return nil, fmt.Errorf("recipe: hooks.on_error %q: %w", name, err)
			}
			mws = append(mws, wrapOnError(mw))
		}
	}

	return mws, nil
}

// resolveMiddleware resolves a named middleware from the registry.
func resolveMiddleware(name string, o *buildOptions) (blades.Middleware, error) {
	if o.middlewareRegistry == nil {
		return nil, fmt.Errorf("middleware registry is required when hooks are referenced")
	}
	return o.middlewareRegistry.Resolve(name)
}

// BuildFromAgentSpec constructs a blades.Agent from an AgentSpec by converting
// it to a RecipeSpec and delegating to Build.
func BuildFromAgentSpec(spec *AgentSpec, opts ...BuildOption) (blades.Agent, error) {
	if spec == nil {
		return nil, fmt.Errorf("recipe: agent spec is required")
	}
	return Build(spec.ToRecipeSpec(), opts...)
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

	// Context manager
	cm, err := buildContextManager(spec.Context, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if cm != nil {
		agentOpts = append(agentOpts, blades.WithContextManager(cm))
	}

	// Middleware (observability, approval, hooks)
	mws, err := buildAgentMiddlewares(spec, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(mws) > 0 {
		agentOpts = append(agentOpts, blades.WithMiddleware(mws...))
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
	var agent blades.Agent = flow.NewSequentialAgent(flow.SequentialConfig{
		Name:        spec.Name,
		Description: spec.Description,
		SubAgents:   subAgents,
	})
	mws, err := buildAgentMiddlewares(spec, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(mws) > 0 {
		agent = wrapWithMiddlewares(agent, mws)
	}
	return agent, nil
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
	var agent blades.Agent = flow.NewParallelAgent(flow.ParallelConfig{
		Name:        spec.Name,
		Description: spec.Description,
		SubAgents:   subAgents,
	})
	mws, err := buildAgentMiddlewares(spec, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(mws) > 0 {
		agent = wrapWithMiddlewares(agent, mws)
	}
	return agent, nil
}

// middlewareAgent wraps a blades.Agent with a middleware chain.
type middlewareAgent struct {
	inner blades.Agent
	mws   []blades.Middleware
}

func (m *middlewareAgent) Name() string        { return m.inner.Name() }
func (m *middlewareAgent) Description() string { return m.inner.Description() }
func (m *middlewareAgent) Run(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
	handler := blades.Handler(blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
		return m.inner.Run(ctx, inv)
	}))
	handler = blades.ChainMiddlewares(m.mws...)(handler)
	return handler.Handle(ctx, inv)
}

// wrapWithMiddlewares wraps a blades.Agent with the given middleware chain.
func wrapWithMiddlewares(agent blades.Agent, mws []blades.Middleware) blades.Agent {
	return &middlewareAgent{inner: agent, mws: mws}
}

// hookResult holds a single (message, error) pair collected from the inner handler.
type hookResult struct {
	msg *blades.Message
	err error
}

// replayHandler creates a Handler that replays the collected results so that
// lifecycle hook middlewares can observe the original execution output.
func replayHandler(results []hookResult) blades.Handler {
	return blades.HandleFunc(func(_ context.Context, _ *blades.Invocation) blades.Generator[*blades.Message, error] {
		return func(yield func(*blades.Message, error) bool) {
			for _, r := range results {
				if !yield(r.msg, r.err) {
					return
				}
			}
		}
	})
}

// wrapOnComplete wraps mw so it only executes after the inner handler finishes
// without any error. The hook middleware receives a replay of the original
// results via its next handler, so it can observe the actual messages.
// Output from the hook itself is consumed but not re-yielded to the caller.
func wrapOnComplete(mw blades.Middleware) blades.Middleware {
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
			return func(yield func(*blades.Message, error) bool) {
				var results []hookResult
				hasError := false
				for msg, err := range next.Handle(ctx, inv) {
					if err != nil {
						hasError = true
					}
					results = append(results, hookResult{msg, err})
					if !yield(msg, err) {
						return
					}
				}
				if !hasError {
					// Run hook for side-effects; discard output to avoid duplicates.
					for range mw(replayHandler(results)).Handle(ctx, inv) {
					}
				}
			}
		})
	}
}

// wrapOnError wraps mw so it only executes after the inner handler yields an
// error. The hook middleware receives a replay of the original results via its
// next handler, so it can observe the actual errors.
// Output from the hook itself is consumed but not re-yielded to the caller.
func wrapOnError(mw blades.Middleware) blades.Middleware {
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
			return func(yield func(*blades.Message, error) bool) {
				var results []hookResult
				for msg, err := range next.Handle(ctx, inv) {
					results = append(results, hookResult{msg, err})
					if err != nil {
						// Run hook before surfacing the error. Callers like Runner.Run
						// stop consuming as soon as they see an error, so post-yield
						// hooks would otherwise never execute.
						for range mw(replayHandler(results)).Handle(ctx, inv) {
						}
						yield(msg, err)
						return
					}
					if !yield(msg, err) {
						return
					}
				}
			}
		})
	}
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

	// Context manager
	cm, err := buildContextManager(spec.Context, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if cm != nil {
		agentOpts = append(agentOpts, blades.WithContextManager(cm))
	}

	// Middleware (observability, approval, hooks)
	mws, err := buildAgentMiddlewares(spec, o)
	if err != nil {
		return nil, fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if len(mws) > 0 {
		agentOpts = append(agentOpts, blades.WithMiddleware(mws...))
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
