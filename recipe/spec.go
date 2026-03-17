package recipe

// ExecutionMode defines how sub-recipes are executed.
type ExecutionMode string

const (
	// ExecutionSequential runs sub-recipes one after another.
	ExecutionSequential ExecutionMode = "sequential"
	// ExecutionParallel runs sub-recipes concurrently.
	ExecutionParallel ExecutionMode = "parallel"
	// ExecutionTool wraps each sub-recipe as a tool for the parent agent.
	ExecutionTool ExecutionMode = "tool"
)

// ParameterType defines the type of a recipe parameter.
type ParameterType string

const (
	ParameterString  ParameterType = "string"
	ParameterNumber  ParameterType = "number"
	ParameterBoolean ParameterType = "boolean"
	ParameterSelect  ParameterType = "select"
)

// ParameterRequirement defines whether a parameter is required or optional.
type ParameterRequirement string

const (
	ParameterRequired ParameterRequirement = "required"
	ParameterOptional ParameterRequirement = "optional"
)

// ContextStrategy defines the strategy for managing the agent's context window.
type ContextStrategy string

const (
	// ContextTruncate drops oldest messages when the context limit is exceeded.
	ContextTruncate ContextStrategy = "truncate"
	// ContextSummarize compresses old messages into a rolling summary.
	ContextSummarize ContextStrategy = "summarize"
)

// ContextSpec configures context window management for an agent.
type ContextSpec struct {
	Strategy    ContextStrategy `yaml:"strategy"`
	MaxTokens   int64           `yaml:"max_tokens,omitempty"`
	MaxMessages int             `yaml:"max_messages,omitempty"` // truncate only
	KeepRecent  int             `yaml:"keep_recent,omitempty"`  // summarize only
	BatchSize   int             `yaml:"batch_size,omitempty"`   // summarize only
	Model       string          `yaml:"model,omitempty"`        // summarize: model used for compression
}

// ApprovalSpec declares that agent invocations require human approval.
// The actual callback is injected via WithApprovalHandler.
type ApprovalSpec struct {
	// OnTools restricts approval to invocations that include any of these tool names.
	// An empty list means every invocation requires approval.
	OnTools []string `yaml:"on_tools,omitempty"`
}

// MiddlewareSpec references a named middleware and optional configuration options.
// The name is resolved via the MiddlewareRegistry provided to Build.
type MiddlewareSpec struct {
	Name    string         `yaml:"name"`
	Options map[string]any `yaml:"options,omitempty"`
}

// RecipeSpec is the top-level declarative specification for a recipe.
// A recipe YAML file is parsed into this structure and then built into a blades.Agent.
type RecipeSpec struct {
	Version       string             `yaml:"version"`
	Name          string             `yaml:"name"`
	Description   string             `yaml:"description"`
	Model         string             `yaml:"model,omitempty"`
	Instruction   string             `yaml:"instruction"`
	Prompt        string             `yaml:"prompt,omitempty"`
	Parameters    []ParameterSpec    `yaml:"parameters,omitempty"`
	SubRecipes    []SubRecipeSpec    `yaml:"sub_recipes,omitempty"`
	Execution     ExecutionMode      `yaml:"execution,omitempty"`
	Tools         []string           `yaml:"tools,omitempty"`
	OutputKey     string             `yaml:"output_key,omitempty"`
	MaxIterations int                `yaml:"max_iterations,omitempty"`
	Context       *ContextSpec       `yaml:"context,omitempty"`
	Approval      *ApprovalSpec      `yaml:"approval,omitempty"`
	Middlewares   []MiddlewareSpec    `yaml:"middlewares,omitempty"`
}

// SubRecipeSpec defines a child agent within a recipe.
type SubRecipeSpec struct {
	Name          string          `yaml:"name"`
	Description   string          `yaml:"description,omitempty"`
	Model         string          `yaml:"model,omitempty"`
	Instruction   string          `yaml:"instruction"`
	Prompt        string          `yaml:"prompt,omitempty"`
	Parameters    []ParameterSpec `yaml:"parameters,omitempty"`
	Tools         []string        `yaml:"tools,omitempty"`
	OutputKey     string          `yaml:"output_key,omitempty"`
	MaxIterations int             `yaml:"max_iterations,omitempty"`
}

// ParameterSpec defines a configurable parameter for a recipe.
type ParameterSpec struct {
	Name        string               `yaml:"name"`
	Type        ParameterType        `yaml:"type"`
	Description string               `yaml:"description"`
	Default     any                  `yaml:"default,omitempty"`
	Required    ParameterRequirement `yaml:"required,omitempty"`
	Options     []string             `yaml:"options,omitempty"`
}

// AgentSpec is a Kubernetes-style declarative specification for a blades agent.
// It provides a flat, human-friendly YAML format that is converted to a RecipeSpec
// internally via ToRecipeSpec.
type AgentSpec struct {
	Kind        string          `yaml:"kind"`
	Version     string          `yaml:"version"`
	Name        string          `yaml:"name"`
	Description string          `yaml:"description,omitempty"`
	Instruction string          `yaml:"instruction,omitempty"`
	Model       AgentModelSpec  `yaml:"model"`
	Tools       []string        `yaml:"tools,omitempty"`
	Policy      *PolicySpec     `yaml:"policy,omitempty"`
	Context     *ContextSpec    `yaml:"context,omitempty"`
	Approval    *ApprovalSpec   `yaml:"approval,omitempty"`
	Middlewares []MiddlewareSpec `yaml:"middlewares,omitempty"`
}

// AgentModelSpec defines the model configuration for an AgentSpec.
type AgentModelSpec struct {
	Primary  string `yaml:"primary"`
	Fallback string `yaml:"fallback,omitempty"` // reserved: fallback logic is not yet implemented
	Router   string `yaml:"router,omitempty"`   // reserved: model routing is not yet implemented
}

// PolicySpec defines runtime policy constraints for an AgentSpec.
type PolicySpec struct {
	MaxTurns      int    `yaml:"max_turns,omitempty"`
	MaxCostPerRun string `yaml:"max_cost_per_run,omitempty"` // reserved, not yet implemented
}

// ToRecipeSpec converts an AgentSpec into an equivalent RecipeSpec so that
// both formats share the same Build pipeline.
func (a *AgentSpec) ToRecipeSpec() *RecipeSpec {
	spec := &RecipeSpec{
		Version:     a.Version,
		Name:        a.Name,
		Description: a.Description,
		Model:       a.Model.Primary,
		Instruction: a.Instruction,
		Tools:       a.Tools,
		Context:     a.Context,
		Approval:    a.Approval,
		Middlewares: a.Middlewares,
	}
	if a.Policy != nil {
		spec.MaxIterations = a.Policy.MaxTurns
	}
	return spec
}
